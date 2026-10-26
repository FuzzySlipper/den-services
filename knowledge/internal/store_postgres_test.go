package knowledge

import (
	"context"
	"errors"
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
	_ = store.DeleteEntry(ctx, "fts-knowledge")
	_ = store.DeleteEntry(ctx, "fts-linked-target")
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
	if _, err := store.UpsertEntry(ctx, entry, PreservedFields{}, "postgres smoke"); err != nil {
		t.Fatalf("UpsertEntry() error = %v", err)
	}
	results, err := store.SearchEntries(ctx, SearchQuery{Query: "postgres vector", RequiredTags: []string{"knowledge"}, Limit: 10})
	if err != nil {
		t.Fatalf("SearchEntries() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("SearchEntries() returned no results")
	}
	target, err := NewEntry(NewEntryParams{
		Slug:          "fts-linked-target",
		Title:         "FTS Linked Target",
		BodyMarkdown:  "A target used to prove retained missing links.",
		Kind:          KindReference,
		Status:        StatusReviewed,
		CurationState: CurationAgentCurated,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("NewEntry(target) error = %v", err)
	}
	if _, err := store.UpsertEntry(ctx, target, PreservedFields{}, "postgres link smoke"); err != nil {
		t.Fatalf("UpsertEntry(target) error = %v", err)
	}
	if err := store.ReplaceLinks(ctx, "fts-knowledge", []EntryLink{{ToSlug: "fts-linked-target", Kind: LinkKindRelated}}); err != nil {
		t.Fatalf("ReplaceLinks() error = %v", err)
	}
	if err := store.DeleteEntry(ctx, "fts-linked-target"); err != nil {
		t.Fatalf("DeleteEntry(target) error = %v", err)
	}
	links, err := store.ListLinks(ctx, "fts-knowledge", MaxNavigationLinks)
	if err != nil {
		t.Fatalf("ListLinks() error = %v", err)
	}
	if len(links) != 1 || links[0].Target.Slug != "fts-linked-target" || !links[0].Target.Missing {
		t.Fatalf("links after target delete = %#v", links)
	}
	if err := store.DeleteEntry(ctx, "fts-knowledge"); err != nil {
		t.Fatalf("DeleteEntry() error = %v", err)
	}
	if _, err := store.GetEntry(ctx, "fts-knowledge", true); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("GetEntry() after delete error = %v, want ErrEntryNotFound", err)
	}
}

func TestStorePostgresUpsertKeepsPreservedMetadata(t *testing.T) {
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
	_ = store.DeleteEntry(ctx, "preserved-metadata")
	now := time.Now().UTC().Truncate(time.Microsecond)
	due := now.Add(30 * 24 * time.Hour)
	first, err := NewEntry(NewEntryParams{
		Slug: "preserved-metadata", Title: "Preserved Metadata", BodyMarkdown: "first body", Kind: KindReference,
		Status: StatusReviewed, CurationState: CurationAgentCurated, Aliases: []string{"kept-alias"},
		SourceRefs: []SourceRef{{SourceKind: "document", SourceID: "_global/den-operating-contract"}}, AccuracyNotes: "checked",
		ReplacementSlug: "other-entry", LastReviewedAt: &now, ReviewDueAt: &due, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewEntry(first) error = %v", err)
	}
	if _, err := store.UpsertEntry(ctx, first, PreservedFields{}, "create"); err != nil {
		t.Fatalf("UpsertEntry(first) error = %v", err)
	}
	second, err := NewEntry(NewEntryParams{
		Slug: "preserved-metadata", Title: "Preserved Metadata", BodyMarkdown: "second body", Kind: KindReference,
		Status: StatusNeedsReview, CurationState: CurationNeedsRecheck, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewEntry(second) error = %v", err)
	}
	keepAll := PreservedFields{Aliases: true, SourceRefs: true, AccuracyNotes: true, ReplacementSlug: true, LastReviewedAt: true, ReviewDueAt: true}
	if _, err := store.UpsertEntry(ctx, second, keepAll, "body only"); err != nil {
		t.Fatalf("UpsertEntry(second) error = %v", err)
	}
	stored, err := store.GetEntry(ctx, "preserved-metadata", true)
	if err != nil {
		t.Fatalf("GetEntry() error = %v", err)
	}
	if stored.BodyMarkdown() != "second body" || len(stored.Aliases()) != 1 || len(stored.SourceRefs()) != 1 ||
		stored.AccuracyNotes() != "checked" || stored.ReplacementSlug() != "other-entry" ||
		stored.LastReviewedAt() == nil || !stored.LastReviewedAt().Equal(now) || stored.ReviewDueAt() == nil || !stored.ReviewDueAt().Equal(due) {
		t.Fatalf("preserved metadata lost: %#v", stored)
	}
	if err := store.DeleteEntry(ctx, "preserved-metadata"); err != nil {
		t.Fatalf("DeleteEntry() error = %v", err)
	}
}
