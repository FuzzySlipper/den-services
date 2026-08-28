package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProgressiveReadReturnsCompactRevisionReceiptAndSafeSection(t *testing.T) {
	ctx := context.Background()
	service := NewService(newMemoryStore(), fixedClock())
	entry := seedEntry(t, service, "navigation", "Navigation", StatusReviewed, []string{"navigation"}, "# Intro\nstart\n## Detail\ninside\n# End\nfinish")

	outline, err := service.ReadEntry(ctx, entry.Slug(), "outline", "", 0, "", false)
	if err != nil {
		t.Fatalf("ReadEntry(outline) error = %v", err)
	}
	if len(outline.Outline) != 3 || outline.Body != "" || outline.Revision != 1 {
		t.Fatalf("outline response = %#v", outline)
	}
	section, err := service.ReadEntry(ctx, entry.Slug(), "section", "intro", 0, "", false)
	if err != nil {
		t.Fatalf("ReadEntry(section) error = %v", err)
	}
	if !strings.Contains(section.Body, "## Detail") || strings.Contains(section.Body, "# End") {
		t.Fatalf("section body = %q", section.Body)
	}
	repeated, err := service.ReadEntry(ctx, entry.Slug(), "full", "", outline.Revision, "", false)
	if err != nil {
		t.Fatalf("ReadEntry(repeated) error = %v", err)
	}
	if !repeated.Unchanged || repeated.Body != "" || len(repeated.Links) != 0 {
		t.Fatalf("repeat receipt = %#v", repeated)
	}
	if _, err := service.StoreEntry(ctx, StoreEntryRequest{Slug: entry.Slug(), Title: entry.Title(), Summary: "changed", BodyMarkdown: "# Intro\nchanged", Kind: KindReference, Status: StatusReviewed, CurationState: CurationAgentCurated}); err != nil {
		t.Fatalf("StoreEntry(update) error = %v", err)
	}
	changed, err := service.ReadEntry(ctx, entry.Slug(), "full", "", outline.Revision, outline.Digest, false)
	if err != nil {
		t.Fatalf("ReadEntry(changed) error = %v", err)
	}
	if changed.Unchanged || changed.Revision != 2 || !strings.Contains(changed.Body, "changed") {
		t.Fatalf("changed read = %#v", changed)
	}
	if _, err := service.ReadEntry(ctx, entry.Slug(), "section", "does-not-exist", 0, "", false); !errors.Is(err, ErrSectionNotFound) {
		t.Fatalf("missing section error = %v", err)
	}
}

func TestProgressiveNavigationConcurrentCardReads(t *testing.T) {
	service := NewService(newMemoryStore(), fixedClock())
	seedEntry(t, service, "concurrent", "Concurrent", StatusReviewed, nil, "body")
	errors := make(chan error, 32)
	for index := 0; index < cap(errors); index++ {
		go func() {
			_, err := service.EntryCard(context.Background(), "concurrent", false)
			errors <- err
		}()
	}
	for index := 0; index < cap(errors); index++ {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent EntryCard() error = %v", err)
		}
	}
}

func TestMarkdownHeadingIDsSupportUnicodeAndLiteralHashes(t *testing.T) {
	sections := outlineMarkdown("# 日本語\nbody\n# C#\nmore\n# 日本語\nagain")
	if len(sections) != 3 {
		t.Fatalf("sections = %#v", sections)
	}
	if sections[0].ID == "" || sections[0].ID == sections[2].ID || sections[1].Title != "C#" {
		t.Fatalf("heading IDs/titles = %#v", sections)
	}
	if _, body, err := selectMarkdownSection("# 日本語\nbody\n# End\nstop", sections[0].ID); err != nil || !strings.Contains(body, "body") {
		t.Fatalf("unicode section = %q, %v", body, err)
	}
}

func TestMarkdownFenceRequiresMatchingMarkerAndRunLength(t *testing.T) {
	markdown := "# Intro\n`````go\n# hidden-one\n~~~\n# hidden-two\n````\n# hidden-three\n`````\n# Visible\nvisible body\n"
	sections := outlineMarkdown(markdown)
	if len(sections) != 2 || sections[0].Title != "Intro" || sections[1].Title != "Visible" {
		t.Fatalf("outline with mixed fences = %#v", sections)
	}
	intro, body, err := selectMarkdownSection(markdown, "intro")
	if err != nil {
		t.Fatalf("select intro: %v", err)
	}
	if intro.Title != "Intro" || !strings.Contains(body, "# hidden-three") || strings.Contains(body, "# Visible") {
		t.Fatalf("intro section body = %q", body)
	}
	visible, visibleBody, err := selectMarkdownSection(markdown, "visible")
	if err != nil || visible.Title != "Visible" || visibleBody != "# Visible\nvisible body" {
		t.Fatalf("visible section = %#v %q %v", visible, visibleBody, err)
	}
}

