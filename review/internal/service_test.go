package review

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceReviewRoundFindingVerdictAndResponse(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}}
	service := newTestService(store, messages, tasks)

	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/3696-review-service", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
		TestsRun: []string{"go test ./review/..."},
	})
	if err != nil {
		t.Fatalf("CreateRound() error = %v", err)
	}
	if round.RoundNumber != 1 || round.PreferredDiffBaseRef != "main" || round.PreferredDiffHeadRef != "task/3696-review-service" {
		t.Fatalf("round metadata not defaulted: %+v", round)
	}

	finding, err := service.CreateFinding(ctx, round.ID, CreateReviewFindingRequest{
		CreatedBy: "pi-reviewer", Category: CategoryBlockingBug, Summary: "Status update can lose evidence", FileReferences: []string{"review/internal/service.go:1"},
	})
	if err != nil {
		t.Fatalf("CreateFinding() error = %v", err)
	}
	if finding.FindingKey != "R42-1" || finding.Status != StatusOpen {
		t.Fatalf("finding numbering/status mismatch: %+v", finding)
	}

	responded, err := service.RespondToFinding(ctx, finding.ID, RespondToFindingRequest{
		RespondedBy: "pi-coder", ResponseNotes: "Fixed", Status: StatusClaimedFixed, StatusNotes: "Added test",
	})
	if err != nil {
		t.Fatalf("RespondToFinding() error = %v", err)
	}
	if responded.ResponseNotes != "Fixed" || responded.StatusNotes != "Added test" || responded.Status != StatusClaimedFixed {
		t.Fatalf("response/status fields not preserved separately: %+v", responded)
	}

	verdict, err := service.SetVerdict(ctx, round.ID, SetReviewVerdictRequest{Verdict: VerdictBlockedByDependency, DecidedBy: "pi-reviewer", Notes: "One issue"})
	if err != nil {
		t.Fatalf("SetVerdict() error = %v", err)
	}
	if verdict.Verdict != VerdictBlockedByDependency {
		t.Fatalf("verdict not stored: %+v", verdict)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "review_feedback" {
		t.Fatalf("verdict message not appended as review feedback: %+v", messages.appended)
	}
	metadata := messages.appended[0].Metadata
	if metadata["type"] != "review_feedback" || metadata["packet_kind"] != PacketKindReviewFindings {
		t.Fatalf("verdict metadata did not separate type/packet_kind: %#v", metadata)
	}
}

func TestServiceSetVerdictRejectsGreenPathVerdicts(t *testing.T) {
	for _, verdict := range []string{VerdictLooksGood, VerdictChangesRequested} {
		t.Run(verdict, func(t *testing.T) {
			ctx := context.Background()
			service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
				42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview},
			}})
			round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
				RequestedBy: "pi", Branch: "task/verdict", BaseBranch: "main", BaseCommit: "base", HeadCommit: verdict,
			})
			if err != nil {
				t.Fatalf("CreateRound() error = %v", err)
			}
			if _, err := service.SetVerdict(ctx, round.ID, SetReviewVerdictRequest{
				Verdict: verdict, DecidedBy: "pi-reviewer",
			}); err == nil || !errors.Is(err, ErrInvalidVerdict) {
				t.Fatalf("SetVerdict() error = %v, want ErrInvalidVerdict", err)
			}
		})
	}
}

func TestRequestReviewMetadataUsesCanonicalPacketKind(t *testing.T) {
	ctx := context.Background()
	messages := &fakeMessages{}
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}}
	service := newTestService(newMemoryStore(), messages, tasks)
	packet, err := service.RequestReview(ctx, "den-services", 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/3696-review-service", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	})
	if err != nil {
		t.Fatalf("RequestReview() error = %v", err)
	}
	if packet.TypedEnvelope["type"] != "review_request_packet" || packet.TypedEnvelope["packet_kind"] != PacketKindReviewRequest {
		t.Fatalf("packet metadata did not separate type/packet_kind: %#v", packet.TypedEnvelope)
	}
	if len(messages.appended) != 1 {
		t.Fatalf("expected one appended message, got %d", len(messages.appended))
	}
	if messages.appended[0].Metadata["type"] != "review_request_packet" || messages.appended[0].Metadata["packet_kind"] != PacketKindReviewRequest {
		t.Fatalf("message metadata did not separate type/packet_kind: %#v", messages.appended[0].Metadata)
	}
	if packet.TaskTransition != TaskTransitionAlreadySatisfied || packet.ResultingTaskStatus != TaskStatusReview {
		t.Fatalf("task transition result = %q/%q", packet.TaskTransition, packet.ResultingTaskStatus)
	}
	if len(tasks.statusUpdates) != 0 {
		t.Fatalf("already-review task received status updates: %+v", tasks.statusUpdates)
	}
}

func TestRequestReviewTransitionsInProgressTaskAndConvergesAfterResponseLoss(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	tasks := &fakeTasks{
		tasks: map[int64]TaskContext{
			42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
		},
		failAfterStatusUpdate: true,
	}
	service := newTestService(store, messages, tasks)
	request := CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/6377-request-review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
		TestsRun: []string{"go test -race ./review/internal"}, Notes: "Request review and transition atomically from the caller's perspective.",
	}

	if _, err := service.RequestReview(ctx, "den-services", 42, request); err == nil || !strings.Contains(err.Error(), "response lost") {
		t.Fatalf("first RequestReview() error = %v, want response loss", err)
	}
	packet, err := service.RequestReview(ctx, "den-services", 42, request)
	if err != nil {
		t.Fatalf("retry RequestReview() error = %v", err)
	}
	rounds, err := store.ListRounds(ctx, "den-services", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 || len(messages.appended) != 1 || len(tasks.statusUpdates) != 1 {
		t.Fatalf("retry duplicated workflow state: rounds=%d messages=%d status_updates=%d",
			len(rounds), len(messages.appended), len(tasks.statusUpdates))
	}
	if packet.TaskTransition != TaskTransitionAlreadySatisfied || packet.ResultingTaskStatus != TaskStatusReview {
		t.Fatalf("retry task transition result = %q/%q", packet.TaskTransition, packet.ResultingTaskStatus)
	}
}

func TestRequestReviewTransitionsInProgressTaskAndReportsResult(t *testing.T) {
	ctx := context.Background()
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Status: TaskStatusInProgress},
	}}
	service := newTestService(newMemoryStore(), &fakeMessages{}, tasks)
	packet, err := service.RequestReview(ctx, "den-services", 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "main", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	})
	if err != nil {
		t.Fatalf("RequestReview() error = %v", err)
	}
	response := toPacketResponse(packet)
	if response.TaskTransition != TaskTransitionApplied || response.ResultingTaskStatus != TaskStatusReview {
		t.Fatalf("task transition response = %+v", response)
	}
	if len(tasks.statusUpdates) != 1 || tasks.statusUpdates[0].Status != TaskStatusReview {
		t.Fatalf("task status updates = %+v", tasks.statusUpdates)
	}
}

func TestRequestReviewReturnsTypedRetryableTaskTransitionError(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	tasks := &fakeTasks{
		tasks: map[int64]TaskContext{
			42: {ID: 42, ProjectID: "den-services", Status: TaskStatusInProgress},
		},
		failStatusUpdate: true,
	}
	service := newTestService(store, &fakeMessages{}, tasks)
	request := CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "main", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	}
	if _, err := service.RequestReview(ctx, "den-services", 42, request); err == nil {
		t.Fatal("RequestReview() error = nil, want retryable transition error")
	} else {
		var serviceErr *ServiceError
		if !errors.As(err, &serviceErr) || serviceErr.Code() != "task_transition_retryable" ||
			serviceErr.HTTPStatus() != http.StatusServiceUnavailable {
			t.Fatalf("RequestReview() error = %#v, want typed retryable transition error", err)
		}
	}

	tasks.failStatusUpdate = false
	packet, err := service.RequestReview(ctx, "den-services", 42, request)
	if err != nil {
		t.Fatalf("retry RequestReview() error = %v", err)
	}
	rounds, err := store.ListRounds(ctx, "den-services", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 || packet.TaskTransition != TaskTransitionApplied {
		t.Fatalf("retry did not converge: rounds=%d packet=%+v", len(rounds), packet)
	}
}

func TestRequestReviewRejectsTerminalTasks(t *testing.T) {
	for _, status := range []string{TaskStatusDone, TaskStatusCancelled} {
		t.Run(status, func(t *testing.T) {
			service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
				42: {ID: 42, ProjectID: "den-services", Status: status},
			}})
			_, err := service.RequestReview(context.Background(), "den-services", 42, CreateReviewRoundRequest{
				RequestedBy: "pi", Branch: "main", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
			})
			if err == nil || !errors.Is(err, ErrInvalidTaskState) {
				t.Fatalf("RequestReview() error = %v, want ErrInvalidTaskState", err)
			}
		})
	}
}

func TestRequestReviewDoesNotOverwriteStatusThatWinsAfterValidation(t *testing.T) {
	for _, status := range []string{TaskStatusDone, TaskStatusCancelled, "blocked"} {
		t.Run(status, func(t *testing.T) {
			store := newMemoryStore()
			messages := &fakeMessages{}
			tasks := &fakeTasks{
				tasks: map[int64]TaskContext{
					42: {ID: 42, ProjectID: "den-services", Status: TaskStatusInProgress},
				},
				statusBeforeReviewTransition: status,
			}
			service := newTestService(store, messages, tasks)
			_, err := service.RequestReview(context.Background(), "den-services", 42, CreateReviewRoundRequest{
				RequestedBy: "pi", Branch: "main", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
			})
			var serviceErr *ServiceError
			if !errors.As(err, &serviceErr) || serviceErr.Code() != "review_transition_ineligible" ||
				serviceErr.HTTPStatus() != http.StatusConflict {
				t.Fatalf("RequestReview() error = %#v, want typed non-mutating conflict", err)
			}
			if got := tasks.tasks[42].Status; got != status {
				t.Fatalf("task status = %q, want winning status %q", got, status)
			}
			rounds, listErr := store.ListRounds(context.Background(), "den-services", 42)
			if listErr != nil {
				t.Fatal(listErr)
			}
			if len(rounds) != 1 || len(messages.appended) != 1 || len(tasks.statusUpdates) != 0 {
				t.Fatalf("workflow effects: rounds=%d messages=%d status_updates=%d",
					len(rounds), len(messages.appended), len(tasks.statusUpdates))
			}
		})
	}
}

func TestRequestReviewConcurrentRetriesCreateOneWorkflowTransition(t *testing.T) {
	ctx := context.Background()
	store := &synchronizedRequestStore{memoryStore: newMemoryStore()}
	messages := &synchronizedMessages{fakeMessages: &fakeMessages{}}
	tasks := newBarrierTasks(false)
	tasks.task.Status = TaskStatusInProgress
	service := newTestService(store, messages, tasks)
	request := CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/6377-request-review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	}

	results := make(chan *ReviewPacket, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			packet, err := service.RequestReview(ctx, "den-services", 42, request)
			results <- packet
			errs <- err
		}()
	}
	<-tasks.entered
	<-tasks.entered
	close(tasks.release)

	var packets []*ReviewPacket
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent RequestReview() error = %v", err)
		}
		packets = append(packets, <-results)
	}
	rounds, err := store.ListRounds(ctx, "den-services", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 || packets[0].ID != packets[1].ID {
		t.Fatalf("concurrent requests diverged: rounds=%d packet_ids=%d/%d", len(rounds), packets[0].ID, packets[1].ID)
	}
	if len(messages.appended) != 1 || tasks.historyTransitions != 1 {
		t.Fatalf("concurrent requests duplicated effects: messages=%d transitions=%d", len(messages.appended), tasks.historyTransitions)
	}
}

