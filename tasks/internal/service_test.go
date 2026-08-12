package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServiceCreateListAndSubtasks(t *testing.T) {
	service := newTestService()
	ctx := context.Background()

	parent, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{
		Title:    "Parent",
		Priority: 2,
		Tags:     []string{"infra", "tasks", "infra"},
	})
	if err != nil {
		t.Fatalf("CreateTask(parent) error = %v", err)
	}
	if parent.Priority() != 2 || parent.Status() != StatusPlanned {
		t.Fatalf("parent = %+v", parent)
	}

	child, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{
		Title:    "Child",
		ParentID: int64Ptr(parent.ID()),
		Priority: 1,
	})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}

	topLevel, err := service.ListTasks(ctx, "den-services", ListTasksQuery{})
	if err != nil {
		t.Fatalf("ListTasks(top-level) error = %v", err)
	}
	if len(topLevel) != 1 || topLevel[0].Task.ID() != parent.ID() || topLevel[0].SubtaskCount != 1 {
		t.Fatalf("top-level summaries = %+v", topLevel)
	}

	children, err := service.ListTasks(ctx, "den-services", ListTasksQuery{ParentID: int64Ptr(parent.ID())})
	if err != nil {
		t.Fatalf("ListTasks(parent_id) error = %v", err)
	}
	if len(children) != 1 || children[0].Task.ID() != child.ID() {
		t.Fatalf("children summaries = %+v", children)
	}

	tagged, err := service.ListTasks(ctx, "den-services", ListTasksQuery{Tags: []string{"infra", "tasks"}})
	if err != nil {
		t.Fatalf("ListTasks(tags) error = %v", err)
	}
	if len(tagged) != 1 || tagged[0].Task.ID() != parent.ID() {
		t.Fatalf("tagged summaries = %+v", tagged)
	}
	if parent.Tags()[0] != "infra" || len(parent.Tags()) != 2 {
		t.Fatalf("tags were not normalized: %+v", parent.Tags())
	}
}