func TestNavigationHandlerCardsOmitBodiesAndExposeContinuation(t *testing.T) {
	service := NewService(newMemoryStore(), fixedClock())
	if _, err := service.StoreEntry(context.Background(), StoreEntryRequest{Slug: "http-card", Title: "HTTP Card", Summary: "safe summary", BodyMarkdown: "private full body", Kind: KindReference, Status: StatusReviewed, CurationState: CurationAgentCurated}); err != nil {
		t.Fatalf("StoreEntry() error = %v", err)
	}
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	request := httptest.NewRequest(http.MethodPost, "/v1/knowledge/entries/cards", strings.NewReader(`{"slugs":["http-card","missing"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "body_markdown") || strings.Contains(response.Body.String(), "private full body") {
		t.Fatalf("card response status/body = %d %s", response.Code, response.Body.String())
	}
	var result CardsResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode cards response: %v", err)
	}
	if len(result.Cards) != 1 || len(result.Missing) != 1 {
		t.Fatalf("cards response = %#v", result)
	}
	if result.Cards[0].LastReviewedAt == nil {
		t.Fatal("reviewed card omitted last_reviewed_at")
	}
}

func TestBatchCardsLinksAndMapsAreBoundedAndValidated(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	service := NewService(store, fixedClock())
	seedEntry(t, service, "one", "One", StatusReviewed, nil, "one")
	seedEntry(t, service, "two", "Two", StatusReviewed, nil, "two")
	seedEntry(t, service, "three", "Three", StatusReviewed, nil, "three")

	if err := service.ReplaceLinks(ctx, "one", []EntryLinkRequest{{ToSlug: "two", Kind: LinkKindRelated}}); err != nil {
		t.Fatalf("ReplaceLinks() error = %v", err)
	}
	if err := service.ReplaceLinks(ctx, "one", []EntryLinkRequest{{ToSlug: "missing", Kind: LinkKindRelated}}); !errors.Is(err, ErrInvalidLinkTarget) {
		t.Fatalf("missing target error = %v", err)
	}
	if err := service.ReplaceLinks(ctx, "one", []EntryLinkRequest{{ToSlug: "two", Kind: "mentions"}}); !errors.Is(err, ErrInvalidLinkKind) {
		t.Fatalf("invalid kind error = %v", err)
	}
	store.links = append(store.links, EntryLink{FromSlug: "one", ToSlug: "removed", Kind: LinkKindReplacement})
	read, err := service.ReadEntry(ctx, "one", "outline", "", 0, "", false)
	if err != nil {
		t.Fatalf("ReadEntry links error = %v", err)
	}
	if len(read.Links) != 2 || !read.Links[1].Target.Missing {
		t.Fatalf("resolved links = %#v", read.Links)
	}

	slugs := make([]string, 0, MaxBatchCards+2)
	for index := 0; index < MaxBatchCards+2; index++ {
		slugs = append(slugs, "missing-"+string(rune('a'+index)))
	}
	slugs[0], slugs[1], slugs[2] = "one", "two", "three"
	cards, err := service.BatchCards(ctx, slugs, false, 0)
	if err != nil {
		t.Fatalf("BatchCards() error = %v", err)
	}
	if len(cards.Cards) != 3 || cards.NextOffset == nil || len(cards.Missing) != MaxBatchCards-3 {
		t.Fatalf("batch cards = %#v", cards)
	}

	if _, err := service.StoreMap(ctx, StoreKnowledgeMapRequest{Slug: "starter", Title: "Starter", Entries: []KnowledgeMapEntryRequest{{EntrySlug: "one", GroupName: "start", Position: 0}, {EntrySlug: "two", Position: 1}}}); err != nil {
		t.Fatalf("StoreMap() error = %v", err)
	}
	mapResult, err := service.GetMap(ctx, "starter")
	if err != nil {
		t.Fatalf("GetMap() error = %v", err)
	}
	if len(mapResult.Entries) != 2 || mapResult.Entries[0].Card.Slug != "one" {
		t.Fatalf("map response = %#v", mapResult)
	}
	if _, err := service.StoreMap(ctx, StoreKnowledgeMapRequest{Slug: "bad", Title: "Bad", Entries: []KnowledgeMapEntryRequest{{EntrySlug: "missing", Position: 0}}}); !errors.Is(err, ErrInvalidLinkTarget) {
		t.Fatalf("invalid map reference error = %v", err)
	}
	if _, err := service.StoreMap(ctx, StoreKnowledgeMapRequest{Slug: "bad-position", Title: "Bad position", Entries: []KnowledgeMapEntryRequest{{EntrySlug: "one", Position: MaxMapEntries}}}); !errors.Is(err, ErrInvalidMap) {
		t.Fatalf("out-of-range map position error = %v", err)
	}
}
