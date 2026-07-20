package tasks

import (
	"context"
	"errors"
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
