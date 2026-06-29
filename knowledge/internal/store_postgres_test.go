package knowledge

import (
	"context"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestStorePostgresKnowledgeFTSRepresentativeFlow(t *testing.T) {
	databaseURL := os.Getenv("DEN_KNOWLEDGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_KNOWLEDGE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)
	now := time.Now().UTC()
	entry, err := NewEntry(NewEntryParams{
		Slug:          "fts-knowledge",
		Title:         "FTS Knowledge",
		BodyMarkdown:  "A reviewed entry about postgres vector search and knowledge retrieval.",
		Summary:       "Postgres vector knowledge",
		Kind:          KindReference,
		Status:        StatusReviewed,
		CurationState: CurationAgentCurated,
		Tags:          []string{"fts", "knowledge"},
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("NewEntry() error = %v", err)
	}
	if _, err := store.UpsertEntry(ctx, entry, "postgres smoke"); err != nil {
		t.Fatalf("UpsertEntry() error = %v", err)
	}
	results, err := store.SearchEntries(ctx, SearchQuery{Query: "postgres vector", RequiredTags: []string{"knowledge"}, Limit: 10})
	if err != nil {
		t.Fatalf("SearchEntries() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("SearchEntries() returned no results")
	}
}
