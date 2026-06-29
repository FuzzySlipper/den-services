package review

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestServiceReviewRoundFindingVerdictAndResponse(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	service := newTestService(store, messages, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review service", Status: TaskStatusReview, Priority: 1},
	}})

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
	if len(messages.appended) != 1 || messages.appended[0].Intent != "review_feedback" {
		t.Fatalf("verdict message not appended as review feedback: %+v", messages.appended)
	}
	metadata := messages.appended[0].Metadata
	if metadata["type"] != "review_feedback" || metadata["packet_kind"] != PacketKindReviewFindings {
		t.Fatalf("verdict metadata did not separate type/packet_kind: %#v", metadata)
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

func newTestService(store ReviewStore, messages MessageClient, tasks TaskClient) *Service {
	return NewService(store, NoopProjectValidator{}, tasks, messages, func() time.Time {
		return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	})
}

type fakeTasks struct {
	tasks   map[int64]TaskContext
	created []CreateFollowUpTaskRequest
}

func (f fakeTasks) GetTaskContext(_ context.Context, projectID string, taskID int64) (TaskContext, error) {
	if task, ok := f.tasks[taskID]; ok {
		return task, nil
	}
	return TaskContext{}, validationError(errors.New("task not found"), "task_not_found", "task_id", "common.task_id")
}

func (f *fakeTasks) CreateFollowUpTask(_ context.Context, projectID string, req CreateFollowUpTaskRequest) (CreatedTask, error) {
	f.created = append(f.created, req)
	return CreatedTask{ID: int64(9000 + len(f.created)), ProjectID: projectID, Title: req.Title, Status: "planned"}, nil
}

type fakeMessages struct {
	appended   []AppendMessageRequest
	failAppend bool
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