func TestRequestCampaignReviewSnapshotsThreeRepositoryCampaignAndFinalizesIdempotently(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	parentID := int64(5934)
	campaignTag := "campaign:den-services:5934"
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		parentID: {ID: parentID, ProjectID: "den-services", Status: TaskStatusReview, Tags: []string{campaignTag}},
		6097:     {ID: 6097, ProjectID: "den-services", Status: TaskStatusDone, ParentID: &parentID},
		6098:     {ID: 6098, ProjectID: "asha-d20-fantasy", Status: TaskStatusDone, Tags: []string{campaignTag}},
		6099:     {ID: 6099, ProjectID: "asha-rulebench", Status: TaskStatusDone, Tags: []string{campaignTag}},
	}}
	service := newTestService(store, messages, tasks)

	repositories := []CampaignRepositoryHead{
		{Repository: "FuzzySlipper/asha-rpg", HeadSHA: strings.Repeat("a", 40)},
		{Repository: "FuzzySlipper/asha-d20-fantasy", HeadSHA: strings.Repeat("b", 40)},
		{Repository: "FuzzySlipper/asha-rulebench", HeadSHA: strings.Repeat("c", 40)},
	}
	childSpecs := []struct {
		projectID string
		taskID    int64
		head      string
	}{
		{projectID: "den-services", taskID: 6097, head: repositories[0].HeadSHA},
		{projectID: "asha-d20-fantasy", taskID: 6098, head: repositories[1].HeadSHA},
		{projectID: "asha-rulebench", taskID: 6099, head: repositories[2].HeadSHA},
	}
	children := make([]CampaignReviewChildRequest, 0, len(childSpecs))
	for _, spec := range childSpecs {
		round, err := store.CreateRound(ctx, &ReviewRound{
			ProjectID: spec.projectID, TaskID: spec.taskID, RequestedBy: "implementer",
			TargetKind: ReviewTargetCodeDiff, Branch: "main", BaseBranch: "main",
			BaseCommit: strings.Repeat("0", 40), HeadCommit: spec.head,
			RequestedAt: fixedReviewTestTime(), CreatedAt: fixedReviewTestTime(), UpdatedAt: fixedReviewTestTime(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetVerdict(ctx, round.ID, VerdictLooksGood, "reviewer", "approved", fixedReviewTestTime()); err != nil {
			t.Fatal(err)
		}
		children = append(children, CampaignReviewChildRequest{
			ProjectID: spec.projectID, TaskID: spec.taskID, ReviewRoundID: round.ID,
		})
	}

	packet, err := service.RequestCampaignReview(ctx, "den-services", parentID, CreateCampaignReviewRequest{
		RequestedBy: "campaign-agent", Children: children, Repositories: repositories,
		TestsRun: []string{"three-repo campaign replay"}, Notes: "Modeled after campaign task 5934.",
	})
	if err != nil {
		t.Fatalf("RequestCampaignReview() error = %v", err)
	}
	round, err := store.GetRound(ctx, *packet.ReviewRoundID)
	if err != nil {
		t.Fatal(err)
	}
	if round.TargetKind != ReviewTargetCampaignReconciliation || len(round.CampaignChildren) != 3 || len(round.CampaignRepositories) != 3 {
		t.Fatalf("campaign snapshot = %+v", round)
	}
	if round.Branch != "" || round.BaseCommit != "" || round.HeadCommit != "" {
		t.Fatalf("campaign unexpectedly stored diff target: %+v", round)
	}
	membershipByTask := map[int64]string{}
	for _, child := range round.CampaignChildren {
		membershipByTask[child.TaskID] = child.MembershipKind
	}
	if membershipByTask[6097] != CampaignMembershipDirectSubtask ||
		membershipByTask[6098] != CampaignMembershipTag ||
		membershipByTask[6099] != CampaignMembershipTag {
		t.Fatalf("membership snapshot = %+v", membershipByTask)
	}
	if _, exists := packet.TypedEnvelope["head_commit"]; exists {
		t.Fatalf("campaign packet contains diff head: %#v", packet.TypedEnvelope)
	}
	if packet.TypedEnvelope["target_kind"] != ReviewTargetCampaignReconciliation ||
		!strings.Contains(packet.MarkdownBody, "Campaign Reconciliation") {
		t.Fatalf("campaign packet = %#v\n%s", packet.TypedEnvelope, packet.MarkdownBody)
	}
	retriedPacket, err := service.RequestCampaignReview(ctx, "den-services", parentID, CreateCampaignReviewRequest{
		RequestedBy: "campaign-agent",
		Children: []CampaignReviewChildRequest{
			children[2],
			children[0],
			children[1],
		},
		Repositories: []CampaignRepositoryHead{
			repositories[2],
			repositories[0],
			repositories[1],
		},
		TestsRun: []string{"three-repo campaign replay"}, Notes: "Modeled after campaign task 5934.",
	})
	if err != nil {
		t.Fatalf("RequestCampaignReview() retry error = %v", err)
	}
	parentRounds, err := store.ListRounds(ctx, "den-services", parentID)
	if err != nil {
		t.Fatal(err)
	}
	if retriedPacket.ID != packet.ID || len(parentRounds) != 1 || len(messages.appended) != 1 {
		t.Fatalf("campaign rerequest duplicated state: packets=%d/%d rounds=%d messages=%d",
			packet.ID, retriedPacket.ID, len(parentRounds), len(messages.appended))
	}

	finding, err := service.CreateFinding(ctx, round.ID, CreateReviewFindingRequest{
		CreatedBy: "reviewer", Category: CategoryBlockingBug, Summary: "campaign integration needs correction",
	})
	if err != nil {
		t.Fatal(err)
	}
	changesRequested, err := service.FinalizeReview(ctx, FinalizeReviewRequest{
		ReviewRoundID: round.ID, Verdict: VerdictChangesRequested, DecidedBy: "reviewer",
	})
	if err != nil {
		t.Fatalf("changes-requested FinalizeReview() error = %v", err)
	}
	if changesRequested.Finalization.State != FinalizationStateComplete || tasks.tasks[parentID].Status != TaskStatusInProgress {
		t.Fatalf("changes-requested finalization = %+v task=%+v", changesRequested.Finalization, tasks.tasks[parentID])
	}
	if _, err := service.SetFindingStatus(ctx, finding.ID, SetFindingStatusRequest{
		Status: StatusVerifiedFixed, UpdatedBy: "implementer", Notes: "campaign correction verified",
	}); err != nil {
		t.Fatal(err)
	}

	rereviewPacket, err := service.RequestCampaignReview(ctx, "den-services", parentID, CreateCampaignReviewRequest{
		RequestedBy: "campaign-agent", Children: children, Repositories: repositories,
		TestsRun: []string{"three-repo campaign replay"}, Notes: "Modeled after campaign task 5934.",
	})
	if err != nil {
		t.Fatalf("RequestCampaignReview() after changes requested error = %v", err)
	}
	parentRounds, err = store.ListRounds(ctx, "den-services", parentID)
	if err != nil {
		t.Fatal(err)
	}
	if rereviewPacket.ID == packet.ID || len(parentRounds) != 2 || parentRounds[1].RoundNumber != 2 {
		t.Fatalf("campaign rereview did not create a new round: packets=%d/%d rounds=%+v",
			packet.ID, rereviewPacket.ID, parentRounds)
	}

	parentTask := tasks.tasks[parentID]
	parentTask.Status = TaskStatusReview
	tasks.tasks[parentID] = parentTask
	finalize := FinalizeReviewRequest{ReviewRoundID: *rereviewPacket.ReviewRoundID, Verdict: VerdictLooksGood, DecidedBy: "reviewer"}
	first, err := service.FinalizeReview(ctx, finalize)
	if err != nil {
		t.Fatalf("looks-good FinalizeReview() error = %v", err)
	}
	second, err := service.FinalizeReview(ctx, finalize)
	if err != nil {
		t.Fatalf("looks-good FinalizeReview() retry error = %v", err)
	}
	if first.Finalization.ID != second.Finalization.ID || first.Finalization.State != FinalizationStateComplete {
		t.Fatalf("campaign finalization not idempotent: first=%+v second=%+v", first.Finalization, second.Finalization)
	}
	if len(messages.appended) != 4 || len(tasks.statusUpdates) != 2 {
		t.Fatalf("campaign lifecycle side effects: messages=%d task_updates=%d", len(messages.appended), len(tasks.statusUpdates))
	}
	if _, exists := first.Packet.TypedEnvelope["reviewed_head_commit"]; exists {
		t.Fatalf("campaign completion packet contains diff head: %#v", first.Packet.TypedEnvelope)
	}
}

func TestRequestCampaignReviewRejectsStaleUnapprovedUnrelatedDuplicateAndMismatchedTargets(t *testing.T) {
	const (
		parentID = int64(6212)
		childID  = int64(7001)
	)
	head := strings.Repeat("d", 40)
	baseTasks := func() *fakeTasks {
		return &fakeTasks{tasks: map[int64]TaskContext{
			parentID: {ID: parentID, ProjectID: "den-services", Status: TaskStatusReview},
			childID:  {ID: childID, ProjectID: "den-services", Status: TaskStatusDone, ParentID: ptrInt64(parentID)},
		}}
	}
	seedRound := func(t *testing.T, store *memoryStore, verdict string) *ReviewRound {
		t.Helper()
		round, err := store.CreateRound(context.Background(), &ReviewRound{
			ProjectID: "den-services", TaskID: childID, RequestedBy: "agent", TargetKind: ReviewTargetCodeDiff,
			Branch: "main", BaseBranch: "main", BaseCommit: strings.Repeat("0", 40), HeadCommit: head,
			RequestedAt: fixedReviewTestTime(), CreatedAt: fixedReviewTestTime(), UpdatedAt: fixedReviewTestTime(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if verdict != "" {
			if _, err := store.SetVerdict(context.Background(), round.ID, verdict, "reviewer", "", fixedReviewTestTime()); err != nil {
				t.Fatal(err)
			}
			round.Verdict = verdict
		}
		return round
	}
	request := func(roundID int64) CreateCampaignReviewRequest {
		return CreateCampaignReviewRequest{
			RequestedBy:  "agent",
			Children:     []CampaignReviewChildRequest{{ProjectID: "den-services", TaskID: childID, ReviewRoundID: roundID}},
			Repositories: []CampaignRepositoryHead{{Repository: "owner/repo", HeadSHA: head}},
		}
	}

	t.Run("unapproved", func(t *testing.T) {
		store := newMemoryStore()
		round := seedRound(t, store, VerdictChangesRequested)
		_, err := newTestService(store, &fakeMessages{}, baseTasks()).RequestCampaignReview(context.Background(), "den-services", parentID, request(round.ID))
		assertServiceErrorCode(t, err, "unapproved_campaign_review_round")
	})
	t.Run("stale", func(t *testing.T) {
		store := newMemoryStore()
		round := seedRound(t, store, VerdictLooksGood)
		store.rounds[round.ID].HeadCommit = strings.Repeat("e", 40)
		_ = seedRound(t, store, VerdictLooksGood)
		_, err := newTestService(store, &fakeMessages{}, baseTasks()).RequestCampaignReview(context.Background(), "den-services", parentID, request(round.ID))
		assertServiceErrorCode(t, err, "stale_campaign_review_round")
	})
	t.Run("unrelated", func(t *testing.T) {
		store := newMemoryStore()
		round := seedRound(t, store, VerdictLooksGood)
		tasks := baseTasks()
		tasks.tasks[childID] = TaskContext{ID: childID, ProjectID: "den-services", Status: TaskStatusDone}
		_, err := newTestService(store, &fakeMessages{}, tasks).RequestCampaignReview(context.Background(), "den-services", parentID, request(round.ID))
		assertServiceErrorCode(t, err, "unrelated_campaign_child")
	})
	t.Run("duplicate", func(t *testing.T) {
		store := newMemoryStore()
		round := seedRound(t, store, VerdictLooksGood)
		req := request(round.ID)
		req.Children = append(req.Children, req.Children[0])
		_, err := newTestService(store, &fakeMessages{}, baseTasks()).RequestCampaignReview(context.Background(), "den-services", parentID, req)
		assertServiceErrorCode(t, err, "duplicate_campaign_child")
	})
	t.Run("head mismatch", func(t *testing.T) {
		store := newMemoryStore()
		round := seedRound(t, store, VerdictLooksGood)
		req := request(round.ID)
		req.Repositories[0].HeadSHA = strings.Repeat("f", 40)
		_, err := newTestService(store, &fakeMessages{}, baseTasks()).RequestCampaignReview(context.Background(), "den-services", parentID, req)
		assertServiceErrorCode(t, err, "campaign_head_mismatch")
	})
	t.Run("missing round", func(t *testing.T) {
		_, err := newTestService(newMemoryStore(), &fakeMessages{}, baseTasks()).RequestCampaignReview(
			context.Background(), "den-services", parentID, request(9999),
		)
		assertServiceErrorCode(t, err, "missing_campaign_review_round")
	})
	t.Run("duplicate repository", func(t *testing.T) {
		store := newMemoryStore()
		round := seedRound(t, store, VerdictLooksGood)
		req := request(round.ID)
		req.Repositories = append(req.Repositories, req.Repositories[0])
		_, err := newTestService(store, &fakeMessages{}, baseTasks()).RequestCampaignReview(context.Background(), "den-services", parentID, req)
		assertServiceErrorCode(t, err, "duplicate_campaign_repository")
	})
	t.Run("unresolved blocker", func(t *testing.T) {
		store := newMemoryStore()
		round := seedRound(t, store, VerdictLooksGood)
		if _, err := store.CreateFinding(context.Background(), &ReviewFinding{
			ProjectID: "den-services", TaskID: childID, ReviewRoundID: round.ID, CreatedBy: "reviewer",
			Category: CategoryAcceptanceGap, Summary: "missing proof", Status: StatusOpen,
			CreatedAt: fixedReviewTestTime(), UpdatedAt: fixedReviewTestTime(),
		}); err != nil {
			t.Fatal(err)
		}
		_, err := newTestService(store, &fakeMessages{}, baseTasks()).RequestCampaignReview(context.Background(), "den-services", parentID, request(round.ID))
		assertServiceErrorCode(t, err, "blocked_campaign_child")
	})
}

func TestParseCampaignReviewPacketDoesNotRequireDiffFields(t *testing.T) {
	packet, err := ParseReviewPacketMarkdown(`---
schema: den_review_packet
schema_version: 1
packet_kind: review_request
project_id: den-services
task_id: 6212
review_round_id: 77
sender: campaign-agent
requested_by: campaign-agent
target_kind: campaign_reconciliation
campaign_children:
  - project_id: den-services
    task_id: 7001
    review_round_id: 70
campaign_repositories:
  - repository: owner/repo
    head_sha: 0123456789abcdef0123456789abcdef01234567
verify:
  - checked: true
    item: child rounds approved
---
Campaign review evidence.
`)
	if err != nil {
		t.Fatalf("ParseReviewPacketMarkdown() error = %v", err)
	}
	if packet.TypedEnvelope["target_kind"] != ReviewTargetCampaignReconciliation || packet.ReviewRoundID == nil {
		t.Fatalf("packet = %+v", packet)
	}
}

func ptrInt64(value int64) *int64 {
	return &value
}

func assertServiceErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	var serviceError *ServiceError
	if !errors.As(err, &serviceError) || serviceError.Code() != want {
		t.Fatalf("error = %T %v, want service code %s", err, err, want)
	}
}

func TestDiscoverGitHubChecksIsReadOnly(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Status: TaskStatusInProgress},
	}}
	provider := &fakeGitHubChecks{result: GitHubCheckResult{
		ObservedCheckRuns: []GitHubCheckRun{
			{Name: "Fast changed-surface safeguards", Status: "completed", Conclusion: "success", DetailsURL: "https://github.test/job/1"},
		},
		MissingRequiredChecks:     []string{"ASHA CI"},
		AllObservedChecksTerminal: true,
	}}
	service := newTestService(store, messages, tasks)
	service.ConfigureGitHubChecks(provider, DefaultGitHubCheckOptions())

	discovery, err := service.DiscoverGitHubChecks(ctx, DiscoverGitHubChecksRequest{
		Repository: " FuzzySlipper/asha ", CommitSHA: "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
		RequiredChecks: []string{" ASHA CI "},
	})
	if err != nil {
		t.Fatalf("DiscoverGitHubChecks() error = %v", err)
	}
	if discovery.Repository != "FuzzySlipper/asha" || discovery.CommitSHA != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("normalized discovery target = %+v", discovery)
	}
	if discovery.ConfigurationStatus != GitHubCheckDiscoveryMissing ||
		len(discovery.MissingRequiredChecks) != 1 ||
		len(discovery.ObservedCheckRuns) != 1 ||
		discovery.ObservedCheckRuns[0].DetailsURL == "" {
		t.Fatalf("discovery diagnostics = %+v", discovery)
	}
	if provider.calls != 1 || provider.lastRepository != "FuzzySlipper/asha" ||
		provider.lastCommitSHA != discovery.CommitSHA ||
		len(provider.lastRequiredChecks) != 1 || provider.lastRequiredChecks[0] != "ASHA CI" {
		t.Fatalf("provider call = %+v", provider)
	}
	if len(tasks.statusUpdates) != 0 || len(messages.appended) != 0 || len(store.githubCheckGates) != 0 {
		t.Fatalf("discovery mutated workflow: task_updates=%v messages=%v gates=%v",
			tasks.statusUpdates, messages.appended, store.githubCheckGates)
	}
}

