package guidance

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

func TestServiceAddListResolveGuidance(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	docs := &fakeDocuments{documents: map[string]*Document{
		"_global/global-doc": {
			ProjectID: "_global", Slug: "global-doc", Title: "Global Guidance", Content: "Global rules.", DocType: "spec", Visibility: VisibilityNormal,
			UpdatedAt: time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC),
		},
		"den-services/project-doc": {
			ProjectID: "den-services", Slug: "project-doc", Title: "Project Guidance", Content: "Project rules.", DocType: "spec", Visibility: VisibilityNormal,
			UpdatedAt: time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC),
		},
	}}
	service := NewService(store, fakeProjects{}, docs, fixedClock, 4096)

	global, err := service.AddEntry(ctx, "_global", AddEntryRequest{DocumentProjectID: "_global", DocumentSlug: "global-doc", Importance: ImportanceRequired, SortOrder: 10})
	if err != nil {
		t.Fatalf("AddEntry(global) error = %v", err)
	}
	project, err := service.AddEntry(ctx, "den-services", AddEntryRequest{DocumentSlug: "project-doc", Importance: ImportanceImportant, Audience: []string{"runner"}, SortOrder: 20})
	if err != nil {
		t.Fatalf("AddEntry(project) error = %v", err)
	}

	entries, err := service.ListEntries(ctx, "den-services", true)
	if err != nil {
		t.Fatalf("ListEntries() error = %v", err)
	}
	if len(entries) != 2 || entries[0].ID != global.ID || entries[1].ID != project.ID {
		t.Fatalf("entries order = %#v, want global then project", entries)
	}

	packet, err := service.Resolve(ctx, ResolveQuery{ProjectID: "den-services", IncludeContent: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(packet.Sources) != 2 {
		t.Fatalf("source count = %d, want 2", len(packet.Sources))
	}
	if packet.Incomplete || packet.Truncated {
		t.Fatalf("packet incomplete=%t truncated=%t, want false/false", packet.Incomplete, packet.Truncated)
	}
	if packet.ContentSHA256 == "" || packet.ContentBytes == 0 {
		t.Fatalf("packet digest/bytes missing: %#v", packet)
	}
}

func TestServiceRejectsHiddenDocumentOnAdd(t *testing.T) {
	service := NewService(newMemoryStore(), fakeProjects{}, &fakeDocuments{documents: map[string]*Document{
		"den-services/hidden": {ProjectID: "den-services", Slug: "hidden", Title: "Hidden", Content: "hidden", Visibility: VisibilityHidden},
	}}, fixedClock, 4096)

	_, err := service.AddEntry(context.Background(), "den-services", AddEntryRequest{DocumentSlug: "hidden"})
	if !errors.Is(err, ErrDocumentNotVisible) {
		t.Fatalf("AddEntry(hidden) error = %v, want %v", err, ErrDocumentNotVisible)
	}
}

func TestServiceAddEntryAcceptsSameScopeDocumentProjectFromDocumentsClient(t *testing.T) {
	ctx := context.Background()
	documentUpdatedAt := time.Date(2026, 7, 2, 0, 58, 22, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/rusty-crew/documents/architecture-phase0-boundary-clarifications-2026-07-01" {
			t.Fatalf("unexpected document request %s %s", r.Method, r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         1643,
			"project_id": "rusty-crew",
			"slug":       "architecture-phase0-boundary-clarifications-2026-07-01",
			"title":      "Architecture Phase 0 Boundary Clarifications",
			"content":    "Boundary clarification.",
			"doc_type":   "spec",
			"visibility": "normal",
			"tags":       []string{"architecture", "guidance"},
			"summary":    "Phase 0 clarification.",
			"updated_at": documentUpdatedAt.Format(time.RFC3339),
		})
	}))
	defer server.Close()

	store := newMemoryStore()
	service := NewService(store, fakeProjects{}, NewDocumentsClient(server.URL, ""), fixedClock, 4096)
	entry, err := service.AddEntry(ctx, "rusty-crew", AddEntryRequest{
		DocumentProjectID: "rusty-crew",
		DocumentSlug:      "architecture-phase0-boundary-clarifications-2026-07-01",
		Importance:        ImportanceRequired,
		SortOrder:         15,
		Notes:             "Active amendment clarifying Phase 0 Rusty Crew architecture boundaries.",
	})
	if err != nil {
		t.Fatalf("AddEntry() error = %v", err)
	}
	if entry.ProjectID != "rusty-crew" || entry.DocumentProjectID != "rusty-crew" {
		t.Fatalf("entry scope/document project = %s/%s, want rusty-crew/rusty-crew", entry.ProjectID, entry.DocumentProjectID)
	}
	if entry.DocumentSlug != "architecture-phase0-boundary-clarifications-2026-07-01" {
		t.Fatalf("entry slug = %q", entry.DocumentSlug)
	}

	entries, err := service.ListEntries(ctx, "rusty-crew", false)
	if err != nil {
		t.Fatalf("ListEntries() error = %v", err)
	}
	if len(entries) != 1 || entries[0].DocumentProjectID != "rusty-crew" {
		t.Fatalf("persisted entries = %#v", entries)
	}
}