func TestServiceListTasksUsesBoundedOffsetPagination(t *testing.T) {
	service := newTestService()
	for _, title := range []string{"First", "Second", "Third"} {
		if _, err := service.CreateTask(t.Context(), "den-services", CreateTaskRequest{Title: title, Priority: 2}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := service.ListTasks(t.Context(), "den-services", ListTasksQuery{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].Task.Title() != "Second" {
		t.Fatalf("page = %+v", page)
	}
	if _, err := service.ListTasks(t.Context(), "den-services", ListTasksQuery{Limit: 201}); err == nil {
		t.Fatal("expected limit validation error")
	}
}

func TestServiceTransitionTaskToReviewIsConditionalAndIdempotent(t *testing.T) {
	service := newTestService()
	ctx := context.Background()
	task, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Review transition"})
	if err != nil {
		t.Fatal(err)
	}
	inProgress := StatusInProgress
	if _, err := service.UpdateTask(ctx, task.ID(), UpdateTaskRequest{Agent: "runner", Status: &inProgress}); err != nil {
		t.Fatal(err)
	}
	transitioned, transition, err := service.TransitionTaskToReview(
		ctx,
		"den-services",
		task.ID(),
		ReviewTransitionRequest{Agent: "review"},
	)
	if err != nil {
		t.Fatalf("TransitionTaskToReview() error = %v", err)
	}
	if transitioned.Status() != StatusReview || transition != TaskTransitionApplied {
		t.Fatalf("transitioned task/result = %s/%s", transitioned.Status(), transition)
	}
	_, transition, err = service.TransitionTaskToReview(
		ctx,
		"den-services",
		task.ID(),
		ReviewTransitionRequest{Agent: "review"},
	)
	if err != nil || transition != TaskTransitionAlreadySatisfied {
		t.Fatalf("idempotent transition = %q, error = %v", transition, err)
	}
	history, err := service.History(ctx, task.ID())
	if err != nil {
		t.Fatal(err)
	}
	var reviewTransitions int
	for _, entry := range history {
		if entry.Field == "status" && entry.OldValue == StatusInProgress && entry.NewValue == StatusReview {
			reviewTransitions++
		}
	}
	if reviewTransitions != 1 {
		t.Fatalf("in_progress -> review history transitions = %d, want 1", reviewTransitions)
	}

	doneTask, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Already done"})
	if err != nil {
		t.Fatal(err)
	}
	done := StatusDone
	if _, err := service.UpdateTask(ctx, doneTask.ID(), UpdateTaskRequest{Agent: "runner", Status: &done}); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.TransitionTaskToReview(
		ctx,
		"den-services",
		doneTask.ID(),
		ReviewTransitionRequest{Agent: "review"},
	)
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Code() != "review_transition_ineligible" {
		t.Fatalf("done transition error = %#v, want review_transition_ineligible", err)
	}
	detail, err := service.GetTask(ctx, doneTask.ID())
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.Status() != StatusDone {
		t.Fatalf("rejected transition changed status to %q", detail.Task.Status())
	}
}

func TestServiceRecordsHumanAcceptanceAndExplicitlyCompletesEligibleParent(t *testing.T) {
	service := newTestService()
	ctx := context.Background()
	parent, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Playtest campaign"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Hands-on playtest", ParentID: int64Ptr(parent.ID())})
	if err != nil {
		t.Fatal(err)
	}
	readAt := child.UpdatedAt()
	result, err := service.RecordHumanAcceptance(ctx, child.ID(), RecordHumanAcceptanceRequest{
		ReviewerIdentity: "user", Rationale: "Used the pointer-lock flow and it feels correct.",
		ReviewedRevision: "786010d68f434487ed01b8d0acba0db8a05dce8c",
		EvidenceLinks:    []string{"run://playtest/one"}, LifecycleEffect: HumanAcceptanceCompleteTaskAndParent,
		IdempotencyKey: "human-acceptance-6783", ExpectedTaskUpdatedAt: &readAt,
	})
	if err != nil {
		t.Fatalf("RecordHumanAcceptance() error = %v", err)
	}
	if result.Task.Status() != StatusDone || result.Parent == nil || result.Parent.Status() != StatusDone {
		t.Fatalf("result task/parent = %+v/%+v", result.Task, result.Parent)
	}
	if len(result.ChangedTaskIDs) != 2 || result.Review.Verdict != HumanAcceptanceVerdictLooksGood {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Review.NoteMarkdown, "Used the pointer-lock flow") ||
		strings.Contains(result.Review.NoteMarkdown, "tests passed") {
		t.Fatalf("generated note invented or lost facts: %s", result.Review.NoteMarkdown)
	}
	detail, err := service.GetTask(ctx, child.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.HumanAcceptanceReviews) != 1 || detail.HumanAcceptanceReviews[0].ReviewerIdentity != "user" {
		t.Fatalf("human acceptance projection = %+v", detail.HumanAcceptanceReviews)
	}
	retry, err := service.RecordHumanAcceptance(ctx, child.ID(), RecordHumanAcceptanceRequest{
		ReviewerIdentity: "user", Rationale: "Used the pointer-lock flow and it feels correct.",
		ReviewedRevision: "786010d68f434487ed01b8d0acba0db8a05dce8c",
		EvidenceLinks:    []string{"run://playtest/one"}, LifecycleEffect: HumanAcceptanceCompleteTaskAndParent,
		IdempotencyKey: "human-acceptance-6783", ExpectedTaskUpdatedAt: &readAt,
	})
	if err != nil || retry.Review.ID != result.Review.ID {
		t.Fatalf("idempotent retry = %+v, error = %v", retry, err)
	}
	detail, _ = service.GetTask(ctx, child.ID())
	if len(detail.HumanAcceptanceReviews) != 1 {
		t.Fatalf("retry duplicated acceptance: %+v", detail.HumanAcceptanceReviews)
	}
}

func TestServiceHumanAcceptanceReconciliationAndParentEligibility(t *testing.T) {
	service := newTestService()
	ctx := context.Background()
	parent, _ := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Parent"})
	child, _ := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Child", ParentID: int64Ptr(parent.ID())})
	_, _ = service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Still open", ParentID: int64Ptr(parent.ID())})
	stale := child.UpdatedAt().Add(-time.Minute)
	_, err := service.RecordHumanAcceptance(ctx, child.ID(), RecordHumanAcceptanceRequest{
		ReviewerIdentity: "user", IdempotencyKey: "stale", ExpectedTaskUpdatedAt: &stale,
	})
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Code() != "human_acceptance_task_changed" {
		t.Fatalf("stale readback error = %#v", err)
	}
	_, err = service.RecordHumanAcceptance(ctx, child.ID(), RecordHumanAcceptanceRequest{
		ReviewerIdentity: "user", IdempotencyKey: "parent", LifecycleEffect: HumanAcceptanceCompleteTaskAndParent,
	})
	if !errors.As(err, &serviceErr) || serviceErr.Code() != "human_acceptance_parent_ineligible" {
		t.Fatalf("ineligible parent error = %#v", err)
	}
	detail, _ := service.GetTask(ctx, child.ID())
	if detail.Task.Status() == StatusDone || len(detail.HumanAcceptanceReviews) != 0 {
		t.Fatalf("failed parent mutation was not atomic: %+v", detail)
	}
	_, err = service.RecordHumanAcceptance(ctx, child.ID(), RecordHumanAcceptanceRequest{
		ReviewerIdentity: "user", Rationale: "First", IdempotencyKey: "same-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RecordHumanAcceptance(ctx, child.ID(), RecordHumanAcceptanceRequest{
		ReviewerIdentity: "user", Rationale: "Different", IdempotencyKey: "same-key",
	})
	if !errors.As(err, &serviceErr) || serviceErr.Code() != "human_acceptance_idempotency_conflict" {
		t.Fatalf("idempotency conflict = %#v", err)
	}
}

func TestServiceNextTaskDependenciesAndCycles(t *testing.T) {
	service := newTestService()
	ctx := context.Background()

	dependency, err := service.CreateTask(ctx, "upstream-services", CreateTaskRequest{Title: "Upstream dependency", Priority: 2})
	if err != nil {
		t.Fatalf("CreateTask(dependency) error = %v", err)
	}
	waiting, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Waiting", Priority: 1, DependsOn: []int64{dependency.ID()}})
	if err != nil {
		t.Fatalf("CreateTask(waiting) error = %v", err)
	}

	next, err := service.NextTask(ctx, "den-services", "")
	if err != nil {
		t.Fatalf("NextTask() error = %v", err)
	}
	if next != nil {
		t.Fatalf("next before cross-project dependency completion = %+v", next)
	}
	detail, err := service.GetTask(ctx, waiting.ID())
	if err != nil {
		t.Fatalf("GetTask(waiting) error = %v", err)
	}
	if len(detail.Dependencies) != 1 || detail.Dependencies[0].ProjectID != "upstream-services" {
		t.Fatalf("cross-project dependencies = %+v", detail.Dependencies)
	}

	review := StatusReview
	if _, err := service.UpdateTask(ctx, dependency.ID(), UpdateTaskRequest{Agent: "tester", Status: &review}); err != nil {
		t.Fatalf("UpdateTask(review) error = %v", err)
	}
	next, err = service.NextTask(ctx, "den-services", "")
	if err != nil {
		t.Fatalf("NextTask(after review) error = %v", err)
	}
	if next == nil || next.ID() != waiting.ID() {
		t.Fatalf("next after dependency enters review = %+v", next)
	}

	if err := service.AddDependency(ctx, dependency.ID(), waiting.ID()); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("AddDependency(cycle) error = %v", err)
	}
	if err := service.AddDependency(ctx, waiting.ID(), waiting.ID()); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("AddDependency(self) error = %v", err)
	}
}

