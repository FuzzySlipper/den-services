package guidance

import (
	"context"
	"errors"
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

func TestServiceDeleteMissingEntryReturnsNotFound(t *testing.T) {
	service := NewService(newMemoryStore(), fakeProjects{}, &fakeDocuments{}, fixedClock, 4096)

	err := service.DeleteEntry(context.Background(), "den-services", 99)
	if !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("DeleteEntry(missing) error = %v, want %v", err, ErrEntryNotFound)
	}
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
