package knowledge

import (
	"context"
	"strings"
	"sync"
)

type memoryStore struct {
	mu        sync.Mutex
	nextID    int64
	nextRevID int64
	entries   []*Entry
	revisions []RevisionSummary
}

func newMemoryStore() *memoryStore {
	return &memoryStore{nextID: 1, nextRevID: 1}
}

func (s *memoryStore) Ping(context.Context) error { return nil }

func (s *memoryStore) UpsertEntry(_ context.Context, entry *Entry, changeNote string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.entries {
		if existing.Slug() != entry.Slug() {
			continue
		}
		s.revisions = append(s.revisions, RevisionSummary{
			ID:             s.nextRevID,
			EntryID:        existing.ID(),
			RevisionNumber: len(s.revisions) + 1,
			Title:          existing.Title(),
			Kind:           existing.Kind(),
			Status:         existing.Status(),
			CurationState:  existing.CurationState(),
			ChangeNote:     changeNote,
			ChangedBy:      entry.UpdatedBy(),
			CreatedAt:      entry.UpdatedAt(),
		})
		s.nextRevID++
		updated, err := NewEntry(NewEntryParams{
			ID:              existing.ID(),
			Slug:            entry.Slug(),
			Title:           entry.Title(),
			Summary:         entry.Summary(),
			BodyMarkdown:    entry.BodyMarkdown(),
			Kind:            entry.Kind(),
			Status:          entry.Status(),
			CurationState:   entry.CurationState(),
			Tags:            entry.Tags(),
			Audience:        entry.Audience(),
			Aliases:         entry.Aliases(),
			SourceRefs:      entry.SourceRefs(),
			AccuracyNotes:   entry.AccuracyNotes(),
			ReplacementSlug: entry.ReplacementSlug(),
			LastReviewedAt:  entry.LastReviewedAt(),
			ReviewDueAt:     entry.ReviewDueAt(),
			CreatedBy:       existing.CreatedBy(),
			UpdatedBy:       entry.UpdatedBy(),
			CreatedAt:       existing.CreatedAt(),
			UpdatedAt:       entry.UpdatedAt(),
		})
		if err != nil {
			return nil, err
		}
		s.entries[i] = updated
		return updated, nil
	}
	created, err := NewEntry(NewEntryParams{
		ID:              s.nextID,
		Slug:            entry.Slug(),
		Title:           entry.Title(),
		Summary:         entry.Summary(),
		BodyMarkdown:    entry.BodyMarkdown(),
		Kind:            entry.Kind(),
		Status:          entry.Status(),
		CurationState:   entry.CurationState(),
		Tags:            entry.Tags(),
		Audience:        entry.Audience(),
		Aliases:         entry.Aliases(),
		SourceRefs:      entry.SourceRefs(),
		AccuracyNotes:   entry.AccuracyNotes(),
		ReplacementSlug: entry.ReplacementSlug(),
		LastReviewedAt:  entry.LastReviewedAt(),
		ReviewDueAt:     entry.ReviewDueAt(),
		CreatedBy:       entry.CreatedBy(),
		UpdatedBy:       entry.UpdatedBy(),
		CreatedAt:       entry.CreatedAt(),
		UpdatedAt:       entry.UpdatedAt(),
	})
	if err != nil {
		return nil, err
	}
	s.nextID++
	s.entries = append(s.entries, created)
	return created, nil
}

func (s *memoryStore) GetEntry(_ context.Context, slug string, includeArchived bool) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.entries {
		if entry.Slug() == slug && (includeArchived || entry.Status() != StatusArchived) {
			return entry, nil
		}
	}
	return nil, entryNotFound(slug)
}

