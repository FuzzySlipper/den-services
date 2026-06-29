package review

import (
	"context"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestStorePostgresRepresentativeFlow(t *testing.T) {
	databaseURL := os.Getenv("DEN_REVIEW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_REVIEW_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)
	now := time.Now().UTC()
	round, err := store.CreateRound(ctx, &ReviewRound{
		ProjectID: "review-store-smoke", TaskID: now.UnixNano(), RequestedBy: "pi", Branch: "task/review-smoke",
		BaseBranch: "main", BaseCommit: "base", HeadCommit: "head", PreferredDiffBaseRef: "main",
		PreferredDiffBaseCommit: "base", PreferredDiffHeadRef: "task/review-smoke", PreferredDiffHeadCommit: "head",
		RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateRound() error = %v", err)
	}
	finding, err := store.CreateFinding(ctx, &ReviewFinding{
		ProjectID: round.ProjectID, TaskID: round.TaskID, ReviewRoundID: round.ID, CreatedBy: "pi-reviewer",
		Category: CategoryFollowUpCandidate, Summary: "store smoke finding", Status: StatusOpen, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateFinding() error = %v", err)
	}
	updated, err := store.SetFindingStatus(ctx, finding.ID, FindingStatusUpdate{Status: StatusVerifiedFixed, UpdatedBy: "pi-reviewer", Notes: "verified"}, now)
	if err != nil {
		t.Fatalf("SetFindingStatus() error = %v", err)
	}
	if updated.Status != StatusVerifiedFixed {
		t.Fatalf("updated status = %s", updated.Status)
	}
	summary, err := store.WorkflowSummary(ctx, round.ProjectID, round.TaskID)
	if err != nil {
		t.Fatalf("WorkflowSummary() error = %v", err)
	}
	if summary.ResolvedFindingCount != 1 || summary.ReviewRoundCount != 1 {
		t.Fatalf("summary = %+v", summary)
	}
}
