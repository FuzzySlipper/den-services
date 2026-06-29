package migration

import (
	"strings"
	"testing"
)

func TestTasksMigrationDiscovered(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	var found *Migration
	for i := range migrations {
		if migrations[i].Schema == "den_tasks" && migrations[i].Version == 1 {
			found = &migrations[i]
			break
		}
	}
	if found == nil {
		t.Fatal("den_tasks version 1 migration not discovered")
	}
	for _, want := range []string{
		"create table den_tasks.tasks",
		"create table den_tasks.task_dependencies",
		"create table den_tasks.task_history",
		"grant select, insert, update on den_tasks.tasks to den_tasks_app",
	} {
		if !strings.Contains(found.SQL, want) {
			t.Fatalf("tasks migration missing %q", want)
		}
	}
}