func TestDiscoverGitHubChecksWithoutValidationReportsNoVisibleRuns(t *testing.T) {
	provider := &fakeGitHubChecks{result: GitHubCheckResult{}}
	service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{})
	service.ConfigureGitHubChecks(provider, DefaultGitHubCheckOptions())

	discovery, err := service.DiscoverGitHubChecks(context.Background(), DiscoverGitHubChecksRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567",
	})
	if err != nil {
		t.Fatal(err)
	}
	if discovery.ConfigurationStatus != GitHubCheckDiscoveryNotValidated ||
		!strings.Contains(discovery.Summary, "No GitHub check runs") ||
		len(discovery.ObservedCheckRuns) != 0 {
		t.Fatalf("discovery = %+v", discovery)
	}
}

func TestDiscoverGitHubChecksRejectsInvalidTargetBeforeProviderCall(t *testing.T) {
	provider := &fakeGitHubChecks{}
	service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{})
	service.ConfigureGitHubChecks(provider, DefaultGitHubCheckOptions())

	for _, req := range []DiscoverGitHubChecksRequest{
		{Repository: "not-a-repository", CommitSHA: "0123456789abcdef0123456789abcdef01234567"},
		{Repository: "owner/repo", CommitSHA: "short"},
	} {
		if _, err := service.DiscoverGitHubChecks(context.Background(), req); err == nil {
			t.Fatalf("DiscoverGitHubChecks(%+v) error = nil", req)
		}
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
}

func TestPostReviewFindingsAppendsCompatiblePacket(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}})
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateFinding(ctx, round.ID, CreateReviewFindingRequest{
		CreatedBy: "pi-reviewer", Category: CategoryBlockingBug, Summary: "Needs fix", TestCommands: []string{"go test ./..."},
	}); err != nil {
		t.Fatal(err)
	}
	packet, err := service.PostReviewFindings(ctx, "den-services", 42, PostReviewFindingsRequest{
		ReviewRoundID: round.ID, Sender: "pi-reviewer", Notes: "Review complete", RunID: "run-1",
	})
	if err != nil {
		t.Fatalf("PostReviewFindings() error = %v", err)
	}
	if packet.ID == 0 || packet.MessageID == nil || packet.PacketKind != PacketKindReviewFindings {
		t.Fatalf("packet not accepted: %+v", packet)
	}
	if packet.TypedEnvelope["type"] != "review_findings_packet" || packet.TypedEnvelope["run_id"] != "run-1" {
		t.Fatalf("unexpected packet metadata: %#v", packet.TypedEnvelope)
	}
	if !strings.Contains(packet.SourceMarkdown, "Review findings") || !strings.Contains(packet.SourceMarkdown, "Needs fix") {
		t.Fatalf("packet markdown missing findings: %s", packet.SourceMarkdown)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "review_feedback" {
		t.Fatalf("message not appended as review feedback: %+v", messages.appended)
	}
}

func TestFinalizeReviewLooksGoodCompletesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}}
	service := newTestService(store, messages, tasks)
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := FinalizeReviewRequest{
		ReviewRoundID: round.ID, Verdict: VerdictLooksGood, DecidedBy: "pi-reviewer",
		Notes: "Acceptance proof is complete.", RunID: "run-1", SubagentRole: "reviewer",
	}
	first, err := service.FinalizeReview(ctx, req)
	if err != nil {
		t.Fatalf("FinalizeReview() error = %v", err)
	}
	second, err := service.FinalizeReview(ctx, req)
	if err != nil {
		t.Fatalf("FinalizeReview() retry error = %v", err)
	}
	if first.Finalization.ID != second.Finalization.ID || second.Finalization.State != FinalizationStateComplete {
		t.Fatalf("retry returned different/incomplete finalization: first=%+v second=%+v", first.Finalization, second.Finalization)
	}
	if second.TaskStatus != TaskStatusDone || tasks.tasks[42].Status != TaskStatusDone {
		t.Fatalf("task status = %q/%q, want done", second.TaskStatus, tasks.tasks[42].Status)
	}
	if len(messages.appended) != 1 || len(tasks.statusUpdates) != 1 {
		t.Fatalf("duplicate side effects: messages=%d task_updates=%d", len(messages.appended), len(tasks.statusUpdates))
	}
	if messages.appended[0].Metadata["review_packet_id"] != first.Packet.ID {
		t.Fatalf("message metadata missing canonical packet identity: %#v", messages.appended[0].Metadata)
	}
	if first.Packet.MessageID == nil || first.Packet.ValidationStatus != PacketStatusAccepted {
		t.Fatalf("packet not accepted: %+v", first.Packet)
	}
}

