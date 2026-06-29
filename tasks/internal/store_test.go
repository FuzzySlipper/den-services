package tasks

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

	done := StatusDone
	updated, err := store.UpdateTask(ctx, dependency.ID(), TaskPatch{Status: &done}, "store-test", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.Status() != StatusDone {
		t.Fatalf("updated status = %q", updated.Status())
	}
	history, err := store.History(ctx, dependency.ID())
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	assertHistoryField(t, history, "status", StatusPlanned, StatusDone)
}
