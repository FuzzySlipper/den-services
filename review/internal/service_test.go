package review

import (
	"context"
	"errors"
	"strconv"
	"strings"
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
	if _, err := service.SetVerdict(ctx, round.ID, SetReviewVerdictRequest{Verdict: VerdictChangesRequested, DecidedBy: "pi-reviewer"}); err != nil {
		t.Fatal(err)
	}
	messages.appended = nil

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
	if !strings.Contains(messages.appended[0].Content, "https://github.test/run/1") {
		t.Fatalf("message missing check run URL: %s", messages.appended[0].Content)
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
	tasks   map[int64]TaskContext
	created []CreateFollowUpTaskRequest
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

func (f *fakeTasks) CreateFollowUpTask(_ context.Context, projectID string, req CreateFollowUpTaskRequest) (CreatedTask, error) {
	f.created = append(f.created, req)
	return CreatedTask{ID: int64(9000 + len(f.created)), ProjectID: projectID, Title: req.Title, Status: "planned"}, nil
}

type fakeMessages struct {
	appended   []AppendMessageRequest
	failAppend bool
}

type fakeGitHubChecks struct {
	result GitHubCheckResult
	err    error
}

func (f fakeGitHubChecks) CheckCommit(context.Context, string, string, []string) (GitHubCheckResult, error) {
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
