package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type memoryStore struct {
	mu        sync.Mutex
	nextID    int64
	nextRevID int64
	entries   []*Entry
	revisions []RevisionSummary
	links     []EntryLink
	maps      []KnowledgeMap
}

func newMemoryStore() *memoryStore {
	return &memoryStore{nextID: 1, nextRevID: 1}
}

func (s *memoryStore) Ping(context.Context) error { return nil }

func (s *memoryStore) DeleteEntry(_ context.Context, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, entry := range s.entries {
		if entry.Slug() != slug {
			continue
		}
		s.entries = append(s.entries[:index], s.entries[index+1:]...)
		kept := s.revisions[:0]
		for _, revision := range s.revisions {
			if revision.EntryID != entry.ID() {
				kept = append(kept, revision)
			}
		}
		s.revisions = kept
		keptLinks := s.links[:0]
		for _, link := range s.links {
			if link.FromSlug != slug && link.ToSlug != slug {
				keptLinks = append(keptLinks, link)
			}
		}
		s.links = keptLinks
		return nil
	}
	return entryNotFound(slug)
}

func (s *memoryStore) UpsertEntry(_ context.Context, entry *Entry, changeNote string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.entries {
		if existing.Slug() != entry.Slug() {
			continue
		}
		revisionNumber := 1
		for _, revision := range s.revisions {
			if revision.EntryID == existing.ID() {
				revisionNumber++
			}
		}
		s.revisions = append(s.revisions, RevisionSummary{
			ID:             s.nextRevID,
			EntryID:        existing.ID(),
			RevisionNumber: revisionNumber,
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

func (s *memoryStore) CurrentRevision(_ context.Context, slug string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var entryID int64
	for _, entry := range s.entries {
		if entry.Slug() == slug {
			entryID = entry.ID()
			break
		}
	}
	if entryID == 0 {
		return 0, entryNotFound(slug)
	}
	count := 0
	for _, revision := range s.revisions {
		if revision.EntryID == entryID {
			count++
		}
	}
	return count + 1, nil
}

func (s *memoryStore) ReplaceLinks(_ context.Context, fromSlug string, links []EntryLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.links[:0]
	for _, link := range s.links {
		if link.FromSlug != fromSlug {
			kept = append(kept, link)
		}
	}
	s.links = append(kept, links...)
	return nil
}

func (s *memoryStore) ListLinks(_ context.Context, slug string, limit int) ([]ResolvedLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	links := make([]ResolvedLink, 0, limit)
	for _, link := range s.links {
		if link.FromSlug != slug || len(links) >= limit {
			continue
		}
		resolved := ResolvedLink{Kind: link.Kind, Description: link.Description, Target: LinkTarget{Slug: link.ToSlug, Missing: true}}
		for _, entry := range s.entries {
			if entry.Slug() == link.ToSlug {
				resolved.Target = LinkTarget{Slug: entry.Slug(), Title: entry.Title(), Summary: entry.Summary(), Status: entry.Status()}
				break
			}
		}
		links = append(links, resolved)
	}
	return links, nil
}

func (s *memoryStore) StoreMap(_ context.Context, knowledgeMap KnowledgeMap) (*KnowledgeMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.maps {
		if existing.Slug == knowledgeMap.Slug {
			knowledgeMap.CreatedAt = existing.CreatedAt
			knowledgeMap.CreatedBy = existing.CreatedBy
			s.maps[i] = knowledgeMap
			return &s.maps[i], nil
		}
	}
	s.maps = append(s.maps, knowledgeMap)
	return &s.maps[len(s.maps)-1], nil
}

func (s *memoryStore) GetMap(_ context.Context, slug string) (*KnowledgeMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, knowledgeMap := range s.maps {
		if knowledgeMap.Slug == slug {
			copy := knowledgeMap
			copy.Entries = append([]KnowledgeMapEntry(nil), knowledgeMap.Entries...)
			return &copy, nil
		}
	}
	return nil, NewServiceError(fmt.Errorf("%w: %s", ErrMapNotFound, slug), "knowledge_map_not_found", http.StatusNotFound)
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
