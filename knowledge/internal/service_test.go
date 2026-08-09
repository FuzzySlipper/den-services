package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServiceHardDeletesEntryAndRevisionHistory(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, fixedClock())
	seedEntry(t, service, "remove-me", "Remove Me", StatusReviewed, []string{"delete"}, "remove this entry")
	seedEntry(t, service, "remove-me", "Remove Me Updated", StatusReviewed, []string{"delete"}, "updated body")

	if err := service.DeleteEntry(ctx, " remove-me "); err != nil {
		t.Fatalf("DeleteEntry() error = %v", err)
	}
	if _, err := service.GetEntry(ctx, "remove-me", true); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("GetEntry() error = %v, want ErrEntryNotFound", err)
	}
	revisions, err := service.ListRevisions(ctx, "remove-me")
	if err != nil {
		t.Fatalf("ListRevisions() error = %v", err)
	}
	if len(revisions) != 0 {
		t.Fatalf("revisions after hard delete = %#v", revisions)
	}
	if err := service.DeleteEntry(ctx, "remove-me"); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("second DeleteEntry() error = %v, want ErrEntryNotFound", err)
	}
}

func TestHandlerDeletesKnowledgeEntry(t *testing.T) {
	service := NewService(newMemoryStore(), fixedClock())
	seedEntry(t, service, "remove-me", "Remove Me", StatusReviewed, nil, "body")
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/v1/knowledge/entries/remove-me", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d body = %s", response.Code, response.Body.String())
	}
	var receipt DeleteEntryResponse
	if err := json.NewDecoder(response.Body).Decode(&receipt); err != nil {
		t.Fatalf("decoding delete receipt: %v", err)
	}
	if !receipt.Deleted || receipt.Slug != "remove-me" {
		t.Fatalf("delete receipt = %#v", receipt)
	}
}

func TestServiceKnowledgeReviewedDefaultsAndTagGates(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, fixedClock())

	seedEntry(t, service, "reviewed-one", "Reviewed One", StatusReviewed, []string{"knowledge", "go"}, "Postgres knowledge search guide")
	seedEntry(t, service, "draft-one", "Draft One", StatusDraft, []string{"knowledge", "draft"}, "Draft migration notes")
	seedEntry(t, service, "reviewed-two", "Reviewed Two", StatusReviewed, []string{"knowledge", "mcp"}, "MCP routing notes")

	results, err := service.SearchEntries(ctx, SearchQuery{Query: "knowledge", Limit: 10})
	if err != nil {
		t.Fatalf("SearchEntries() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("reviewed-only results = %#v", results)
	}

	results, err = service.SearchEntries(ctx, SearchQuery{Query: "knowledge", IncludeUnreviewed: true, Limit: 10})
	if err != nil {
		t.Fatalf("SearchEntries(include_unreviewed) error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("include_unreviewed results = %#v", results)
	}

	results, err = service.SearchEntries(ctx, SearchQuery{Query: "knowledge", RequiredTags: []string{"knowledge", "go"}, Limit: 10})
	if err != nil {
		t.Fatalf("SearchEntries(required_tags) error = %v", err)
	}
	if len(results) != 1 || results[0].Slug != "reviewed-one" {
		t.Fatalf("required tag results = %#v", results)
	}

	results, err = service.SearchEntries(ctx, SearchQuery{Query: "knowledge", AnyTags: []string{"mcp", "missing"}, Limit: 10})
	if err != nil {
		t.Fatalf("SearchEntries(any_tags) error = %v", err)
	}
	if len(results) != 1 || results[0].Slug != "reviewed-two" {
		t.Fatalf("any tag results = %#v", results)
	}
}

func TestServiceGetStoreRevisionAndGuideCitations(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, fixedClock())

	created := seedEntry(t, service, "guide-entry", "Guide Entry", StatusReviewed, []string{"guide"}, "Cited retrieval summary")
	if created.LastReviewedAt() == nil {
		t.Fatal("reviewed entry did not receive last_reviewed_at")
	}
	_, err := service.StoreEntry(ctx, StoreEntryRequest{
		Slug:          "guide-entry",
		Title:         "Guide Entry Updated",
		BodyMarkdown:  "Cited retrieval body updated.",
		Summary:       "Updated cited retrieval summary",
		Kind:          KindReference,
		Status:        StatusReviewed,
		CurationState: CurationAgentCurated,
		Tags:          []string{"guide"},
		ChangedBy:     "runner",
		ChangeNote:    "update for test",
	})
	if err != nil {
		t.Fatalf("StoreEntry(update) error = %v", err)
	}
	revisions, err := service.ListRevisions(ctx, "guide-entry")
	if err != nil {
		t.Fatalf("ListRevisions() error = %v", err)
	}
	if len(revisions) != 1 || revisions[0].ChangeNote != "update for test" {
		t.Fatalf("revisions = %#v", revisions)
	}
	entry, err := service.GetEntry(ctx, "guide-entry", false)
	if err != nil {
		t.Fatalf("GetEntry() error = %v", err)
	}
	if !strings.Contains(entry.BodyMarkdown(), "updated") {
		t.Fatalf("body_markdown not returned: %q", entry.BodyMarkdown())
	}
	guide, err := service.Guide(ctx, GuideQuery{Question: "How should cited retrieval work?", IncludeFollowUps: true})
	if err != nil {
		t.Fatalf("Guide() error = %v", err)
	}
	if len(guide.Citations) != 1 || guide.Citations[0].Slug != "guide-entry" {
		t.Fatalf("guide citations = %#v", guide.Citations)
	}
	if !strings.Contains(guide.Answer, "**Guide Entry Updated**") {
		t.Fatalf("guide answer = %q", guide.Answer)
	}
	gap, err := service.Guide(ctx, GuideQuery{Question: "zzzz unmatched term"})
	if err != nil {
		t.Fatalf("Guide(gap) error = %v", err)
	}
	if len(gap.Citations) != 0 || len(gap.Uncertainty) == 0 {
		t.Fatalf("gap response = %#v", gap)
	}
}

func TestServiceDocumentSearchExclusionByBoundary(t *testing.T) {
	service := NewService(newMemoryStore(), fixedClock())
	if _, ok := any(service).(interface {
		SearchDocuments(context.Context, string, string) ([]string, error)
	}); ok {
		t.Fatal("knowledge service exposed document search behavior")
	}
	if !errors.Is(ErrKnowledgeNotDocuments, ErrKnowledgeNotDocuments) {
		t.Fatal("document-search exclusion sentinel is unavailable")
	}
}

func seedEntry(t *testing.T, service *Service, slug string, title string, status string, tags []string, body string) *Entry {
	t.Helper()
	entry, err := service.StoreEntry(context.Background(), StoreEntryRequest{
		Slug:          slug,
		Title:         title,
		BodyMarkdown:  body,
		Summary:       body,
		Kind:          KindReference,
		Status:        status,
		CurationState: CurationAgentCurated,
		Tags:          tags,
		ChangedBy:     "test",
	})
	if err != nil {
		t.Fatalf("StoreEntry(%s) error = %v", slug, err)
	}
	return entry
}

func fixedClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	}
}