func TestServiceDeleteMissingEntryReturnsNotFound(t *testing.T) {
	service := NewService(newMemoryStore(), fakeProjects{}, &fakeDocuments{}, fixedClock, 4096)

	err := service.DeleteEntry(context.Background(), "den-services", 99)
	if !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("DeleteEntry(missing) error = %v, want %v", err, ErrEntryNotFound)
	}
}

func TestServiceResolveAttemptsRequiredBeforeImportantUnderBudgetPressure(t *testing.T) {
	ctx := context.Background()
	important := Entry{
		ID: 1, ProjectID: GlobalProjectID, DocumentProjectID: GlobalProjectID, DocumentSlug: "important", Importance: ImportanceImportant,
	}
	required := Entry{
		ID: 2, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "required", Importance: ImportanceRequired,
	}
	importantDocument := &Document{ProjectID: GlobalProjectID, Slug: "important", Title: "Global Important", Content: strings.Repeat("i", 128), Visibility: VisibilityNormal}
	requiredDocument := &Document{ProjectID: "den-services", Slug: "required", Title: "Project Required", Content: "required", Visibility: VisibilityNormal}
	maxBytes := len(guidancePreamble("den-services")) + len(guidanceSection(required, requiredDocument, true))
	service := NewService(&memoryStore{entries: []Entry{important, required}}, fakeProjects{}, &fakeDocuments{documents: map[string]*Document{
		"_global/important":     importantDocument,
		"den-services/required": requiredDocument,
	}}, fixedClock, maxBytes)

	packet, err := service.Resolve(ctx, ResolveQuery{ProjectID: "den-services", IncludeContent: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(packet.Sources) != 1 || packet.Sources[0].EntryID != required.ID {
		t.Fatalf("sources = %#v, want only project required entry %d", packet.Sources, required.ID)
	}
	if !packet.Truncated || !packet.Incomplete {
		t.Fatalf("truncated/incomplete = %t/%t, want true/true", packet.Truncated, packet.Incomplete)
	}
	if len(packet.SkippedSources) != 1 || packet.SkippedSources[0].EntryID != important.ID {
		t.Fatalf("skipped = %#v, want global important entry %d", packet.SkippedSources, important.ID)
	}
}

func TestServiceResolveAcceptsSectionAtExactByteLimit(t *testing.T) {
	entry := Entry{ID: 1, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "required", Importance: ImportanceRequired}
	document := &Document{ProjectID: "den-services", Slug: "required", Title: "Required", Content: "content", Visibility: VisibilityNormal}
	maxBytes := len(guidancePreamble("den-services")) + len(guidanceSection(entry, document, true))
	service := NewService(&memoryStore{entries: []Entry{entry}}, fakeProjects{}, &fakeDocuments{documents: map[string]*Document{
		"den-services/required": document,
	}}, fixedClock, maxBytes)

	packet, err := service.Resolve(context.Background(), ResolveQuery{ProjectID: "den-services", IncludeContent: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(packet.Sources) != 1 || packet.Truncated || packet.Incomplete {
		t.Fatalf("packet = %#v, want one complete source", packet)
	}
	if packet.ContentBytes != maxBytes {
		t.Fatalf("content bytes = %d, want exact limit %d", packet.ContentBytes, maxBytes)
	}
}

func TestServiceResolveNeverExceedsBudgetSmallerThanPreamble(t *testing.T) {
	entry := Entry{ID: 1, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "required", Importance: ImportanceRequired}
	document := &Document{ProjectID: "den-services", Slug: "required", Title: "Required", Content: "content", Visibility: VisibilityNormal}
	service := NewService(&memoryStore{entries: []Entry{entry}}, fakeProjects{}, &fakeDocuments{documents: map[string]*Document{
		"den-services/required": document,
	}}, fixedClock, 1024)

	packet, err := service.Resolve(context.Background(), ResolveQuery{ProjectID: "den-services", MaxBytes: 1, IncludeContent: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if packet.ContentBytes > 1 || len(packet.ContentMarkdown) > 1 {
		t.Fatalf("content = %q (%d bytes), want at most 1 byte", packet.ContentMarkdown, packet.ContentBytes)
	}
	if !packet.Truncated || !packet.Incomplete {
		t.Fatalf("truncated/incomplete = %t/%t, want true/true", packet.Truncated, packet.Incomplete)
	}
	if len(packet.SkippedSources) != 1 || packet.SkippedSources[0].EntryID != entry.ID {
		t.Fatalf("skipped = %#v, want required source %d", packet.SkippedSources, entry.ID)
	}
}

func TestServiceResolveSkipsOversizeRequiredAndContinuesRequiredTier(t *testing.T) {
	oversize := Entry{ID: 1, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "oversize", Importance: ImportanceRequired}
	fitting := Entry{ID: 2, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "fitting", Importance: ImportanceRequired}
	oversizeDocument := &Document{ProjectID: "den-services", Slug: "oversize", Title: "Oversize", Content: strings.Repeat("o", 128), Visibility: VisibilityNormal}
	fittingDocument := &Document{ProjectID: "den-services", Slug: "fitting", Title: "Fitting", Content: "fits", Visibility: VisibilityNormal}
	maxBytes := len(guidancePreamble("den-services")) + len(guidanceSection(fitting, fittingDocument, true))
	service := NewService(&memoryStore{entries: []Entry{oversize, fitting}}, fakeProjects{}, &fakeDocuments{documents: map[string]*Document{
		"den-services/oversize": oversizeDocument,
		"den-services/fitting":  fittingDocument,
	}}, fixedClock, maxBytes)

	packet, err := service.Resolve(context.Background(), ResolveQuery{ProjectID: "den-services", IncludeContent: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(packet.Sources) != 1 || packet.Sources[0].EntryID != fitting.ID {
		t.Fatalf("sources = %#v, want only fitting required entry", packet.Sources)
	}
	if len(packet.SkippedSources) != 1 || packet.SkippedSources[0].EntryID != oversize.ID || !packet.SkippedSources[0].Required {
		t.Fatalf("skipped = %#v, want oversize required entry", packet.SkippedSources)
	}
}

func TestServiceResolveMarksSingleOversizeRequiredAsIncomplete(t *testing.T) {
	entry := Entry{ID: 1, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "oversize", Importance: ImportanceRequired}
	document := &Document{ProjectID: "den-services", Slug: "oversize", Title: "Oversize", Content: strings.Repeat("o", 128), Visibility: VisibilityNormal}
	maxBytes := len(guidancePreamble("den-services")) + len(guidanceSection(entry, document, true)) - 1
	service := NewService(&memoryStore{entries: []Entry{entry}}, fakeProjects{}, &fakeDocuments{documents: map[string]*Document{
		"den-services/oversize": document,
	}}, fixedClock, maxBytes)

	packet, err := service.Resolve(context.Background(), ResolveQuery{ProjectID: "den-services", IncludeContent: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(packet.Sources) != 0 || !packet.Truncated || !packet.Incomplete {
		t.Fatalf("packet = %#v, want no sources and truncated incomplete packet", packet)
	}
	if len(packet.SkippedSources) != 1 || !packet.SkippedSources[0].Required {
		t.Fatalf("skipped = %#v, want one required skipped source", packet.SkippedSources)
	}
}

func TestServiceResolveKeepsStableOrderWhenRequiredAggregateExceedsBudget(t *testing.T) {
	first := Entry{ID: 1, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "first", Importance: ImportanceRequired}
	second := Entry{ID: 2, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "second", Importance: ImportanceRequired}
	firstDocument := &Document{ProjectID: "den-services", Slug: "first", Title: "First", Content: "first", Visibility: VisibilityNormal}
	secondDocument := &Document{ProjectID: "den-services", Slug: "second", Title: "Second", Content: "second", Visibility: VisibilityNormal}
	maxBytes := len(guidancePreamble("den-services")) + len(guidanceSection(first, firstDocument, true))
	service := NewService(&memoryStore{entries: []Entry{first, second}}, fakeProjects{}, &fakeDocuments{documents: map[string]*Document{
		"den-services/first":  firstDocument,
		"den-services/second": secondDocument,
	}}, fixedClock, maxBytes)

	packet, err := service.Resolve(context.Background(), ResolveQuery{ProjectID: "den-services", IncludeContent: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(packet.Sources) != 1 || packet.Sources[0].EntryID != first.ID {
		t.Fatalf("sources = %#v, want first required source", packet.Sources)
	}
	if len(packet.SkippedSources) != 1 || packet.SkippedSources[0].EntryID != second.ID || !packet.SkippedSources[0].Required {
		t.Fatalf("skipped = %#v, want second required source", packet.SkippedSources)
	}
}

func TestAudienceMatchesAllWildcard(t *testing.T) {
	tests := []struct {
		name      string
		entry     []string
		requested []string
		want      bool
	}{
		{name: "all matches named audience", entry: []string{"all"}, requested: []string{"runner"}, want: true},
		{name: "all matches multiple audiences", entry: []string{"reviewer", "all"}, requested: []string{"runner", "playtester"}, want: true},
		{name: "named audience remains exact", entry: []string{"reviewer"}, requested: []string{"runner"}, want: false},
		{name: "requested all does not broaden named entry", entry: []string{"reviewer"}, requested: []string{"all"}, want: false},
		{name: "empty request remains unrestricted", entry: []string{"reviewer"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := audienceMatches(test.entry, test.requested); got != test.want {
				t.Fatalf("audienceMatches(%q, %q) = %t, want %t", test.entry, test.requested, got, test.want)
			}
		})
	}
}

func guidancePreamble(projectID string) string {
	return "# Den Agent Guidance for " + projectID + "\n\n"
}

type memoryStore struct {
	nextID  int64
	entries []Entry
}

func newMemoryStore() *memoryStore {
	return &memoryStore{nextID: 1}
}

func (s *memoryStore) Ping(context.Context) error { return nil }

func (s *memoryStore) UpsertEntry(_ context.Context, entry *Entry) (*Entry, error) {
	for index := range s.entries {
		existing := &s.entries[index]
		if existing.ProjectID == entry.ProjectID && existing.DocumentProjectID == entry.DocumentProjectID && existing.DocumentSlug == entry.DocumentSlug {
			entry.ID = existing.ID
			entry.CreatedAt = existing.CreatedAt
			s.entries[index] = *entry
			return entry, nil
		}
	}
	entry.ID = s.nextID
	s.nextID++
	s.entries = append(s.entries, *entry)
	return entry, nil
}

func (s *memoryStore) ListEntries(_ context.Context, projectID string, includeGlobal bool) ([]Entry, error) {
	result := []Entry{}
	for _, entry := range s.entries {
		if entry.ProjectID == projectID || (includeGlobal && entry.ProjectID == GlobalProjectID) {
			result = append(result, entry)
		}
	}
	return result, nil
}

func (s *memoryStore) DeleteEntry(_ context.Context, projectID string, entryID int64) (bool, error) {
	for index, entry := range s.entries {
		if entry.ProjectID == projectID && entry.ID == entryID {
			s.entries = append(s.entries[:index], s.entries[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (s *memoryStore) DocumentReferences(_ context.Context, documentProjectID string, documentSlug string) ([]DocumentReference, error) {
	refs := []DocumentReference{}
	for _, entry := range s.entries {
		if entry.DocumentProjectID == documentProjectID && entry.DocumentSlug == documentSlug {
			refs = append(refs, DocumentReference{EntryID: entry.ID, ScopeProjectID: entry.ProjectID, Importance: entry.Importance})
		}
	}
	return refs, nil
}

type fakeProjects struct{}

func (fakeProjects) AssertWritable(context.Context, string) error { return nil }

type fakeDocuments struct {
	documents map[string]*Document
}

func (f *fakeDocuments) GetDocument(_ context.Context, projectID string, slug string) (*Document, error) {
	if f.documents == nil {
		return nil, ErrDocumentUnavailable
	}
	doc, ok := f.documents[projectID+"/"+slug]
	if !ok {
		return nil, ErrDocumentUnavailable
	}
	copy := *doc
	return &copy, nil
}

func fixedClock() time.Time {
	return time.Date(2026, 7, 1, 3, 0, 0, 0, time.UTC)
}