func TestFinalizeReviewChangesRequestedRequiresAndPreservesCurrentRoundFinding(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}}
	service := newTestService(store, messages, tasks)
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	})
	if err != nil {
		t.Fatal(err)
	}
	finding, err := service.CreateFinding(ctx, round.ID, CreateReviewFindingRequest{
		CreatedBy: "pi-reviewer", Category: CategoryAcceptanceGap, Summary: "Live recovery proof is missing.",
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.FinalizeReview(ctx, FinalizeReviewRequest{
		ReviewRoundID: round.ID, Verdict: VerdictChangesRequested, DecidedBy: "pi-reviewer",
	})
	if err != nil {
		t.Fatalf("FinalizeReview() error = %v", err)
	}
	if receipt.Finalization.State != FinalizationStateComplete || receipt.TaskStatus != TaskStatusInProgress {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if !strings.Contains(receipt.Packet.SourceMarkdown, finding.FindingKey) ||
		!strings.Contains(receipt.Packet.SourceMarkdown, finding.Summary) {
		t.Fatalf("canonical packet omitted finding: %s", receipt.Packet.SourceMarkdown)
	}
}

func TestFinalizeReviewRejectsInconsistentFindings(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		verdict    string
		createOpen bool
		wantCode   string
	}{
		{name: "looks good with unresolved task finding", verdict: VerdictLooksGood, createOpen: true, wantCode: "unresolved_review_findings"},
		{name: "changes requested without current round finding", verdict: VerdictChangesRequested, wantCode: "actionable_review_finding_required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			service := newTestService(store, &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
				42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview},
			}})
			round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
				RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.createOpen {
				if _, err := service.CreateFinding(ctx, round.ID, CreateReviewFindingRequest{
					CreatedBy: "reviewer", Category: CategoryBlockingBug, Summary: "Still open",
				}); err != nil {
					t.Fatal(err)
				}
			}
			_, err = service.FinalizeReview(ctx, FinalizeReviewRequest{
				ReviewRoundID: round.ID, Verdict: test.verdict, DecidedBy: "reviewer",
			})
			var serviceErr *ServiceError
			if !errors.As(err, &serviceErr) || serviceErr.Code() != test.wantCode {
				t.Fatalf("FinalizeReview() error = %T %v, want code %s", err, err, test.wantCode)
			}
		})
	}
}

func TestFinalizeReviewResumesMessageFailureAndResponseLoss(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name              string
		configureMessages func(*fakeMessages)
		wantFirstMessages int
	}{
		{
			name: "failure before append",
			configureMessages: func(messages *fakeMessages) {
				messages.failAppend = true
			},
		},
		{
			name: "response lost after append",
			configureMessages: func(messages *fakeMessages) {
				messages.failAfterAppend = true
			},
			wantFirstMessages: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			messages := &fakeMessages{}
			test.configureMessages(messages)
			tasks := &fakeTasks{tasks: map[int64]TaskContext{
				42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview},
			}}
			service := newTestService(store, messages, tasks)
			round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
				RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
			})
			if err != nil {
				t.Fatal(err)
			}
			req := FinalizeReviewRequest{ReviewRoundID: round.ID, Verdict: VerdictLooksGood, DecidedBy: "reviewer"}
			pending, err := service.FinalizeReview(ctx, req)
			if err == nil || pending.Finalization.State != FinalizationStateRetryableError ||
				pending.Finalization.LastErrorStep != FinalizationStepPacketDelivery {
				t.Fatalf("first FinalizeReview() = receipt %+v error %v", pending, err)
			}
			if len(messages.appended) != test.wantFirstMessages || len(tasks.statusUpdates) != 0 {
				t.Fatalf("unexpected first side effects: messages=%d tasks=%d", len(messages.appended), len(tasks.statusUpdates))
			}
			messages.failAppend = false
			completed, err := service.FinalizeReview(ctx, req)
			if err != nil {
				t.Fatalf("retry FinalizeReview() error = %v", err)
			}
			if completed.Finalization.ID != pending.Finalization.ID || completed.Finalization.State != FinalizationStateComplete {
				t.Fatalf("retry did not resume finalization: %+v", completed.Finalization)
			}
			if len(messages.appended) != 1 || len(tasks.statusUpdates) != 1 {
				t.Fatalf("retry duplicated side effects: messages=%d tasks=%d", len(messages.appended), len(tasks.statusUpdates))
			}
		})
	}
}

func TestFinalizeReviewResumesTaskFailureAndResponseLoss(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name           string
		configureTasks func(*fakeTasks)
	}{
		{
			name: "failure before task update",
			configureTasks: func(tasks *fakeTasks) {
				tasks.failStatusUpdate = true
			},
		},
		{
			name: "response lost after task update",
			configureTasks: func(tasks *fakeTasks) {
				tasks.failAfterStatusUpdate = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			messages := &fakeMessages{}
			tasks := &fakeTasks{tasks: map[int64]TaskContext{
				42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview},
			}}
			test.configureTasks(tasks)
			service := newTestService(store, messages, tasks)
			round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
				RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
			})
			if err != nil {
				t.Fatal(err)
			}
			req := FinalizeReviewRequest{ReviewRoundID: round.ID, Verdict: VerdictLooksGood, DecidedBy: "reviewer"}
			pending, err := service.FinalizeReview(ctx, req)
			if err == nil || pending.Finalization.PacketPostedAt == nil ||
				pending.Finalization.LastErrorStep != FinalizationStepTaskTransition {
				t.Fatalf("first FinalizeReview() = receipt %+v error %v", pending, err)
			}
			tasks.failStatusUpdate = false
			completed, err := service.FinalizeReview(ctx, req)
			if err != nil {
				t.Fatalf("retry FinalizeReview() error = %v", err)
			}
			if completed.Finalization.State != FinalizationStateComplete || tasks.tasks[42].Status != TaskStatusDone {
				t.Fatalf("retry did not complete: %+v task=%+v", completed.Finalization, tasks.tasks[42])
			}
			if len(messages.appended) != 1 || len(tasks.statusUpdates) != 1 {
				t.Fatalf("retry duplicated side effects: messages=%d tasks=%d", len(messages.appended), len(tasks.statusUpdates))
			}
		})
	}
}

func TestFinalizeReviewConcurrentRetriesAreIdempotent(t *testing.T) {
	for _, loseResponse := range []bool{false, true} {
		name := "normal"
		if loseResponse {
			name = "one task response is lost"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := &synchronizedFinalizationStore{memoryStore: newMemoryStore()}
			messages := &fakeMessages{}
			seedTasks := &fakeTasks{
				tasks: map[int64]TaskContext{
					42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview},
				},
				failStatusUpdate: true,
			}
			service := newTestService(store, messages, seedTasks)
			round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
				RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
			})
			if err != nil {
				t.Fatal(err)
			}
			req := FinalizeReviewRequest{ReviewRoundID: round.ID, Verdict: VerdictLooksGood, DecidedBy: "reviewer"}
			pending, err := service.FinalizeReview(ctx, req)
			if err == nil || pending.Finalization.LastErrorStep != FinalizationStepTaskTransition {
				t.Fatalf("seed FinalizeReview() = receipt %+v error %v", pending, err)
			}

			tasks := newBarrierTasks(loseResponse)
			service.tasks = tasks
			type result struct {
				receipt *ReviewFinalizationReceipt
				err     error
			}
			results := make(chan result, 2)
			for range 2 {
				go func() {
					receipt, finalizeErr := service.FinalizeReview(ctx, req)
					results <- result{receipt: receipt, err: finalizeErr}
				}()
			}
			for range 2 {
				select {
				case <-tasks.entered:
				case <-time.After(time.Second):
					t.Fatal("concurrent finalization did not reach task transition barrier")
				}
			}
			close(tasks.release)

			var successes int
			for range 2 {
				result := <-results
				if result.err == nil {
					successes++
					if result.receipt.Finalization.ID != pending.Finalization.ID {
						t.Fatalf("concurrent retry returned finalization %d, want %d",
							result.receipt.Finalization.ID, pending.Finalization.ID)
					}
				}
			}
			if successes == 0 {
				t.Fatal("all concurrent finalization retries failed")
			}
			completed, err := service.FinalizeReview(ctx, req)
			if err != nil {
				t.Fatalf("settled FinalizeReview() error = %v", err)
			}
			if completed.Finalization.State != FinalizationStateComplete ||
				completed.Finalization.MessageAttempts != 1 ||
				completed.Finalization.TaskTransitionAttempts != 1 {
				t.Fatalf("settled finalization = %+v", completed.Finalization)
			}
			if len(messages.appended) != 1 {
				t.Fatalf("canonical messages = %d, want 1", len(messages.appended))
			}
			if tasks.setCalls != 2 || tasks.historyTransitions != 1 || tasks.task.Status != TaskStatusDone {
				t.Fatalf("task calls=%d history=%d task=%+v", tasks.setCalls, tasks.historyTransitions, tasks.task)
			}
		})
	}
}

func TestFinalizeReviewResumesAfterTaskCheckpointBeforeComplete(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	store.failCompleteOnce = true
	messages := &fakeMessages{}
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview},
	}}
	service := newTestService(store, messages, tasks)
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := FinalizeReviewRequest{ReviewRoundID: round.ID, Verdict: VerdictLooksGood, DecidedBy: "reviewer"}
	pending, err := service.FinalizeReview(ctx, req)
	if err == nil || pending.Finalization.TaskTransitionedAt == nil ||
		pending.Finalization.LastErrorStep != FinalizationStepCompletion {
		t.Fatalf("first FinalizeReview() = receipt %+v error %v", pending, err)
	}
	completed, err := service.FinalizeReview(ctx, req)
	if err != nil {
		t.Fatalf("retry FinalizeReview() error = %v", err)
	}
	if completed.Finalization.State != FinalizationStateComplete {
		t.Fatalf("retry finalization state = %s", completed.Finalization.State)
	}
	if len(messages.appended) != 1 || len(tasks.statusUpdates) != 1 {
		t.Fatalf("completion retry repeated remote work: messages=%d tasks=%d", len(messages.appended), len(tasks.statusUpdates))
	}
}

func TestPostReviewFindingsReusesFinalizationPacketAfterTaskTransition(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview},
	}}
	service := newTestService(store, messages, tasks)
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	})
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := service.FinalizeReview(ctx, FinalizeReviewRequest{
		ReviewRoundID: round.ID, Verdict: VerdictLooksGood, DecidedBy: "reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	reposted, err := service.PostReviewFindings(ctx, "den-services", 42, PostReviewFindingsRequest{
		ReviewRoundID: round.ID, Sender: "repair-operator", RunID: "repair-run",
	})
	if err != nil {
		t.Fatalf("PostReviewFindings() repair error = %v", err)
	}
	if reposted.ID != finalized.Packet.ID || len(messages.appended) != 1 {
		t.Fatalf("repair path created duplicate packet/message: packet=%d want=%d messages=%d",
			reposted.ID, finalized.Packet.ID, len(messages.appended))
	}
}

func TestServiceTaskOnlyReviewMethodsResolveProjectFromTask(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}}
	service := newTestService(store, &fakeMessages{}, tasks)

	round, err := service.CreateRoundForTask(ctx, 42, CreateReviewRoundRequest{
		RequestedBy: "pi", Branch: "task/review", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head",
	})
	if err != nil {
		t.Fatalf("CreateRoundForTask() error = %v", err)
	}
	if round.ProjectID != "den-services" {
		t.Fatalf("round project = %q, want den-services", round.ProjectID)
	}

	rounds, err := service.ListRoundsForTask(ctx, 42)
	if err != nil {
		t.Fatalf("ListRoundsForTask() error = %v", err)
	}
	if len(rounds) != 1 || rounds[0].ID != round.ID {
		t.Fatalf("rounds = %+v, want round %d", rounds, round.ID)
	}

	finding, err := service.CreateFinding(ctx, round.ID, CreateReviewFindingRequest{
		CreatedBy: "pi-reviewer", Category: CategoryAcceptanceGap, Summary: "Needs evidence",
	})
	if err != nil {
		t.Fatalf("CreateFinding() error = %v", err)
	}
	findings, err := service.ListFindingsForTask(ctx, 42, ListFindingsQuery{})
	if err != nil {
		t.Fatalf("ListFindingsForTask() error = %v", err)
	}
	if len(findings) != 1 || findings[0].ID != finding.ID {
		t.Fatalf("findings = %+v, want finding %d", findings, finding.ID)
	}
}

