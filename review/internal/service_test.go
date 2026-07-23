package review

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
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

	verdict, err := service.SetVerdict(ctx, round.ID, SetReviewVerdictRequest{Verdict: VerdictChangesRequested, DecidedBy: "pi-reviewer", Notes: "One issue"})
	if err != nil {
		t.Fatalf("SetVerdict() error = %v", err)
	}
	if verdict.Verdict != VerdictChangesRequested {
		t.Fatalf("verdict not stored: %+v", verdict)
	}
	if tasks.tasks[42].Status != TaskStatusInProgress || len(tasks.statusUpdates) != 1 || tasks.statusUpdates[0] != (fakeTaskStatusUpdate{Agent: "pi-reviewer", Status: TaskStatusInProgress}) {
		t.Fatalf("changes-requested verdict did not return task to implementation: %+v updates=%+v", tasks.tasks[42], tasks.statusUpdates)
	}
	if len(messages.appended) != 1 || messages.appended[0].Intent != "review_feedback" {
		t.Fatalf("verdict message not appended as review feedback: %+v", messages.appended)
	}
	metadata := messages.appended[0].Metadata
	if metadata["type"] != "review_feedback" || metadata["packet_kind"] != PacketKindReviewFindings {
		t.Fatalf("verdict metadata did not separate type/packet_kind: %#v", metadata)
	}
}

func TestServiceSetVerdictTransitionsTaskStatus(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		verdict    string
		wantStatus string
	}{
		{name: "looks good completes task", verdict: VerdictLooksGood, wantStatus: TaskStatusDone},
		{name: "changes requested resumes task", verdict: VerdictChangesRequested, wantStatus: TaskStatusInProgress},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			tasks := &fakeTasks{tasks: map[int64]TaskContext{42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview}}}
			service := newTestService(newMemoryStore(), &fakeMessages{}, tasks)
			round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "pi", Branch: "task/verdict", BaseBranch: "main", BaseCommit: "base", HeadCommit: testCase.verdict})
			if err != nil {
				t.Fatalf("CreateRound() error = %v", err)
			}
			if _, err := service.SetVerdict(ctx, round.ID, SetReviewVerdictRequest{Verdict: testCase.verdict, DecidedBy: "pi-reviewer"}); err != nil {
				t.Fatalf("SetVerdict() error = %v", err)
			}
			if task := tasks.tasks[42]; task.Status != testCase.wantStatus {
				t.Fatalf("task status = %q, want %q", task.Status, testCase.wantStatus)
			}
			if updates := tasks.statusUpdates; len(updates) != 1 || updates[0] != (fakeTaskStatusUpdate{Agent: "pi-reviewer", Status: testCase.wantStatus}) {
				t.Fatalf("task status updates = %+v", updates)
			}
		})
	}
}

func TestServiceSetVerdictReturnsTaskStatusUpdateFailure(t *testing.T) {
	ctx := context.Background()
	tasks := &fakeTasks{tasks: map[int64]TaskContext{42: {ID: 42, ProjectID: "den-services", Status: TaskStatusReview}}, failStatusUpdate: errors.New("tasks unavailable")}
	messages := &fakeMessages{}
	service := newTestService(newMemoryStore(), messages, tasks)
	round, err := service.CreateRound(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "pi", Branch: "task/verdict", BaseBranch: "main", BaseCommit: "base", HeadCommit: "head"})
	if err != nil {
		t.Fatalf("CreateRound() error = %v", err)
	}
	if _, err := service.SetVerdict(ctx, round.ID, SetReviewVerdictRequest{Verdict: VerdictLooksGood, DecidedBy: "pi-reviewer"}); err == nil || !strings.Contains(err.Error(), "tasks unavailable") {
		t.Fatalf("SetVerdict() error = %v, want task status update failure", err)
	}
	if tasks.tasks[42].Status != TaskStatusReview || len(messages.appended) != 0 {
		t.Fatalf("failed task transition must not claim completion: task=%+v messages=%+v", tasks.tasks[42], messages.appended)
	}
}

func TestRequestReviewMetadataUsesCanonicalPacketKind(t *testing.T) {
	ctx := context.Background()
	messages := &fakeMessages{}
	service := newTestService(newMemoryStore(), messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}})
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

func TestPostPacketMarkdownMessageFailureLeavesPendingWithoutRetryDuplicate(t *testing.T) {
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
		t.Fatalf("retry should return pending packet without appending: %v", err)
	}
	if retry.ID != pending.ID || retry.ValidationStatus != PacketStatusPendingMessageAppend {
		t.Fatalf("retry did not return existing pending packet: %+v", retry)
	}
	if len(messages.appended) != 0 {
		t.Fatalf("retry appended duplicate/ambiguous message attempts: %d", len(messages.appended))
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
	tasks            map[int64]TaskContext
	created          []CreateFollowUpTaskRequest
	statusUpdates    []fakeTaskStatusUpdate
	failStatusUpdate error
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
	if f.failStatusUpdate != nil {
		return TaskContext{}, f.failStatusUpdate
	}
	task, ok := f.tasks[taskID]
	if !ok || task.ProjectID != projectID {
		return TaskContext{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
	}
	task.Status = status
	f.tasks[taskID] = task
	f.statusUpdates = append(f.statusUpdates, fakeTaskStatusUpdate{Agent: agent, Status: status})
	return task, nil
}

func (f *fakeTasks) CreateFollowUpTask(_ context.Context, projectID string, req CreateFollowUpTaskRequest) (CreatedTask, error) {
	f.created = append(f.created, req)
	return CreatedTask{ID: int64(9000 + len(f.created)), ProjectID: projectID, Title: req.Title, Status: "planned"}, nil
}

type fakeMessages struct {
	appended   []AppendMessageRequest
	failAppend bool
}

type fakeGitHubChecks struct {
	result       GitHubCheckResult
	err          error
	resultsBySHA map[string]GitHubCheckResult
	errorsBySHA  map[string]error
	calls        int
}

func (f *fakeGitHubChecks) CheckCommit(_ context.Context, _ string, commitSHA string, _ []string) (GitHubCheckResult, error) {
	f.calls++
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
	f.appended = append(f.appended, req)
	return AppendedMessage{ID: int64(len(f.appended)), ProjectID: projectID, TaskID: &req.TaskID, Intent: req.Intent}, nil
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
