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
		})
	}
}
