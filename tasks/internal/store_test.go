package tasks

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTaskUpdatesLockRowsBeforeComparingPatches(t *testing.T) {
	if !strings.Contains(strings.ToLower(getTaskForUpdateSQL), "for update") {
		t.Fatalf("task update read is not row-locking: %s", getTaskForUpdateSQL)
	}
}

func TestStoreLifecycleSmoke(t *testing.T) {
	databaseURL := os.Getenv("DEN_TASKS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_TASKS_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	projectID := "tasks-store-smoke-" + time.Now().UTC().Format("20060102150405.000000000")
	now := fixedClock()
	dependency, err := NewTask(NewTaskParams{
		ProjectID: projectID,
		Title:     "Dependency",
		Priority:  2,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewTask(dependency) error = %v", err)
	}
	dependency, err = store.CreateTask(ctx, dependency, nil)
	if err != nil {
		t.Fatalf("CreateTask(dependency) error = %v", err)
	}
	task, err := NewTask(NewTaskParams{
		ProjectID: projectID,
		Title:     "Waiting",
		Priority:  1,
		Tags:      []string{"infra", "tasks"},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewTask(task) error = %v", err)
	}
	task, err = store.CreateTask(ctx, task, []int64{dependency.ID()})
	if err != nil {
		t.Fatalf("CreateTask(task) error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "delete from den_tasks.tasks where project_id = $1", projectID)
	})

	summaries, err := store.ListTasks(ctx, ListTasksQuery{ProjectID: projectID, Tags: []string{"infra", "tasks"}})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].Task.ID() != task.ID() || summaries[0].Availability() != AvailabilityWaitingOnDependencies {
		t.Fatalf("summaries = %+v", summaries)
	}

	next, err := store.NextTask(ctx, projectID, "")
	if err != nil {
		t.Fatalf("NextTask() error = %v", err)
	}
	if next == nil || next.ID() != dependency.ID() {
		t.Fatalf("next = %+v, want dependency %d", next, dependency.ID())
	}
	next, err = store.NextTask(ctx, projectID, "codex")
	if err != nil {
		t.Fatalf("NextTask(assigned) error = %v", err)
	}
	if next == nil || next.ID() != dependency.ID() {
		t.Fatalf("next assigned should include unassigned task = %+v, want dependency %d", next, dependency.ID())
	}

	review := StatusReview
	updated, err := store.UpdateTask(ctx, dependency.ID(), TaskPatch{Status: &review}, "store-test", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.Status() != StatusReview {
		t.Fatalf("updated status = %q", updated.Status())
	}
	next, err = store.NextTask(ctx, projectID, "")
	if err != nil {
		t.Fatalf("NextTask(after review) error = %v", err)
	}
	if next == nil || next.ID() != task.ID() {
		t.Fatalf("next after review = %+v, want waiting task %d", next, task.ID())
	}
	inProgress := StatusInProgress
	if _, err := store.UpdateTask(ctx, task.ID(), TaskPatch{Status: &inProgress}, "store-test", now.Add(time.Minute)); err != nil {
		t.Fatalf("UpdateTask(in_progress) error = %v", err)
	}
	reviewed, transition, err := store.TransitionTaskToReview(
		ctx,
		projectID,
		task.ID(),
		"store-test",
		now.Add(2*time.Minute),
	)
	if err != nil || reviewed.Status() != StatusReview || transition != TaskTransitionApplied {
		t.Fatalf("TransitionTaskToReview() task=%+v transition=%q error=%v", reviewed, transition, err)
	}
	if _, transition, err := store.TransitionTaskToReview(
		ctx,
		projectID,
		task.ID(),
		"store-test",
		now.Add(3*time.Minute),
	); err != nil || transition != TaskTransitionAlreadySatisfied {
		t.Fatalf("idempotent TransitionTaskToReview() transition=%q error=%v", transition, err)
	}
	taskHistory, err := store.History(ctx, task.ID())
	if err != nil {
		t.Fatal(err)
	}
	var inProgressToReview int
	for _, entry := range taskHistory {
		if entry.Field == "status" && entry.OldValue == StatusInProgress && entry.NewValue == StatusReview {
			inProgressToReview++
		}
	}
	if inProgressToReview != 1 {
		t.Fatalf("in_progress->review history entries = %d, want 1: %+v", inProgressToReview, taskHistory)
	}
	history, err := store.History(ctx, dependency.ID())
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	assertHistoryField(t, history, "status", StatusPlanned, StatusReview)

	start := make(chan struct{})
	results := make(chan error, 2)
	done := StatusDone
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			_, updateErr := store.UpdateTask(ctx, dependency.ID(), TaskPatch{Status: &done}, "concurrent-store-test", now.Add(2*time.Minute))
			results <- updateErr
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if updateErr := <-results; updateErr != nil {
			t.Fatalf("concurrent UpdateTask() error = %v", updateErr)
		}
	}
	history, err = store.History(ctx, dependency.ID())
	if err != nil {
		t.Fatalf("History(after concurrent update) error = %v", err)
	}
	var reviewToDone int
	for _, entry := range history {
		if entry.Field == "status" && entry.OldValue == StatusReview && entry.NewValue == StatusDone {
			reviewToDone++
		}
	}
	if reviewToDone != 1 {
		t.Fatalf("review->done history entries = %d, want 1: %+v", reviewToDone, history)
	}
}
