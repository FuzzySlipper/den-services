package guidance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type memoryBindingStore struct {
	nextID   int64
	bindings []KnowledgeBinding
}

func (s *memoryBindingStore) CreateBinding(_ context.Context, binding *KnowledgeBinding) (*KnowledgeBinding, error) {
	for _, existing := range s.bindings {
		if existing.TargetKind == binding.TargetKind && existing.TargetRef == binding.TargetRef && existing.ScopeKind == binding.ScopeKind && existing.ScopeRef == binding.ScopeRef {
			return nil, ErrDuplicateBinding
		}
	}
	s.nextID++
	binding.ID = s.nextID
	s.bindings = append(s.bindings, *binding)
	return binding, nil
}

func (s *memoryBindingStore) GetBinding(_ context.Context, id int64) (*KnowledgeBinding, error) {
	for _, binding := range s.bindings {
		if binding.ID == id {
			copy := binding
			return &copy, nil
		}
	}
	return nil, ErrBindingNotFound
}

func (s *memoryBindingStore) ListBindings(_ context.Context, query BindingListQuery) (BindingListResult, error) {
	bindings := append([]KnowledgeBinding(nil), s.bindings...)
	if query.Limit == 0 {
		return BindingListResult{Bindings: bindings}, nil
	}
	if query.Offset >= len(bindings) {
		return BindingListResult{Bindings: []KnowledgeBinding{}}, nil
	}
	end := query.Offset + query.Limit
	if end > len(bindings) {
		end = len(bindings)
	}
	result := BindingListResult{Bindings: bindings[query.Offset:end]}
	if end < len(bindings) {
		next := end
		result.NextOffset = &next
	}
	return result, nil
}

func (s *memoryBindingStore) UpdateBinding(_ context.Context, binding *KnowledgeBinding) (*KnowledgeBinding, error) {
	for index := range s.bindings {
		if s.bindings[index].ID == binding.ID {
			for _, other := range s.bindings {
				if other.ID != binding.ID && other.TargetKind == binding.TargetKind && other.TargetRef == binding.TargetRef && other.ScopeKind == binding.ScopeKind && other.ScopeRef == binding.ScopeRef {
					return nil, ErrDuplicateBinding
				}
			}
			s.bindings[index] = *binding
			return binding, nil
		}
	}
	return nil, ErrBindingNotFound
}

