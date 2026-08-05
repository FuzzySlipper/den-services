package migration_test

import (
	"strings"
	"testing"

	migration "den-services/migration"
)

func TestHandoffMigrationOwnsCurrentAndRecoveryRows(t *testing.T) {
	migrations, err := migration.Discover(migration.DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	for _, item := range migrations {
		if item.Path != "postgres/den_handoff/001_initial.sql" {
			continue
		}
		for _, marker := range []string{
			"create table den_handoff.handoffs",
			"label text primary key",
			"create table den_handoff.handoff_revisions",
			"primary key (label, revision)",
			"grant select, insert, update on den_handoff.handoffs to den_handoff_app",
		} {
			if !strings.Contains(item.SQL, marker) {
				t.Fatalf("handoff migration missing %q", marker)
			}
		}
		return
	}
	t.Fatal("handoff migration not discovered")
}
