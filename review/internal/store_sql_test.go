package review

import (
	"strings"
	"testing"
)

func TestFindingWriteQueriesSelectAliasedProjection(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "create", query: createFindingSQL},
		{name: "respond", query: respondFindingSQL},
		{name: "status", query: setFindingStatusSQL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.query, "select "+findingColumns) {
				t.Fatalf("query does not select finding projection:\n%s", tt.query)
			}
			if !strings.Contains(tt.query, "join den_review.review_rounds r on r.id = f.review_round_id") {
				t.Fatalf("query does not join review rounds for round_number:\n%s", tt.query)
			}
			returningAt := strings.LastIndex(tt.query, "returning")
			selectAt := strings.LastIndex(tt.query, "select")
			if returningAt == -1 || selectAt == -1 || returningAt > selectAt {
				t.Fatalf("query should return raw rows before selecting aliased projection:\n%s", tt.query)
			}
		})
	}
}

func TestTerminalGateWritesAtomicallyInsertIdempotentEvents(t *testing.T) {
	for name, query := range map[string]string{"completion": completeGitHubCheckGateSQL, "supersession": supersedeGitHubCheckGatesSQL} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(query, "insert into den_review.github_check_gate_terminal_events") || !strings.Contains(query, "on conflict(gate_id) do nothing") {
				t.Fatalf("query lacks atomic idempotent terminal event insert:\n%s", query)
			}
			if !strings.Contains(query, "coalesce(check_runs, '[]'::jsonb)") {
				t.Fatalf("terminal event check runs are not normalized to a non-null JSON array:\n%s", query)
			}
		})
	}
}

func TestCampaignRoundQueriesPersistTypedImmutableSnapshot(t *testing.T) {
	for _, want := range []string{
		"target_kind",
		"campaign_children",
		"campaign_repositories",
		"coalesce(branch, '')",
		"coalesce(head_commit, '')",
	} {
		if !strings.Contains(roundColumns, want) {
			t.Fatalf("round projection missing %q:\n%s", want, roundColumns)
		}
	}
	for _, want := range []string{"target_kind", "campaign_children", "campaign_repositories", "$29"} {
		if !strings.Contains(createRoundSQL, want) {
			t.Fatalf("create round query missing %q:\n%s", want, createRoundSQL)
		}
	}
}

func TestFinalizationSerializesRoundCreationAndRejectsStaleRounds(t *testing.T) {
	if !strings.Contains(lockReviewTaskSQL, "pg_advisory_xact_lock") {
		t.Fatalf("review task lock must serialize round creation and finalization: %s", lockReviewTaskSQL)
	}
	if !strings.Contains(currentRoundForUpdateSQL, "order by round_number desc") ||
		!strings.Contains(currentRoundForUpdateSQL, "for update") {
		t.Fatalf("current-round query must lock the latest round: %s", currentRoundForUpdateSQL)
	}
}

func TestFinalizationProjectionCarriesMaterialDigest(t *testing.T) {
	if !strings.Contains(finalizationColumns, "material_digest") || !strings.Contains(insertFinalizationSQL, "material_digest") {
		t.Fatalf("finalization persistence omits material digest: columns=%s insert=%s", finalizationColumns, insertFinalizationSQL)
	}
}