func (s *memoryBindingStore) DeleteBinding(_ context.Context, id int64) (bool, error) {
	for index, binding := range s.bindings {
		if binding.ID == id {
			s.bindings = append(s.bindings[:index], s.bindings[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}

type fakeKnowledge struct {
	entries     map[string]*KnowledgeMetadata
	unavailable bool
	calls       int
}

func (f *fakeKnowledge) GetKnowledge(_ context.Context, slug string) (*KnowledgeMetadata, error) {
	f.calls++
	if f.unavailable {
		return nil, errors.New("knowledge backend unavailable")
	}
	entry, ok := f.entries[slug]
	if !ok {
		return nil, ErrKnowledgeTargetNotFound
	}
	copy := *entry
	return &copy, nil
}

func TestKnowledgeBindingsResolveScopePolicyShadowAndBudget(t *testing.T) {
	store, bindings := newMemoryStore(), &memoryBindingStore{}
	knowledge := &fakeKnowledge{entries: map[string]*KnowledgeMetadata{"shared-concept": {Slug: "shared-concept", Title: "Shared", Summary: "1234", Status: "reviewed", CurationState: "human_curated", Revision: 4, RevisionKnown: true}}}
	service := NewServiceWithKnowledge(store, bindings, fakeProjects{}, &fakeDocuments{}, knowledge, fixedClock, 4096)
	global, err := service.CreateKnowledgeBinding(context.Background(), CreateKnowledgeBindingRequest{TargetRef: "shared-concept", ScopeKind: ScopeGlobal, ScopeRef: GlobalScopeRef, ReadPolicy: ReadPolicyInline, ReadWhen: "always"})
	if err != nil {
		t.Fatalf("CreateKnowledgeBinding(global) = %v", err)
	}
	project, err := service.CreateKnowledgeBinding(context.Background(), CreateKnowledgeBindingRequest{TargetRef: "shared-concept", ScopeKind: ScopeProject, ScopeRef: "den-services", ReadPolicy: ReadPolicyMustRead, ReadWhen: "project work", ShadowsGlobal: true})
	if err != nil {
		t.Fatalf("CreateKnowledgeBinding(project) = %v", err)
	}
	if global.ID == project.ID {
		t.Fatal("distinct scopes must produce distinct bindings")
	}
	resolution, err := service.ResolveKnowledgeBindings(context.Background(), BindingResolveQuery{Scopes: []BindingScope{{Kind: ScopeProject, Ref: "den-services"}}, InlineBudget: 0})
	if err != nil {
		t.Fatalf("ResolveKnowledgeBindings() = %v", err)
	}
	if len(resolution.Selections) != 1 || resolution.Selections[0].Binding.ID != project.ID || resolution.Selections[0].Binding.ReadPolicy != ReadPolicyMustRead {
		t.Fatalf("selections = %#v, want project binding only", resolution.Selections)
	}
	if len(resolution.ExcludedBindings) != 1 || resolution.ExcludedBindings[0].Binding.ID != global.ID || resolution.ExcludedBindings[0].TargetState != "shadowed_by_applicable_binding" {
		t.Fatalf("excluded = %#v, want shadowed global", resolution.ExcludedBindings)
	}
	if resolution.InlineUsed != 0 {
		t.Fatalf("zero budget inline used = %d, want 0", resolution.InlineUsed)
	}
	if _, err := service.CreateKnowledgeBinding(context.Background(), CreateKnowledgeBindingRequest{TargetRef: "shared-concept", ScopeKind: ScopeTask, ScopeRef: "42"}); err != nil {
		t.Fatalf("same-target provenance row = %v", err)
	}
	knowledge.calls = 0
	if _, err := service.ResolveKnowledgeBindings(context.Background(), BindingResolveQuery{Scopes: []BindingScope{{Kind: ScopeProject, Ref: "den-services"}, {Kind: ScopeTask, Ref: "42"}}}); err != nil {
		t.Fatal(err)
	}
	if knowledge.calls != 1 {
		t.Fatalf("knowledge reads = %d, want one cached target read", knowledge.calls)
	}
}

func TestKnowledgeBindingsResolvePolicyOrderingTargetStateAndLegacyProjection(t *testing.T) {
	entries := map[string]*KnowledgeMetadata{
		"inline":   {Slug: "inline", Title: "Inline", Summary: "1234", Status: "reviewed", CurationState: "human_curated"},
		"must":     {Slug: "must", Title: "Must", Status: "reviewed", CurationState: "human_curated"},
		"demand":   {Slug: "demand", Title: "Demand", Status: "deprecated", CurationState: "needs_recheck"},
		"latent":   {Slug: "latent", Title: "Latent", Status: "reviewed", CurationState: "human_curated"},
		"replaced": {Slug: "replaced", Title: "Replaced", Status: "deprecated", ReplacementSlug: "new-target", CurationState: "human_curated"},
	}
	store, bindings := newMemoryStore(), &memoryBindingStore{}
	service := NewServiceWithKnowledge(store, bindings, fakeProjects{}, &fakeDocuments{}, &fakeKnowledge{entries: entries}, fixedClock, 4096)
	for _, item := range []struct{ slug, policy string }{{"inline", ReadPolicyInline}, {"must", ReadPolicyMustRead}, {"demand", ReadPolicyOnDemand}, {"latent", ReadPolicyLatent}, {"replaced", ReadPolicyOnDemand}} {
		if _, err := service.CreateKnowledgeBinding(context.Background(), CreateKnowledgeBindingRequest{TargetRef: item.slug, ScopeKind: ScopeTask, ScopeRef: "42", ReadPolicy: item.policy}); err != nil {
			t.Fatalf("create %s: %v", item.slug, err)
		}
	}
	resolution, err := service.ResolveKnowledgeBindings(context.Background(), BindingResolveQuery{Scopes: []BindingScope{{Kind: ScopeTask, Ref: "42"}}, InlineBudget: 4})
	if err != nil {
		t.Fatalf("ResolveKnowledgeBindings = %v", err)
	}
	if got := []string{resolution.Selections[0].Binding.ReadPolicy, resolution.Selections[1].Binding.ReadPolicy, resolution.Selections[2].Binding.ReadPolicy, resolution.Selections[3].Binding.ReadPolicy}; strings.Join(got, ",") != "must_read,inline,on_demand,latent" {
		t.Fatalf("policy order = %v", got)
	}
	if !resolution.Selections[1].InlineAdmitted || resolution.InlineUsed != 4 || resolution.Selections[2].TargetState != "deprecated" {
		t.Fatalf("resolution = %#v", resolution)
	}
	if len(resolution.ExcludedBindings) != 1 || resolution.ExcludedBindings[0].TargetState != "superseded" {
		t.Fatalf("excluded = %#v", resolution.ExcludedBindings)
	}
	delete(entries, "latent")
	resolution, err = service.ResolveKnowledgeBindings(context.Background(), BindingResolveQuery{Scopes: []BindingScope{{Kind: ScopeTask, Ref: "42"}}})
	if err != nil || len(resolution.ExcludedBindings) != 2 {
		t.Fatalf("missing target resolution = %#v, %v", resolution, err)
	}
	legacy, err := service.AddEntry(context.Background(), "den-services", AddEntryRequest{DocumentSlug: "legacy-document", Importance: ImportanceRequired})
	if err == nil || legacy != nil {
		t.Fatal("legacy setup needs document")
	}
	store.entries = append(store.entries, Entry{ID: 99, ProjectID: "den-services", DocumentProjectID: "den-services", DocumentSlug: "legacy-document", Importance: ImportanceRequired})
	contextResolution, err := service.ResolveContext(context.Background(), "den-services", BindingResolveQuery{Scopes: []BindingScope{{Kind: ScopeTask, Ref: "42"}}})
	if err != nil {
		t.Fatalf("ResolveContext = %v", err)
	}
	found := false
	for _, item := range contextResolution.Selections {
		if item.SelectionSource == "legacy_document_guidance" && item.Binding.TargetKind == "document" {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy document projection absent: %#v", contextResolution.Selections)
	}
}

func TestResolveContextCapsCombinedKnowledgeAndLegacySelections(t *testing.T) {
	store := newMemoryStore()
	for index := 0; index < MaxResolvedBindings+1; index++ {
		store.entries = append(store.entries, Entry{
			ID:                int64(index + 1),
			ProjectID:         "den-services",
			DocumentProjectID: "den-services",
			DocumentSlug:      fmt.Sprintf("legacy-%03d", index),
			SortOrder:         index,
		})
	}
	service := NewServiceWithKnowledge(store, &memoryBindingStore{}, fakeProjects{}, &fakeDocuments{}, &fakeKnowledge{entries: map[string]*KnowledgeMetadata{}}, fixedClock, 4096)
	resolution, err := service.ResolveContext(context.Background(), "den-services", BindingResolveQuery{})
	if err != nil {
		t.Fatalf("ResolveContext = %v", err)
	}
	if len(resolution.Selections) != MaxResolvedBindings || !resolution.Truncated {
		t.Fatalf("combined resolution count/truncated = %d/%t, want %d/true", len(resolution.Selections), resolution.Truncated, MaxResolvedBindings)
	}
}

func TestBindingCapRetainsMustReadAcrossLessSpecificScope(t *testing.T) {
	items := make([]KnowledgeBinding, 0, MaxResolvedBindings+1)
	for index := 0; index < MaxResolvedBindings; index++ {
		items = append(items, KnowledgeBinding{ID: int64(index + 1), TargetRef: fmt.Sprintf("task-%03d", index), ScopeKind: ScopeTask, ReadPolicy: ReadPolicyOnDemand})
	}
	items = append(items, KnowledgeBinding{ID: 1000, TargetRef: "global-required", ScopeKind: ScopeGlobal, ReadPolicy: ReadPolicyMustRead})
	sortBindings(items)
	selected := capApplicableBindings(items, MaxResolvedBindings)
	found := false
	for _, item := range selected {
		found = found || item.TargetRef == "global-required"
	}
	if len(selected) != MaxResolvedBindings || !found {
		t.Fatalf("selected count/required = %d/%t", len(selected), found)
	}
}

func TestCombinedResolutionCapRetainsMustRead(t *testing.T) {
	items := make([]ResolvedKnowledgeBinding, 0, MaxResolvedBindings+1)
	for index := 0; index < MaxResolvedBindings; index++ {
		items = append(items, ResolvedKnowledgeBinding{Binding: KnowledgeBinding{ID: int64(index + 1), TargetRef: fmt.Sprintf("task-%03d", index), ScopeKind: ScopeTask, ReadPolicy: ReadPolicyOnDemand}})
	}
	items = append(items, ResolvedKnowledgeBinding{Binding: KnowledgeBinding{ID: 1000, TargetRef: "global-required", ScopeKind: ScopeGlobal, ReadPolicy: ReadPolicyMustRead}})
	sortResolvedBindings(items)
	selected := capResolvedBindings(items, MaxResolvedBindings)
	found := false
	for _, item := range selected {
		found = found || item.Binding.TargetRef == "global-required"
	}
	if len(selected) != MaxResolvedBindings || !found {
		t.Fatalf("selected count/required = %d/%t", len(selected), found)
	}
}

func TestKnowledgeClientRequestsArchivedMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/knowledge/entries/archived-card/card":
			if r.URL.Query().Get("include_archived") != "true" {
				t.Fatalf("include_archived = %q, want true", r.URL.Query().Get("include_archived"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "archived-card", "title": "Archived", "status": "archived", "revision": 3, "updated_at": fixedClock().Format(time.RFC3339)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	metadata, err := NewKnowledgeClient(server.URL, "").GetKnowledge(context.Background(), "archived-card")
	if err != nil {
		t.Fatalf("GetKnowledge = %v", err)
	}
	if metadata.Status != "archived" {
		t.Fatalf("status = %q, want archived", metadata.Status)
	}
	if metadata.Revision != 3 || !metadata.RevisionKnown {
		t.Fatalf("revision = %d/%t, want 3/true", metadata.Revision, metadata.RevisionKnown)
	}
}

func TestKnowledgeBindingValidationAndBackendFailure(t *testing.T) {
	service := NewServiceWithKnowledge(newMemoryStore(), &memoryBindingStore{}, fakeProjects{}, &fakeDocuments{}, &fakeKnowledge{entries: map[string]*KnowledgeMetadata{"known": {Slug: "known"}}}, fixedClock, 4096)
	for _, request := range []CreateKnowledgeBindingRequest{{TargetRef: "", ScopeKind: ScopeProject, ScopeRef: "den-services"}, {TargetRef: "known", ScopeKind: ScopeGlobal, ScopeRef: ""}, {TargetRef: "known", ScopeKind: ScopeTask, ScopeRef: "x"}, {TargetRef: "known", ScopeKind: ScopeProject, ScopeRef: "den-services", ReadPolicy: "later"}, {TargetRef: "known", ScopeKind: ScopeProject, ScopeRef: "den-services", Audience: []string{""}}} {
		if _, err := service.CreateKnowledgeBinding(context.Background(), request); err == nil {
			t.Fatalf("invalid binding accepted: %#v", request)
		}
	}
	service.knowledge = &fakeKnowledge{unavailable: true}
	if _, err := service.CreateKnowledgeBinding(context.Background(), CreateKnowledgeBindingRequest{TargetRef: "known", ScopeKind: ScopeProject, ScopeRef: "den-services"}); err == nil {
		t.Fatal("backend failure accepted binding")
	}
}

func TestKnowledgeBindingRESTCRUDAndResolveReadback(t *testing.T) {
	service := NewServiceWithKnowledge(newMemoryStore(), &memoryBindingStore{}, fakeProjects{}, &fakeDocuments{}, &fakeKnowledge{entries: map[string]*KnowledgeMetadata{"api-target": {Slug: "api-target", Title: "API Target", Summary: "summary", Status: "reviewed", CurationState: "human_curated"}}}, fixedClock, 4096)
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
		return recorder
	}
	created := request(http.MethodPost, "/v1/guidance/knowledge-bindings", `{"target_ref":"api-target","scope_kind":"project","scope_ref":"den-services","read_policy":"inline","read_when":"api test"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", created.Code, created.Body.String())
	}
	var binding KnowledgeBindingResponse
	if err := json.NewDecoder(created.Body).Decode(&binding); err != nil {
		t.Fatal(err)
	}
	listed := request(http.MethodGet, "/v1/guidance/knowledge-bindings", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"count":1`) {
		t.Fatalf("list status/body = %d/%s", listed.Code, listed.Body.String())
	}
	updated := request(http.MethodPut, fmt.Sprintf("/v1/guidance/knowledge-bindings/%d", binding.ID), `{"scope_kind":"project","scope_ref":"den-services","read_policy":"must_read","read_when":"updated"}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"read_policy":"must_read"`) {
		t.Fatalf("update status/body = %d/%s", updated.Code, updated.Body.String())
	}
	resolved := request(http.MethodPost, "/v1/guidance/knowledge-bindings/resolve", `{"scopes":[{"kind":"project","ref":"den-services"}],"inline_budget":0}`)
	if resolved.Code != http.StatusOK || !strings.Contains(resolved.Body.String(), `"selection_source":"knowledge_binding"`) || strings.Contains(resolved.Body.String(), "body_markdown") {
		t.Fatalf("resolve status/body = %d/%s", resolved.Code, resolved.Body.String())
	}
	deleted := request(http.MethodDelete, fmt.Sprintf("/v1/guidance/knowledge-bindings/%d", binding.ID), "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status/body = %d/%s", deleted.Code, deleted.Body.String())
	}
}

func TestResolveContextIncludesProjectAndRejectsConflictingProject(t *testing.T) {
	service := NewServiceWithKnowledge(newMemoryStore(), &memoryBindingStore{}, fakeProjects{}, &fakeDocuments{}, &fakeKnowledge{entries: map[string]*KnowledgeMetadata{"project-target": {Slug: "project-target", Status: "reviewed"}}}, fixedClock, 4096)
	if _, err := service.CreateKnowledgeBinding(context.Background(), CreateKnowledgeBindingRequest{TargetRef: "project-target", ScopeKind: ScopeProject, ScopeRef: "den-services"}); err != nil {
		t.Fatal(err)
	}
	resolution, err := service.ResolveContext(context.Background(), "den-services", BindingResolveQuery{})
	if err != nil || len(resolution.Selections) != 1 {
		t.Fatalf("implicit project resolution = %#v, %v", resolution, err)
	}
	if _, err := service.ResolveContext(context.Background(), "den-services", BindingResolveQuery{Scopes: []BindingScope{{Kind: ScopeProject, Ref: "other-project"}}}); err == nil {
		t.Fatal("cross-project context scope accepted")
	}
}

func TestLegacyServiceBindingRoutesFailClosedWithoutPanic(t *testing.T) {
	service := NewService(newMemoryStore(), fakeProjects{}, &fakeDocuments{}, fixedClock, 4096)
	if _, err := service.ListKnowledgeBindings(context.Background(), BindingListQuery{}); err == nil {
		t.Fatal("legacy service list unexpectedly configured")
	}
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/guidance/knowledge-bindings", strings.NewReader(`{"target_ref":"x","scope_kind":"project","scope_ref":"den-services"}`)))
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "knowledge_bindings_unconfigured") {
		t.Fatalf("legacy binding route = %d %s", response.Code, response.Body.String())
	}
}
