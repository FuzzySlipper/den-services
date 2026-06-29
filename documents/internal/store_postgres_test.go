package documents

import (
	"context"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestStorePostgresFTSRepresentativeFlow(t *testing.T) {
	databaseURL := os.Getenv("DEN_DOCUMENTS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_DOCUMENTS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)
	now := time.Now().UTC()
	doc, err := NewDocument(NewDocumentParams{
		ProjectID: "store-smoke",
		Slug:      "fts-doc",
		Title:     "FTS Document",
		Content:   "A document about postgres vector search and archive behavior.",
		DocType:   DocTypeSpec,
		Tags:      []string{"fts"},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewDocument() error = %v", err)
	}
	if _, err := store.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	results, err := store.SearchDocuments(ctx, "postgres vector", "store-smoke", VisibilityNormal)
	if err != nil {
		t.Fatalf("SearchDocuments() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("SearchDocuments() returned no results")
	}
}