func TestServiceSplitFindingsToFollowUpSkipsBlockingWithoutOverride(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	tasks := &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}}
	service := newTestService(store, &fakeMessages{}, tasks)
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "pi", Branch: "b", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head"})
	if err != nil {
		t.Fatal(err)
	}
	blocking, err := service.CreateFinding(ctx, round.ID, CreateReviewFindingRequest{CreatedBy: "reviewer", Category: CategoryBlockingBug, Summary: "Blocking"})
	if err != nil {
		t.Fatal(err)
	}
	followUp, err := service.CreateFinding(ctx, round.ID, CreateReviewFindingRequest{CreatedBy: "reviewer", Category: CategoryFollowUpCandidate, Summary: "Later"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.SplitFindingsToFollowUp(ctx, "den-services", 42, SplitFindingsRequest{
		FindingIDs: []int64{blocking.ID, followUp.ID}, SplitBy: "pi", IdempotencyKey: "split-1",
	})
	if err != nil {
		t.Fatalf("SplitFindingsToFollowUp() error = %v", err)
	}
	if result.FollowUpTaskID == 0 || len(result.SplitFindings) != 1 || len(result.SkippedFindings) != 1 {
		t.Fatalf("unexpected split result: %+v", result)
	}
	if result.SplitFindings[0].Status != StatusSplitToFollowUp || result.SkippedFindings[0].ID != blocking.ID {
		t.Fatalf("wrong findings split/skipped: %+v", result)
	}
	if len(tasks.created) != 1 {
		t.Fatalf("expected one follow-up task, got %d", len(tasks.created))
	}
}

func TestPacketValidationAcceptsMarkdownAndRejectsBadContext(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := newTestService(store, &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}})
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "pi", Branch: "b", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head"})
	if err != nil {
		t.Fatal(err)
	}
	markdown := `---
schema: den_review_packet
schema_version: 1
packet_kind: review_findings
project_id: den-services
task_id: 42
sender: pi-reviewer
review_round_id: ` + itoa(round.ID) + `
reviewed_head_commit: head
verdict: changes_requested
verify:
  - id: reviewed_head_matches_round
    checked: true
---
# Findings

One finding.`
	packet, err := service.ValidatePacketMarkdown(ctx, "den-services", 42, markdown)
	if err != nil {
		t.Fatalf("ValidatePacketMarkdown() error = %v", err)
	}
	if packet.ValidationStatus != "valid" || packet.PacketKind != PacketKindReviewFindings {
		t.Fatalf("packet not valid: %+v", packet)
	}

	stale := replace(markdown, "reviewed_head_commit: head", "reviewed_head_commit: stale")
	_, err = service.ValidatePacketMarkdown(ctx, "den-services", 42, stale)
	if err == nil {
		t.Fatal("expected stale reviewed head rejection")
	}
	var serviceError *ServiceError
	if !errors.As(err, &serviceError) || serviceError.Field() != "reviewed_head_commit" || serviceError.DocsRef() == "" {
		t.Fatalf("expected field/docs validation error, got %T %v", err, err)
	}
}

func TestPostPacketMarkdownStoresPacketAndAppendsMessage(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}})
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "pi", Branch: "b", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head"})
	if err != nil {
		t.Fatal(err)
	}
	markdown := `---
schema: den_review_packet
schema_version: 1
packet_kind: completion_evidence
project_id: den-services
task_id: 42
sender: pi-reviewer
review_round_id: ` + itoa(round.ID) + `
reviewed_head_commit: head
verdict: looks_good
verify:
  - id: completion_refs_checked
    checked: true
---
# Completion Evidence

Looks good.`
	packet, err := service.PostPacketMarkdown(ctx, "den-services", 42, PostPacketMarkdownRequest{Markdown: markdown, IdempotencyKey: "packet-1"})
	if err != nil {
		t.Fatalf("PostPacketMarkdown() error = %v", err)
	}
	if packet.ID == 0 || packet.MessageID == nil || packet.ValidationStatus != "accepted" {
		t.Fatalf("packet not stored/accepted: %+v", packet)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "review_approval" {
		t.Fatalf("completion packet did not append approval message: %+v", messages.appended)
	}
}

func TestPostPacketMarkdownIdempotencyDoesNotAppendDuplicateMessages(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}})
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "pi", Branch: "b", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head"})
	if err != nil {
		t.Fatal(err)
	}
	req := PostPacketMarkdownRequest{Markdown: completionPacketMarkdown(round.ID), IdempotencyKey: "packet-1"}
	first, err := service.PostPacketMarkdown(ctx, "den-services", 42, req)
	if err != nil {
		t.Fatalf("first PostPacketMarkdown() error = %v", err)
	}
	second, err := service.PostPacketMarkdown(ctx, "den-services", 42, req)
	if err != nil {
		t.Fatalf("second PostPacketMarkdown() error = %v", err)
	}
	if first.ID != second.ID || second.ValidationStatus != PacketStatusAccepted {
		t.Fatalf("retry did not return accepted existing packet: first=%+v second=%+v", first, second)
	}
	if len(messages.appended) != 1 {
		t.Fatalf("retry appended duplicate messages: %d", len(messages.appended))
	}
}

func TestPostPacketMarkdownMessageFailureResumesWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{failAppend: true}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}})
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "pi", Branch: "b", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head"})
	if err != nil {
		t.Fatal(err)
	}
	req := PostPacketMarkdownRequest{Markdown: completionPacketMarkdown(round.ID), IdempotencyKey: "packet-1"}
	pending, err := service.PostPacketMarkdown(ctx, "den-services", 42, req)
	if err == nil {
		t.Fatal("expected message append failure")
	}
	if pending == nil || pending.ValidationStatus != PacketStatusPendingMessageAppend || pending.MessageID != nil {
		t.Fatalf("expected pending packet without message id, got %+v", pending)
	}
	messages.failAppend = false
	retry, err := service.PostPacketMarkdown(ctx, "den-services", 42, req)
	if err != nil {
		t.Fatalf("retry should resume pending packet: %v", err)
	}
	if retry.ID != pending.ID || retry.ValidationStatus != PacketStatusAccepted || retry.MessageID == nil {
		t.Fatalf("retry did not return existing pending packet: %+v", retry)
	}
	if len(messages.appended) != 1 {
		t.Fatalf("retry appended wrong message count: %d", len(messages.appended))
	}
}

func TestRegisterGitHubCheckGateRecordsPassEvidence(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	github := &fakeGitHubChecks{result: GitHubCheckResult{
		Status:  GitHubCheckGateStatusPassed,
		Summary: "All required GitHub checks passed.",
		CheckRuns: []GitHubCheckRun{
			{Name: "go test", Status: "completed", Conclusion: "success", URL: "https://github.test/run/1"},
		},
	}}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})
	service.ConfigureGitHubChecks(github, GitHubCheckOptions{DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: time.Minute})

	gate, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"go test"}, RequestedBy: "codex",
	})
	if err != nil {
		t.Fatalf("RegisterGitHubCheckGate() error = %v", err)
	}
	if gate.Status != GitHubCheckGateStatusPassed || gate.CompletedAt == nil {
		t.Fatalf("gate not passed: %+v", gate)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "github_checks_passed" {
		t.Fatalf("pass evidence message not appended: %+v", messages.appended)
	}
	if messages.appended[0].Sender != "den-review" || messages.appended[0].Metadata["requested_by"] != "codex" {
		t.Fatalf("evidence authorship/metadata = %+v", messages.appended[0])
	}
	if !strings.Contains(messages.appended[0].Content, "https://github.test/run/1") {
		t.Fatalf("message missing check run URL: %s", messages.appended[0].Content)
	}
}

func TestRegisterGitHubCheckGatePromotesTaskToReviewRegardlessOfCurrentStatus(t *testing.T) {
	statuses := []string{"planned", "in_progress", "review", "blocked", "done", "cancelled"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			tasks := &fakeTasks{tasks: map[int64]TaskContext{
				42: {ID: 42, ProjectID: "den-services", Title: "GitHub gate", Status: status, Priority: 1},
			}}
			service := newTestService(newMemoryStore(), &fakeMessages{}, tasks)

			if _, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
				Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
				RequiredChecks: []string{"go test"}, RequestedBy: "codex",
			}); err != nil {
				t.Fatalf("RegisterGitHubCheckGate() error = %v", err)
			}
			if got := tasks.tasks[42].Status; got != TaskStatusReview {
				t.Fatalf("task status = %q, want %q", got, TaskStatusReview)
			}
			if status == TaskStatusReview {
				if len(tasks.statusUpdates) != 0 {
					t.Fatalf("status updates = %+v, want no redundant review transition", tasks.statusUpdates)
				}
			} else if len(tasks.statusUpdates) != 1 || tasks.statusUpdates[0].Agent != "codex" {
				t.Fatalf("status updates = %+v, want one codex-authored review transition", tasks.statusUpdates)
			}
		})
	}
}

func TestTerminalGitHubCheckGateEventIsIdempotentAndResumable(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := newTestService(store, &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})
	service.ConfigureGitHubChecks(&fakeGitHubChecks{result: GitHubCheckResult{
		Status: GitHubCheckGateStatusPassed, Summary: "passed", TerminalReason: GitHubCheckTerminalReasonChecksPassed,
		ObservedCheckRuns: []GitHubCheckRun{{Name: "go test", Status: "completed", Conclusion: "success", DetailsURL: "https://github.test/check/1"}},
	}}, GitHubCheckOptions{DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: time.Minute})

	gate, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"go test"}, RequestedBy: "codex", AgentProfile: "codex-cli",
		AgentInstanceID: "agent-1", SessionKey: "session-1",
	})
	if err != nil {
		t.Fatalf("RegisterGitHubCheckGate() error = %v", err)
	}
	if _, changed, err := store.CompleteGitHubCheckGate(ctx, gate.ID, GitHubCheckGateStatusPassed, GitHubCheckResult{}, fixedReviewTestTime()); err != nil || changed {
		t.Fatalf("duplicate completion changed=%v err=%v", changed, err)
	}
	page, err := service.WaitGitHubCheckGateEvents(ctx, ListGitHubCheckGateEventsQuery{ProjectID: "den-services", TaskID: 42}, 0)
	if err != nil {
		t.Fatalf("WaitGitHubCheckGateEvents() error = %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].GateID != gate.ID || page.Events[0].SchemaVersion != 1 || page.Events[0].SessionKey != "session-1" {
		t.Fatalf("terminal event = %+v", page)
	}
	if got := page.Events[0].ObservedCheckRuns[0].DetailsURL; got != "https://github.test/check/1" {
		t.Fatalf("observed check URL = %q", got)
	}
	resumed, err := service.WaitGitHubCheckGateEvents(ctx, ListGitHubCheckGateEventsQuery{
		ProjectID: "den-services", AfterID: page.NextCursor,
	}, 0)
	if err != nil || len(resumed.Events) != 0 || resumed.NextCursor != page.NextCursor {
		t.Fatalf("resumed page = %+v err=%v", resumed, err)
	}
}

