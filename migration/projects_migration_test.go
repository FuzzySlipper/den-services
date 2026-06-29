package migration

import (
	"strings"
	"testing"
)

func TestProjectsMigrationDiscovered(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	var found *Migration
	for i := range migrations {
		if migrations[i].Schema == "den_projects" && migrations[i].Version == 1 {
			found = &migrations[i]
			break
		}
	}
	if found == nil {
		t.Fatal("den_projects version 1 migration not discovered")
	}
	for _, want := range []string{
		"create table den_projects.projects",
		"create view den_projects.project_refs",
		"grant select, insert, update on den_projects.projects to den_projects_app",
	} {
		if !strings.Contains(found.SQL, want) {
			t.Fatalf("projects migration missing %q", want)
		}
	}
}