func TestServiceBlockedInvariantAndHistory(t *testing.T) {
	service := newTestService()
	ctx := context.Background()
	task, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Blocked task"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	blocked := StatusBlocked
	if _, err := service.UpdateTask(ctx, task.ID(), UpdateTaskRequest{Agent: "tester", Status: &blocked}); !errors.Is(err, ErrBlockedContextMissing) {
		t.Fatalf("UpdateTask(blocked missing fields) error = %v", err)
	}

	summary := "Waiting for deploy window"
	reason := "Production services are active"
	requiresHuman := true
	updated, err := service.UpdateTask(ctx, task.ID(), UpdateTaskRequest{
		Agent:                     "tester",
		Status:                    &blocked,
		BlockerSummary:            &summary,
		BlockerReason:             &reason,
		BlockerRequiresHumanInput: &requiresHuman,
	})
	if err != nil {
		t.Fatalf("UpdateTask(blocked) error = %v", err)
	}
	if updated.Status() != StatusBlocked || updated.BlockerSummary() != summary || !updated.BlockerRequiresHumanInput() {
		t.Fatalf("updated blocked task = %+v", updated)
	}

	history, err := service.History(ctx, task.ID())
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	assertHistoryField(t, history, "status", StatusPlanned, StatusBlocked)
	assertHistoryField(t, history, "blocker_summary", "", summary)
	assertHistoryField(t, history, "blocker_reason", "", reason)
	assertHistoryField(t, history, "blocker_requires_human_input", "false", "true")

	review := StatusReview
	updated, err = service.UpdateTask(ctx, task.ID(), UpdateTaskRequest{Agent: "tester", Status: &review})
	if err != nil {
		t.Fatalf("UpdateTask(review) error = %v", err)
	}
	if updated.Status() != StatusReview || updated.BlockerSummary() != "" || updated.BlockerReason() != "" || updated.BlockerRequiresHumanInput() {
		t.Fatalf("updated review task retained blocker context: %+v", updated)
	}
}