func TestWaitGitHubCheckGateEventsHasBoundedEmptyWait(t *testing.T) {
	service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{})
	service.ConfigureGitHubChecks(nil, GitHubCheckOptions{EventWaitMax: 20 * time.Millisecond, EventWaitPoll: time.Millisecond})
	started := time.Now()
	page, err := service.WaitGitHubCheckGateEvents(context.Background(), ListGitHubCheckGateEventsQuery{
		ProjectID: "den-services", AfterID: 9,
	}, time.Second)
	if err != nil {
		t.Fatalf("WaitGitHubCheckGateEvents() error = %v", err)
	}
	if !page.TimedOut || page.NextCursor != 9 || time.Since(started) > 250*time.Millisecond {
		t.Fatalf("bounded page = %+v elapsed=%s", page, time.Since(started))
	}
}

func TestWaitForGitHubCheckGateReturnsProgressWithoutMutatingGate(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := newTestService(store, &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Status: TaskStatusInProgress},
	}})
	sha := "0123456789abcdef0123456789abcdef01234567"
	nextPollAt := fixedReviewTestTime().Add(30 * time.Second)
	gate, _, err := store.RegisterGitHubCheckGate(ctx, &GitHubCheckGate{
		ProjectID: "den-services", TaskID: 42, Repository: "owner/repo", CommitSHA: sha, Ref: "main",
		RequiredChecks: []string{"Verify"}, Status: GitHubCheckGateStatusPending, RequestedBy: "codex",
		TimeoutAt: fixedReviewTestTime().Add(time.Hour), PollIntervalSeconds: 30, NextPollAt: nextPollAt,
		CreatedAt: fixedReviewTestTime(), UpdatedAt: fixedReviewTestTime(),
	}, fixedReviewTestTime())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.WaitForGitHubCheckGate(ctx, "den-services", 42, sha, 17, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Terminal || !receipt.TimedOut || receipt.NextCursor != 17 || receipt.Gate.ID != gate.ID {
		t.Fatalf("receipt = %+v", receipt)
	}
	stored := store.githubCheckGates[gate.ID]
	if !stored.NextPollAt.Equal(nextPollAt) || !stored.TimeoutAt.Equal(gate.TimeoutAt) || stored.PollIntervalSeconds != 30 {
		t.Fatalf("bounded wait mutated gate: %+v", stored)
	}
}

func TestRegisterGitHubCheckGateSupersedesOlderPendingSHA(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})
	firstSHA := "0123456789abcdef0123456789abcdef01234567"
	secondSHA := "abcdef0123456789abcdef0123456789abcdef01"

	first, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: firstSHA, Ref: "main", RequiredChecks: []string{"go test"}, RequestedBy: "codex",
	})
	if err != nil {
		t.Fatalf("first RegisterGitHubCheckGate() error = %v", err)
	}
	second, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: secondSHA, Ref: "main", RequiredChecks: []string{"go test"}, RequestedBy: "codex",
	})
	if err != nil {
		t.Fatalf("second RegisterGitHubCheckGate() error = %v", err)
	}
	if second.Status != GitHubCheckGateStatusPending {
		t.Fatalf("second gate status = %s", second.Status)
	}
	old := store.githubCheckGates[first.ID]
	if old.Status != GitHubCheckGateStatusSuperseded {
		t.Fatalf("older gate was not superseded: %+v", old)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "github_checks_superseded" {
		t.Fatalf("superseded message not appended: %+v", messages.appended)
	}
	if old.EvidenceMessageStatus != GitHubCheckEvidenceStatusPosted {
		t.Fatalf("superseded evidence was not marked posted: %+v", old)
	}
	page, err := service.WaitGitHubCheckGateEvents(ctx, ListGitHubCheckGateEventsQuery{ProjectID: "den-services", TaskID: 42}, 0)
	if err != nil || len(page.Events) != 1 || page.Events[0].GateID != first.ID || page.Events[0].TerminalReason != GitHubCheckTerminalReasonSuperseded {
		t.Fatalf("supersession event = %+v err=%v", page, err)
	}
}

func TestRegisterGitHubCheckGateSupersedesNoCommit422WithNormalizedTerminalRuns(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	invalidSHA := "0123456789abcdef0123456789abcdef01234567"
	correctedSHA := "abcdef0123456789abcdef0123456789abcdef01"
	github := &fakeGitHubChecks{errorsBySHA: map[string]error{
		invalidSHA: &GitHubHTTPError{Status: "422 Unprocessable Entity", StatusCode: http.StatusUnprocessableEntity, Message: "No commit found for SHA"},
	}}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})
	service.ConfigureGitHubChecks(github, GitHubCheckOptions{DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: time.Minute})

	first, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: invalidSHA, Ref: "main", RequiredChecks: []string{"Verify"}, RequestedBy: "codex",
	})
	if err != nil {
		t.Fatalf("invalid-SHA RegisterGitHubCheckGate() error = %v", err)
	}
	if err := service.PollGitHubCheckGates(ctx, 10); err != nil {
		t.Fatalf("PollGitHubCheckGates() error = %v", err)
	}
	if stored := store.githubCheckGates[first.ID]; stored.CheckRuns == nil {
		t.Fatalf("422 retry left nullable check runs: %+v", stored)
	}

	if _, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: correctedSHA, Ref: "main", RequiredChecks: []string{"Verify"}, RequestedBy: "codex",
	}); err != nil {
		t.Fatalf("corrected-SHA RegisterGitHubCheckGate() error = %v", err)
	}
	if old := store.githubCheckGates[first.ID]; old.Status != GitHubCheckGateStatusSuperseded {
		t.Fatalf("invalid SHA gate was not superseded: %+v", old)
	}
	page, err := service.WaitGitHubCheckGateEvents(ctx, ListGitHubCheckGateEventsQuery{ProjectID: "den-services", TaskID: 42}, 0)
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("supersession events = %+v err=%v", page, err)
	}
	if page.Events[0].CheckRuns == nil || len(page.Events[0].CheckRuns) != 0 {
		t.Fatalf("supersession terminal event check runs = %#v, want non-nil empty array", page.Events[0].CheckRuns)
	}
	foundSupersessionEvidence := false
	for _, message := range messages.appended {
		if message.Intent == "github_checks_superseded" {
			foundSupersessionEvidence = true
			break
		}
	}
	if !foundSupersessionEvidence {
		t.Fatalf("supersession evidence was not posted: %+v", messages.appended)
	}
}

func TestRegisterGitHubCheckGateClampsShortPollInterval(t *testing.T) {
	ctx := context.Background()
	service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})

	shortInterval := 30
	gate, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"go test"}, RequestedBy: "codex", PollIntervalSeconds: &shortInterval,
	})
	if err != nil {
		t.Fatalf("RegisterGitHubCheckGate() error = %v", err)
	}
	if gate.PollIntervalSeconds != int(defaultGitHubCheckPollInterval.Seconds()) {
		t.Fatalf("PollIntervalSeconds = %d, want %d", gate.PollIntervalSeconds, int(defaultGitHubCheckPollInterval.Seconds()))
	}
}

func TestRegisterGitHubCheckGateAcceptsLongBoundedTimeout(t *testing.T) {
	ctx := context.Background()
	service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})

	maxTimeout := int(DefaultGitHubCheckOptions().MaxTimeout.Seconds())
	gate, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"go test"}, RequestedBy: "codex", TimeoutSeconds: &maxTimeout,
	})
	if err != nil {
		t.Fatalf("RegisterGitHubCheckGate() max timeout error = %v", err)
	}
	if want := fixedReviewTestTime().Add(DefaultGitHubCheckOptions().MaxTimeout); !gate.TimeoutAt.Equal(want) {
		t.Fatalf("TimeoutAt = %s, want %s", gate.TimeoutAt, want)
	}

	tooLong := maxTimeout + 1
	if _, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "abcdef0123456789abcdef0123456789abcdef01", Ref: "main",
		RequiredChecks: []string{"go test"}, RequestedBy: "codex", TimeoutSeconds: &tooLong,
	}); err == nil {
		t.Fatal("expected timeout above max to be rejected")
	}
}

func TestRegisterGitHubCheckGateRecordsFailureEvidence(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	github := &fakeGitHubChecks{result: GitHubCheckResult{
		Status:         GitHubCheckGateStatusFailed,
		Summary:        "One or more required GitHub checks failed.",
		FailureSummary: "Failed checks: go test (failure)",
		CheckRuns: []GitHubCheckRun{
			{Name: "go test", Status: "completed", Conclusion: "failure", URL: "https://github.test/run/2"},
		},
	}}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})
	service.ConfigureGitHubChecks(github, GitHubCheckOptions{DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: time.Minute})

	gate, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"go test"}, RequestedBy: "codex",
	})
	if err != nil {
		t.Fatalf("RegisterGitHubCheckGate() error = %v", err)
	}
	if gate.Status != GitHubCheckGateStatusFailed || gate.EvidenceMessageStatus != GitHubCheckEvidenceStatusPosted {
		t.Fatalf("gate failure evidence not recorded: %+v", gate)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "github_checks_failed" {
		t.Fatalf("failure evidence message not appended: %+v", messages.appended)
	}
	if !strings.Contains(messages.appended[0].Content, "Failed checks: go test") || !strings.Contains(messages.appended[0].Content, "https://github.test/run/2") {
		t.Fatalf("failure message missing summary or URL: %s", messages.appended[0].Content)
	}
}

func TestPollGitHubCheckGateFailsInvalidRequiredNamesAfterGrace(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	now := fixedReviewTestTime()
	service := NewService(store, NoopProjectValidator{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}}, messages, func() time.Time { return now })
	github := &fakeGitHubChecks{result: GitHubCheckResult{
		Status:    GitHubCheckGateStatusPending,
		Summary:   "Waiting for required checks: CI. Observed check runs: Verify Offline, Verify Postgres Backend",
		CheckRuns: []GitHubCheckRun{{Name: "CI", Status: GitHubCheckGateStatusPending}},
		ObservedCheckRuns: []GitHubCheckRun{
			{Name: "Verify Offline", Status: "completed", Conclusion: "success", URL: "https://github.test/offline"},
			{Name: "Verify Postgres Backend", Status: "completed", Conclusion: "success", URL: "https://github.test/postgres"},
		},
		MissingRequiredChecks: []string{"CI"}, AllObservedChecksTerminal: true,
	}}
	service.ConfigureGitHubChecks(github, GitHubCheckOptions{
		DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour,
		PollInterval: 30 * time.Second, MissingCheckGrace: 2 * time.Minute,
	})

	gate, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"CI"}, RequestedBy: "codex",
	})
	if err != nil {
		t.Fatalf("RegisterGitHubCheckGate() error = %v", err)
	}
	if gate.Status != GitHubCheckGateStatusPending {
		t.Fatalf("initial gate = %+v", gate)
	}

	now = now.Add(3 * time.Minute)
	if err := service.PollGitHubCheckGates(ctx, 10); err != nil {
		t.Fatalf("PollGitHubCheckGates() error = %v", err)
	}
	updated := store.githubCheckGates[gate.ID]
	if updated.Status != GitHubCheckGateStatusFailed || updated.TerminalReason != GitHubCheckTerminalReasonRequiredChecksMissing {
		t.Fatalf("updated gate = %+v", updated)
	}
	if len(updated.ObservedCheckRuns) != 2 || len(updated.MissingRequiredChecks) != 1 {
		t.Fatalf("diagnostics missing: %+v", updated)
	}
	if len(messages.appended) != 1 || !strings.Contains(messages.appended[0].Content, "Verify Offline") ||
		!strings.Contains(messages.appended[0].Content, GitHubCheckTerminalReasonRequiredChecksMissing) {
		t.Fatalf("evidence = %+v", messages.appended)
	}
}

