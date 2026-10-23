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
		BaseBranch:  "main",
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

func TestStorePostgresAtomicSupersession(t *testing.T) {
	databaseURL := os.Getenv("DEN_REVIEW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_REVIEW_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewStore(pool)
	now := time.Now().UTC()
	old, err := store.CreateRound(ctx, &ReviewRound{ProjectID: "supersession-store-test", TaskID: now.UnixNano(), RequestedBy: "coder", RequestedAt: now, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	replacement := ReviewRound{ProjectID: old.ProjectID, TaskID: old.TaskID, RequestedBy: "coder", TargetKind: ReviewTargetCodeDiff, Notes: "Corrected range", RequestedAt: now, CreatedAt: now, UpdatedAt: now}
	type result struct {
		round *ReviewRound
		err   error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			request := replacement
			round, err := store.CreateRoundSuperseding(ctx, &request, old.ID, "Corrected the range")
			results <- result{round, err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("first=%v second=%v", first.err, second.err)
	}
	if first.round.ID != second.round.ID {
		t.Fatalf("duplicate replacement rounds: %d and %d", first.round.ID, second.round.ID)
	}
	closed, err := store.GetRound(ctx, old.ID)
	if err != nil || closed.Verdict != VerdictSuperseded || closed.VerdictNotes != supersessionNotes(first.round.ID, "Corrected the range") {
		t.Fatalf("old=%+v err=%v", closed, err)
	}
	rounds, err := store.ListRounds(ctx, old.ProjectID, old.TaskID)
	if err != nil || len(rounds) != 2 {
		t.Fatalf("rounds=%d err=%v", len(rounds), err)
	}
	finding := &ReviewFinding{ProjectID: old.ProjectID, TaskID: old.TaskID, ReviewRoundID: first.round.ID, CreatedBy: "reviewer", Category: CategoryFollowUpCandidate, Summary: "Review started", Status: StatusOpen, CreatedAt: now, UpdatedAt: now}
	if _, err := store.CreateFinding(ctx, finding); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRoundSuperseding(ctx, &replacement, first.round.ID, "Too late"); err == nil {
		t.Fatal("superseded a round with findings")
	}
	finding.ReviewRoundID = old.ID
	if _, err := store.CreateFinding(ctx, finding); err == nil {
		t.Fatal("added finding to closed round")
	}
}
