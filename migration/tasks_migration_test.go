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

func TestHumanAcceptanceMigrationDiscovered(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	for index := range migrations {
		migration := migrations[index]
		if migration.Schema != "den_tasks" || migration.Version != 3 {
			continue
		}
		for _, want := range []string{
			"create table den_tasks.human_acceptance_reviews",
			"unique (task_id, idempotency_key)",
			"revoke update, delete on den_tasks.human_acceptance_reviews from den_tasks_app",
			"grant select, insert on den_tasks.human_acceptance_reviews to den_tasks_app",
		} {
			if !strings.Contains(migration.SQL, want) {
				t.Fatalf("human acceptance migration missing %q", want)
			}
		}
		return
	}
	t.Fatal("den_tasks version 3 migration not discovered")
}