func TestServiceTaskChangesIncludeSummaryForDependentAvailability(t *testing.T) {
	service := newTestService()
	ctx := context.Background()
	dependency, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Dependency"})
	if err != nil {
		t.Fatalf("CreateTask(dependency) error = %v", err)
	}
	waiting, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Waiting", DependsOn: []int64{dependency.ID()}})
	if err != nil {
		t.Fatalf("CreateTask(waiting) error = %v", err)
	}
	initial, err := service.ListTaskChanges(ctx, "den-services", 0, 10)
	if err != nil {
		t.Fatalf("ListTaskChanges(initial) error = %v", err)
	}
	if len(initial) != 2 || initial[1].Summary.Task.ID() != waiting.ID() || initial[1].Summary.Availability() != AvailabilityWaitingOnDependencies {
		t.Fatalf("initial changes = %+v", initial)
	}
	done := StatusDone
	if _, err := service.UpdateTask(ctx, dependency.ID(), UpdateTaskRequest{Agent: "tester", Status: &done}); err != nil {
		t.Fatalf("UpdateTask(done) error = %v", err)
	}
	changes, err := service.ListTaskChanges(ctx, "den-services", initial[len(initial)-1].ID, 10)
	if err != nil {
		t.Fatalf("ListTaskChanges(after done) error = %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes len = %d, want dependency and dependent", len(changes))
	}
	foundWaiting := false
	for _, event := range changes {
		if event.Summary.Task.ID() == waiting.ID() {
			foundWaiting = true
			if event.Summary.Availability() != AvailabilityAvailable {
				t.Fatalf("waiting availability = %q, want available", event.Summary.Availability())
			}
		}
	}
	if !foundWaiting {
		t.Fatalf("dependent waiting task missing from changes: %+v", changes)
	}
}

func TestServiceSubtaskTierPrecedesTopLevelPlanned(t *testing.T) {
	service := newTestService()
	ctx := context.Background()

	parent, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Parent", Priority: 5})
	if err != nil {
		t.Fatalf("CreateTask(parent) error = %v", err)
	}
	inProgress := StatusInProgress
	if _, err := service.UpdateTask(ctx, parent.ID(), UpdateTaskRequest{Agent: "tester", Status: &inProgress}); err != nil {
		t.Fatalf("UpdateTask(parent) error = %v", err)
	}
	child, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Subtask", ParentID: int64Ptr(parent.ID()), Priority: 5})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	topLevel, err := service.CreateTask(ctx, "den-services", CreateTaskRequest{Title: "Top level", Priority: 1})
	if err != nil {
		t.Fatalf("CreateTask(top-level) error = %v", err)
	}
	if topLevel.ID() == 0 {
		t.Fatal("top-level task was not created")
	}

	next, err := service.NextTask(ctx, "den-services", "")
	if err != nil {
		t.Fatalf("NextTask() error = %v", err)
	}
	if next == nil || next.ID() != child.ID() {
		t.Fatalf("next = %+v, want child %d", next, child.ID())
	}
}

func newTestService() *Service {
	return NewService(newMemoryStore(), NoopScopeValidator{}, fixedClock)
}

func fixedClock() time.Time {
	return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
}

func int64Ptr(value int64) *int64 {
	return &value
}

func assertHistoryField(t *testing.T, entries []TaskHistoryEntry, field string, oldValue string, newValue string) {
	t.Helper()
	for _, entry := range entries {
		if entry.Field == field && entry.OldValue == oldValue && entry.NewValue == newValue {
			return
		}
	}
	t.Fatalf("history missing %s %q -> %q in %+v", field, oldValue, newValue, entries)
}