func TestRegisterGitHubCheckGateRetryDoesNotExtendTimeout(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	now := fixedReviewTestTime()
	service := NewService(store, NoopProjectValidator{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}}, &fakeMessages{}, func() time.Time { return now })
	shortTimeout := 600
	longTimeout := 3600
	req := RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"go test"}, RequestedBy: "codex", TimeoutSeconds: &shortTimeout,
	}
	first, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, req)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	req.TimeoutSeconds = &longTimeout
	second, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.TimeoutAt.Equal(first.TimeoutAt) || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("retry reset durable timing: first=%+v second=%+v", first, second)
	}
}

func TestPollGitHubCheckGateAcceptsRequiredCheckThatRegistersLate(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	now := fixedReviewTestTime()
	github := &fakeGitHubChecks{result: GitHubCheckResult{
		Status: GitHubCheckGateStatusPending, Summary: "Waiting for required checks: Verify Offline",
		CheckRuns:             []GitHubCheckRun{{Name: "Verify Offline", Status: GitHubCheckGateStatusPending}},
		ObservedCheckRuns:     []GitHubCheckRun{{Name: "setup", Status: "completed", Conclusion: "success"}},
		MissingRequiredChecks: []string{"Verify Offline"}, AllObservedChecksTerminal: true,
	}}
	service := NewService(store, NoopProjectValidator{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}}, messages, func() time.Time { return now })
	service.ConfigureGitHubChecks(github, GitHubCheckOptions{
		DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour,
		PollInterval: 30 * time.Second, MissingCheckGrace: 2 * time.Minute,
	})

	gate, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"Verify Offline"}, RequestedBy: "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	github.result = GitHubCheckResult{
		Status: GitHubCheckGateStatusPassed, Summary: "All required GitHub checks passed.",
		TerminalReason: GitHubCheckTerminalReasonChecksPassed,
		CheckRuns:      []GitHubCheckRun{{Name: "Verify Offline", Status: "completed", Conclusion: "success"}},
		ObservedCheckRuns: []GitHubCheckRun{
			{Name: "setup", Status: "completed", Conclusion: "success"},
			{Name: "Verify Offline", Status: "completed", Conclusion: "success"},
		},
	}
	if err := service.PollGitHubCheckGates(ctx, 10); err != nil {
		t.Fatal(err)
	}
	updated := store.githubCheckGates[gate.ID]
	if updated.Status != GitHubCheckGateStatusPassed || len(updated.MissingRequiredChecks) != 0 {
		t.Fatalf("late check did not pass: %+v", updated)
	}
}

func TestPollGitHubCheckGatesRecordsTimeoutEvidence(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})
	service.ConfigureGitHubChecks(&fakeGitHubChecks{result: GitHubCheckResult{Status: GitHubCheckGateStatusPending}}, GitHubCheckOptions{
		DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: time.Minute,
	})
	pastTimeout := -1
	gate, _, err := store.RegisterGitHubCheckGate(ctx, &GitHubCheckGate{
		ProjectID: "den-services", TaskID: 42, Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567",
		Ref: "main", RequiredChecks: []string{"go test"}, Status: GitHubCheckGateStatusPending, RequestedBy: "codex",
		TimeoutAt: fixedReviewTestTime().Add(-time.Minute), PollIntervalSeconds: 60, NextPollAt: fixedReviewTestTime().Add(time.Duration(pastTimeout) * time.Minute),
		CreatedAt: fixedReviewTestTime(), UpdatedAt: fixedReviewTestTime(),
	}, fixedReviewTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PollGitHubCheckGates(ctx, 10); err != nil {
		t.Fatalf("PollGitHubCheckGates() error = %v", err)
	}
	updated := store.githubCheckGates[gate.ID]
	if updated.Status != GitHubCheckGateStatusTimedOut || updated.EvidenceMessageStatus != GitHubCheckEvidenceStatusPosted {
		t.Fatalf("timeout evidence not recorded: %+v", updated)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "github_checks_timeout" {
		t.Fatalf("timeout message not appended: %+v", messages.appended)
	}
}

func TestPollGitHubCheckGatesBacksOffGitHubRateLimit(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	resetAt := fixedReviewTestTime().Add(42 * time.Minute)
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})
	service.ConfigureGitHubChecks(&fakeGitHubChecks{err: &GitHubHTTPError{
		Status:                "403 Forbidden",
		StatusCode:            http.StatusForbidden,
		Message:               "API rate limit exceeded",
		RateLimitRemaining:    0,
		RateLimitRemainingSet: true,
		RateLimitReset:        resetAt,
		RateLimitResetSet:     true,
	}}, GitHubCheckOptions{DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: 30 * time.Second})
	gate, _, err := store.RegisterGitHubCheckGate(ctx, &GitHubCheckGate{
		ProjectID: "den-services", TaskID: 42, Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567",
		Ref: "main", RequiredChecks: []string{"go test"}, Status: GitHubCheckGateStatusPending, RequestedBy: "codex",
		TimeoutAt: fixedReviewTestTime().Add(2 * time.Hour), PollIntervalSeconds: 30, NextPollAt: fixedReviewTestTime().Add(-time.Minute),
		CreatedAt: fixedReviewTestTime(), UpdatedAt: fixedReviewTestTime(),
	}, fixedReviewTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PollGitHubCheckGates(ctx, 10); err != nil {
		t.Fatalf("PollGitHubCheckGates() error = %v", err)
	}
	updated := store.githubCheckGates[gate.ID]
	if updated.Status != GitHubCheckGateStatusPending {
		t.Fatalf("gate should remain pending after GitHub throttle, got %+v", updated)
	}
	if updated.LastCheckedAt == nil || !updated.LastCheckedAt.Equal(fixedReviewTestTime()) {
		t.Fatalf("last_checked_at not recorded: %+v", updated.LastCheckedAt)
	}
	if want := resetAt.Add(time.Minute); !updated.NextPollAt.Equal(want) {
		t.Fatalf("next_poll_at = %s, want %s", updated.NextPollAt, want)
	}
	if !strings.Contains(updated.Summary, "403 Forbidden") || !strings.Contains(updated.Summary, "API rate limit exceeded") {
		t.Fatalf("summary did not preserve GitHub throttle details: %s", updated.Summary)
	}
	if len(messages.appended) != 0 {
		t.Fatalf("throttled pending gate should not append evidence: %+v", messages.appended)
	}
}

func TestFrequentWatcherScansPreservePerGateGitHubPollCadence(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	github := &fakeGitHubChecks{result: GitHubCheckResult{Status: GitHubCheckGateStatusPending}}
	service := newTestService(store, &fakeMessages{}, &fakeTasks{})
	service.ConfigureGitHubChecks(github, GitHubCheckOptions{DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: 30 * time.Second})
	gate, _, err := store.RegisterGitHubCheckGate(ctx, &GitHubCheckGate{
		ProjectID: "den-services", TaskID: 42, Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567",
		Ref: "main", RequiredChecks: []string{"go test"}, Status: GitHubCheckGateStatusPending, RequestedBy: "codex",
		TimeoutAt: fixedReviewTestTime().Add(time.Hour), PollIntervalSeconds: 30, NextPollAt: fixedReviewTestTime(),
		CreatedAt: fixedReviewTestTime(), UpdatedAt: fixedReviewTestTime(),
	}, fixedReviewTestTime())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PollGitHubCheckGates(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := service.PollGitHubCheckGates(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if github.calls != 1 || !store.githubCheckGates[gate.ID].NextPollAt.Equal(fixedReviewTestTime().Add(30*time.Second)) {
		t.Fatalf("calls=%d gate=%+v", github.calls, store.githubCheckGates[gate.ID])
	}
}

func TestPollGitHubCheckGatesDrainsMultipleBatchesAndIsolatesTransportFailure(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	failedSHA := "1111111111111111111111111111111111111111"
	github := &fakeGitHubChecks{
		result:      GitHubCheckResult{Status: GitHubCheckGateStatusPassed, TerminalReason: GitHubCheckTerminalReasonChecksPassed},
		errorsBySHA: map[string]error{failedSHA: errors.New("temporary transport failure")},
	}
	service := newTestService(store, &fakeMessages{}, &fakeTasks{})
	service.ConfigureGitHubChecks(github, GitHubCheckOptions{DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: 30 * time.Second})
	shas := []string{failedSHA, "2222222222222222222222222222222222222222", "3333333333333333333333333333333333333333"}
	for i, sha := range shas {
		_, _, err := store.RegisterGitHubCheckGate(ctx, &GitHubCheckGate{
			ProjectID: "den-services", TaskID: int64(100 + i), Repository: "owner/repo", CommitSHA: sha,
			Ref: "main", RequiredChecks: []string{"go test"}, Status: GitHubCheckGateStatusPending, RequestedBy: "codex",
			TimeoutAt: fixedReviewTestTime().Add(time.Hour), PollIntervalSeconds: 30, NextPollAt: fixedReviewTestTime(),
			CreatedAt: fixedReviewTestTime(), UpdatedAt: fixedReviewTestTime(),
		}, fixedReviewTestTime())
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := service.PollGitHubCheckGates(ctx, 2); err != nil {
		t.Fatalf("PollGitHubCheckGates() error = %v", err)
	}
	if github.calls != 3 {
		t.Fatalf("GitHub calls = %d, want 3", github.calls)
	}
	var passed, pending int
	for _, gate := range store.githubCheckGates {
		switch gate.Status {
		case GitHubCheckGateStatusPassed:
			passed++
		case GitHubCheckGateStatusPending:
			pending++
			if !strings.Contains(gate.Summary, "temporary transport failure") || !gate.NextPollAt.After(fixedReviewTestTime()) {
				t.Fatalf("transport failure was not durably delayed: %+v", gate)
			}
		}
	}
	if passed != 2 || pending != 1 {
		t.Fatalf("passed=%d pending=%d gates=%+v", passed, pending, store.githubCheckGates)
	}
}

func TestGitHubCheckGateEvidenceAppendFailureIsDurableAndRetried(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{failAppend: true}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusInProgress, Priority: 1},
	}})
	service.ConfigureGitHubChecks(&fakeGitHubChecks{result: GitHubCheckResult{
		Status:  GitHubCheckGateStatusPassed,
		Summary: "All required GitHub checks passed.",
	}}, GitHubCheckOptions{DefaultTimeout: time.Hour, MaxTimeout: 2 * time.Hour, PollInterval: time.Minute})

	gate, err := service.RegisterGitHubCheckGate(ctx, "den-services", 42, RegisterGitHubCheckGateRequest{
		Repository: "owner/repo", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Ref: "main",
		RequiredChecks: []string{"go test"}, RequestedBy: "codex",
	})
	if err == nil {
		t.Fatal("expected message append failure")
	}
	if gate == nil || gate.Status != GitHubCheckGateStatusPassed {
		t.Fatalf("terminal gate should still be durable, got %+v", gate)
	}
	stored := store.githubCheckGates[gate.ID]
	if stored.EvidenceMessageStatus != GitHubCheckEvidenceStatusError || stored.EvidenceMessageError == "" {
		t.Fatalf("append failure was not recorded durably: %+v", stored)
	}

	messages.failAppend = false
	if err := service.PollGitHubCheckGates(ctx, 10); err != nil {
		t.Fatalf("PollGitHubCheckGates() retry error = %v", err)
	}
	if stored = store.githubCheckGates[gate.ID]; stored.EvidenceMessageStatus != GitHubCheckEvidenceStatusPosted || stored.EvidenceMessageID == nil {
		t.Fatalf("evidence retry did not mark posted: %+v", stored)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "github_checks_passed" {
		t.Fatalf("retry did not append pass evidence: %+v", messages.appended)
	}
}