func (s *memoryStore) ListEntries(_ context.Context, query ListQuery) ([]EntrySummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	summaries := []EntrySummary{}
	for _, entry := range s.entries {
		if !entryMatches(query.Status, query.IncludeDeprecated, query.IncludeUnreviewed, query.IncludeArchived, entry.Status()) {
			continue
		}
		if query.Kind != "" && entry.Kind() != query.Kind {
			continue
		}
		if !hasAll(entry.Tags(), query.RequiredTags) || !hasAny(entry.Tags(), query.AnyTags) || !hasAll(entry.Audience(), query.Audience) {
			continue
		}
		summaries = append(summaries, EntrySummary{
			ID:             entry.ID(),
			Slug:           entry.Slug(),
			Title:          entry.Title(),
			Summary:        entry.Summary(),
			Kind:           entry.Kind(),
			Status:         entry.Status(),
			CurationState:  entry.CurationState(),
			Tags:           entry.Tags(),
			Audience:       entry.Audience(),
			Aliases:        entry.Aliases(),
			SourceRefs:     entry.SourceRefs(),
			LastReviewedAt: entry.LastReviewedAt(),
			CreatedAt:      entry.CreatedAt(),
			UpdatedAt:      entry.UpdatedAt(),
		})
	}
	return summaries, nil
}

func (s *memoryStore) SearchEntries(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := []SearchResult{}
	terms := searchTerms(query.Query)
	for _, entry := range s.entries {
		if !entryMatches(query.Status, query.IncludeDeprecated, query.IncludeUnreviewed, query.IncludeArchived, entry.Status()) {
			continue
		}
		if query.Kind != "" && entry.Kind() != query.Kind {
			continue
		}
		if !hasAll(entry.Tags(), query.RequiredTags) || !hasAny(entry.Tags(), query.AnyTags) || !hasAll(entry.Audience(), query.Audience) {
			continue
		}
		haystack := strings.ToLower(entry.Title() + " " + entry.Summary() + " " + entry.BodyMarkdown() + " " + strings.Join(entry.Tags(), " "))
		matched := false
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		results = append(results, SearchResult{
			Slug:           entry.Slug(),
			Title:          entry.Title(),
			Summary:        entry.Summary(),
			Kind:           entry.Kind(),
			Status:         entry.Status(),
			CurationState:  entry.CurationState(),
			Tags:           entry.Tags(),
			Audience:       entry.Audience(),
			Aliases:        entry.Aliases(),
			SourceRefs:     entry.SourceRefs(),
			Snippet:        entry.Summary(),
			Rank:           1,
			UpdatedAt:      entry.UpdatedAt(),
			LastReviewedAt: entry.LastReviewedAt(),
		})
	}
	return results, nil
}

func (s *memoryStore) ListRevisions(_ context.Context, slug string) ([]RevisionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var entryID int64
	for _, entry := range s.entries {
		if entry.Slug() == slug {
			entryID = entry.ID()
			break
		}
	}
	revisions := []RevisionSummary{}
	for _, revision := range s.revisions {
		if revision.EntryID == entryID {
			revisions = append(revisions, revision)
		}
	}
	return revisions, nil
}

func entryMatches(explicit string, includeDeprecated bool, includeUnreviewed bool, includeArchived bool, status string) bool {
	if explicit != "" {
		if !includeArchived && status == StatusArchived {
			return false
		}
		for _, wanted := range splitCSV(explicit) {
			if status == wanted {
				return true
			}
		}
		return false
	}
	switch status {
	case StatusReviewed:
		return true
	case StatusDeprecated:
		return includeDeprecated
	case StatusDraft, StatusNeedsReview:
		return includeUnreviewed
	case StatusArchived:
		return includeArchived
	default:
		return false
	}
}

func hasAll(values []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	for _, value := range required {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}

func hasAny(values []string, optional []string) bool {
	if len(optional) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	for _, value := range optional {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

func searchTerms(query string) []string {
	parts := strings.Fields(strings.ToLower(strings.ReplaceAll(query, "OR", " ")))
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, `"'()`)
		if part != "" {
			terms = append(terms, part)
		}
	}
	return terms
}
