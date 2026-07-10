package migration

import (
	"strings"
	"testing"
)

func TestReviewMigrationDiscovered(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	var found *Migration
	for i := range migrations {
		if migrations[i].Schema == "den_review" && migrations[i].Version == 1 {
			found = &migrations[i]
			break
		}
	}
	if found == nil {
		t.Fatal("den_review version 1 migration not discovered")
	}
	for _, want := range []string{
		"create table den_review.review_rounds",
		"create table den_review.review_findings",
		"create table den_review.review_finding_events",
		"create table den_review.review_packets",
		"grant select, insert, update on den_review.review_rounds to den_review_app",
		"review_packets_round_kind_created_idx",
	} {
		if !strings.Contains(found.SQL, want) {
			t.Fatalf("review migration missing %q", want)
		}
	}
}

func TestReviewGitHubDiagnosticsMigrationDiscovered(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	for i := range migrations {
		if migrations[i].Schema == "den_review" && migrations[i].Version == 3 {
			if !strings.Contains(migrations[i].SQL, "missing_required_checks") || !strings.Contains(migrations[i].SQL, "observed_check_runs") {
				t.Fatalf("unexpected migration SQL: %s", migrations[i].SQL)
			}
			return
		}
	}
	t.Fatal("den_review version 3 migration not discovered")
}