func newTestService(store ReviewStore, messages MessageClient, tasks TaskClient) *Service {
	return NewService(store, NoopProjectValidator{}, tasks, messages, func() time.Time {
		return fixedReviewTestTime()
	})
}

func fixedReviewTestTime() time.Time {
	return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
}

type fakeTasks struct {
	tasks                        map[int64]TaskContext
	created                      []CreateFollowUpTaskRequest
	statusUpdates                []fakeTaskStatusUpdate
	failStatusUpdate             bool
	failAfterStatusUpdate        bool
	statusBeforeReviewTransition string
}

type fakeTaskStatusUpdate struct {
	Agent  string
	Status string
}

func (f fakeTasks) GetTask(_ context.Context, taskID int64) (TaskContext, error) {
	if task, ok := f.tasks[taskID]; ok {
		return task, nil
	}
	return TaskContext{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
}

func (f fakeTasks) GetTaskContext(_ context.Context, projectID string, taskID int64) (TaskContext, error) {
	if task, ok := f.tasks[taskID]; ok {
		return task, nil
	}
	return TaskContext{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
}

func (f *fakeTasks) SetTaskStatus(_ context.Context, projectID string, taskID int64, agent string, status string) (TaskContext, error) {
	task, ok := f.tasks[taskID]
	if !ok || task.ProjectID != projectID {
		return TaskContext{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
	}
	if f.failStatusUpdate {
		return TaskContext{}, errors.New("task update failed")
	}
	task.Status = status
	f.tasks[taskID] = task
	f.statusUpdates = append(f.statusUpdates, fakeTaskStatusUpdate{Agent: agent, Status: status})
	if f.failAfterStatusUpdate {
		f.failAfterStatusUpdate = false
		return TaskContext{}, errors.New("task update response lost")
	}
	return task, nil
}

func (f *fakeTasks) TransitionTaskToReview(
	_ context.Context,
	projectID string,
	taskID int64,
	agent string,
) (TaskReviewTransition, error) {
	task, ok := f.tasks[taskID]
	if !ok || task.ProjectID != projectID {
		return TaskReviewTransition{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
	}
	if f.statusBeforeReviewTransition != "" {
		task.Status = f.statusBeforeReviewTransition
		f.tasks[taskID] = task
		f.statusBeforeReviewTransition = ""
	}
	if task.Status == TaskStatusReview {
		return TaskReviewTransition{Task: task, Transition: TaskTransitionAlreadySatisfied}, nil
	}
	if task.Status != TaskStatusInProgress {
		return TaskReviewTransition{}, NewServiceError(
			fmt.Errorf("task must currently be in_progress or review: current status is %s", task.Status),
			"review_transition_ineligible",
			http.StatusConflict,
		)
	}
	if f.failStatusUpdate {
		return TaskReviewTransition{}, errors.New("task update failed")
	}
	task.Status = TaskStatusReview
	f.tasks[taskID] = task
	f.statusUpdates = append(f.statusUpdates, fakeTaskStatusUpdate{Agent: agent, Status: TaskStatusReview})
	if f.failAfterStatusUpdate {
		f.failAfterStatusUpdate = false
		return TaskReviewTransition{}, errors.New("task update response lost")
	}
	return TaskReviewTransition{Task: task, Transition: TaskTransitionApplied}, nil
}

func (f *fakeTasks) CreateFollowUpTask(_ context.Context, projectID string, req CreateFollowUpTaskRequest) (CreatedTask, error) {
	f.created = append(f.created, req)
	return CreatedTask{ID: int64(9000 + len(f.created)), ProjectID: projectID, Title: req.Title, Status: "planned"}, nil
}

type fakeMessages struct {
	appended         []AppendMessageRequest
	failAppend       bool
	failAfterAppend  bool
	messagesByPacket map[string]AppendedMessage
}

type synchronizedFinalizationStore struct {
	*memoryStore
	mu sync.Mutex
}

type synchronizedRequestStore struct {
	*memoryStore
	mu sync.Mutex
}

func (s *synchronizedRequestStore) CreateRound(ctx context.Context, round *ReviewRound) (*ReviewRound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.CreateRound(ctx, round)
}

func (s *synchronizedRequestStore) ListRounds(ctx context.Context, projectID string, taskID int64) ([]*ReviewRound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.ListRounds(ctx, projectID, taskID)
}

func (s *synchronizedRequestStore) StorePacket(ctx context.Context, packet *ReviewPacket) (*ReviewPacket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.StorePacket(ctx, packet)
}

func (s *synchronizedRequestStore) GetPacketByIdempotency(
	ctx context.Context,
	projectID string,
	idempotencyKey string,
) (*ReviewPacket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.GetPacketByIdempotency(ctx, projectID, idempotencyKey)
}

type synchronizedMessages struct {
	*fakeMessages
	mu sync.Mutex
}

func (s *synchronizedMessages) AppendTaskMessage(
	ctx context.Context,
	projectID string,
	req AppendMessageRequest,
) (AppendedMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeMessages.AppendTaskMessage(ctx, projectID, req)
}

func (s *synchronizedFinalizationStore) GetFinalizationByRound(ctx context.Context, roundID int64) (*ReviewFinalization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.GetFinalizationByRound(ctx, roundID)
}

func (s *synchronizedFinalizationStore) MarkFinalizationPacketPosted(
	ctx context.Context,
	id int64,
	messageID int64,
	postedAt time.Time,
) (*ReviewFinalization, *ReviewPacket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.MarkFinalizationPacketPosted(ctx, id, messageID, postedAt)
}

func (s *synchronizedFinalizationStore) MarkFinalizationTaskTransitioned(
	ctx context.Context,
	id int64,
	transitionedAt time.Time,
) (*ReviewFinalization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.MarkFinalizationTaskTransitioned(ctx, id, transitionedAt)
}

func (s *synchronizedFinalizationStore) CompleteFinalization(
	ctx context.Context,
	id int64,
	completedAt time.Time,
) (*ReviewFinalization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.CompleteFinalization(ctx, id, completedAt)
}

func (s *synchronizedFinalizationStore) RecordFinalizationError(
	ctx context.Context,
	id int64,
	step string,
	message string,
	attemptedAt time.Time,
) (*ReviewFinalization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryStore.RecordFinalizationError(ctx, id, step, message, attemptedAt)
}

type barrierTasks struct {
	mu                 sync.Mutex
	task               TaskContext
	entered            chan struct{}
	release            chan struct{}
	loseFirstResponse  bool
	responseLost       bool
	setCalls           int
	historyTransitions int
}

func newBarrierTasks(loseFirstResponse bool) *barrierTasks {
	return &barrierTasks{
		task:              TaskContext{ID: 42, ProjectID: "den-services", Status: TaskStatusReview},
		entered:           make(chan struct{}, 2),
		release:           make(chan struct{}),
		loseFirstResponse: loseFirstResponse,
	}
}

func (f *barrierTasks) GetTask(_ context.Context, taskID int64) (TaskContext, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if taskID != f.task.ID {
		return TaskContext{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
	}
	return f.task, nil
}

func (f *barrierTasks) GetTaskContext(_ context.Context, projectID string, taskID int64) (TaskContext, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if taskID != f.task.ID || projectID != f.task.ProjectID {
		return TaskContext{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
	}
	return f.task, nil
}

func (f *barrierTasks) SetTaskStatus(
	ctx context.Context,
	projectID string,
	taskID int64,
	agent string,
	status string,
) (TaskContext, error) {
	f.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return TaskContext{}, ctx.Err()
	case <-f.release:
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if taskID != f.task.ID || projectID != f.task.ProjectID {
		return TaskContext{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
	}
	f.setCalls++
	if f.task.Status != status {
		f.task.Status = status
		f.historyTransitions++
	}
	if f.loseFirstResponse && !f.responseLost {
		f.responseLost = true
		return TaskContext{}, errors.New("task update response lost")
	}
	return f.task, nil
}

func (f *barrierTasks) TransitionTaskToReview(
	ctx context.Context,
	projectID string,
	taskID int64,
	_ string,
) (TaskReviewTransition, error) {
	f.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return TaskReviewTransition{}, ctx.Err()
	case <-f.release:
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if taskID != f.task.ID || projectID != f.task.ProjectID {
		return TaskReviewTransition{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
	}
	f.setCalls++
	transition := TaskTransitionAlreadySatisfied
	if f.task.Status == TaskStatusInProgress {
		f.task.Status = TaskStatusReview
		f.historyTransitions++
		transition = TaskTransitionApplied
	} else if f.task.Status != TaskStatusReview {
		return TaskReviewTransition{}, NewServiceError(
			fmt.Errorf("task review transition rejected from %s", f.task.Status),
			"review_transition_ineligible",
			http.StatusConflict,
		)
	}
	if f.loseFirstResponse && !f.responseLost {
		f.responseLost = true
		return TaskReviewTransition{}, errors.New("task update response lost")
	}
	return TaskReviewTransition{Task: f.task, Transition: transition}, nil
}

func (f *barrierTasks) CreateFollowUpTask(
	context.Context,
	string,
	CreateFollowUpTaskRequest,
) (CreatedTask, error) {
	return CreatedTask{}, errors.New("unexpected follow-up task creation")
}

type fakeGitHubChecks struct {
	result             GitHubCheckResult
	err                error
	resultsBySHA       map[string]GitHubCheckResult
	errorsBySHA        map[string]error
	calls              int
	lastRepository     string
	lastCommitSHA      string
	lastRequiredChecks []string
}

func (f *fakeGitHubChecks) CheckCommit(_ context.Context, repository string, commitSHA string, requiredChecks []string) (GitHubCheckResult, error) {
	f.calls++
	f.lastRepository = repository
	f.lastCommitSHA = commitSHA
	f.lastRequiredChecks = append([]string(nil), requiredChecks...)
	if err := f.errorsBySHA[commitSHA]; err != nil {
		return GitHubCheckResult{}, err
	}
	if result, ok := f.resultsBySHA[commitSHA]; ok {
		return result, nil
	}
	return f.result, f.err
}

func (f *fakeMessages) AppendTaskMessage(_ context.Context, projectID string, req AppendMessageRequest) (AppendedMessage, error) {
	if f.failAppend {
		return AppendedMessage{}, errors.New("append failed")
	}
	packetID := fmt.Sprint(req.Metadata["review_packet_id"])
	if packetID != "<nil>" {
		if existing, ok := f.messagesByPacket[packetID]; ok {
			return existing, nil
		}
	}
	f.appended = append(f.appended, req)
	message := AppendedMessage{ID: int64(len(f.appended)), ProjectID: projectID, TaskID: &req.TaskID, Intent: req.Intent}
	if packetID != "<nil>" {
		if f.messagesByPacket == nil {
			f.messagesByPacket = map[string]AppendedMessage{}
		}
		f.messagesByPacket[packetID] = message
	}
	if f.failAfterAppend {
		f.failAfterAppend = false
		return AppendedMessage{}, errors.New("append response lost")
	}
	return message, nil
}

func completionPacketMarkdown(roundID int64) string {
	return `---
schema: den_review_packet
schema_version: 1
packet_kind: completion_evidence
project_id: den-services
task_id: 42
sender: pi-reviewer
review_round_id: ` + itoa(roundID) + `
reviewed_head_commit: head
verdict: looks_good
verify:
  - id: completion_refs_checked
    checked: true
---
# Completion Evidence

Looks good.`
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}
