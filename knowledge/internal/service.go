package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type KnowledgeStore interface {
	Ping(ctx context.Context) error
	UpsertEntry(ctx context.Context, entry *Entry, changeNote string) (*Entry, error)
	GetEntry(ctx context.Context, slug string, includeArchived bool) (*Entry, error)
	ListEntries(ctx context.Context, query ListQuery) ([]EntrySummary, error)
	SearchEntries(ctx context.Context, query SearchQuery) ([]SearchResult, error)
	ListRevisions(ctx context.Context, slug string) ([]RevisionSummary, error)
}

type Service struct {
	store KnowledgeStore
	clock func() time.Time
}

func NewService(store KnowledgeStore, clock func() time.Time) *Service {
	return &Service{store: store, clock: clock}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) StoreEntry(ctx context.Context, req StoreEntryRequest) (*Entry, error) {
	now := s.clock().UTC()
	lastReviewedAt := req.LastReviewedAt
	if req.Status == StatusReviewed && lastReviewedAt == nil {
		lastReviewedAt = &now
	}
	entry, err := NewEntry(NewEntryParams{
		Slug:            req.Slug,
		Title:           req.Title,
		Summary:         req.Summary,
		BodyMarkdown:    req.BodyMarkdown,
		Kind:            req.Kind,
		Status:          req.Status,
		CurationState:   req.CurationState,
		Tags:            req.Tags,
		Audience:        req.Audience,
		Aliases:         req.Aliases,
		SourceRefs:      req.SourceRefs,
		AccuracyNotes:   req.AccuracyNotes,
		ReplacementSlug: req.ReplacementSlug,
		LastReviewedAt:  lastReviewedAt,
		ReviewDueAt:     req.ReviewDueAt,
		CreatedBy:       req.ChangedBy,
		UpdatedBy:       req.ChangedBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.UpsertEntry(ctx, entry, strings.TrimSpace(req.ChangeNote))
}

func (s *Service) GetEntry(ctx context.Context, slug string, includeArchived bool) (*Entry, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, validationFailed(ErrMissingSlug)
	}
	return s.store.GetEntry(ctx, slug, includeArchived)
}

func (s *Service) ListEntries(ctx context.Context, query ListQuery) ([]EntrySummary, error) {
	if query.Kind != "" && !validKind(query.Kind) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidKind, query.Kind))
	}
	if query.Status != "" {
		statuses := splitCSV(query.Status)
		for _, status := range statuses {
			if !validStatus(status) {
				return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidStatus, status))
			}
		}
		query.Status = strings.Join(statuses, ",")
	}
	query.RequiredTags = normalizeLabels(query.RequiredTags)
	query.AnyTags = normalizeLabels(query.AnyTags)
	query.Audience = normalizeLabels(query.Audience)
	query.Limit = clampLimit(query.Limit, 50)
	query.Offset = max(query.Offset, 0)
	return s.store.ListEntries(ctx, query)
}

func (s *Service) SearchEntries(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		return nil, validationFailed(ErrSearchQueryEmpty)
	}
	if query.Kind != "" && !validKind(query.Kind) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidKind, query.Kind))
	}
	if query.Status != "" {
		statuses := splitCSV(query.Status)
		for _, status := range statuses {
			if !validStatus(status) {
				return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidStatus, status))
			}
		}
		query.Status = strings.Join(statuses, ",")
	}
	query.RequiredTags = normalizeLabels(query.RequiredTags)
	query.AnyTags = normalizeLabels(query.AnyTags)
	query.Audience = normalizeLabels(query.Audience)
	query.Limit = clampLimit(query.Limit, DefaultSearchLimit)
	return s.store.SearchEntries(ctx, query)
}

func (s *Service) Guide(ctx context.Context, query GuideQuery) (GuideResponse, error) {
	query.Question = strings.TrimSpace(query.Question)
	if query.Question == "" {
		return GuideResponse{}, validationFailed(ErrQuestionEmpty)
	}
	terms := extractTerms(query.Question)
	if len(terms) == 0 {
		return GuideResponse{
			Answer:      "I could not extract searchable terms from your question.",
			Citations:   []GuideCitation{},
			Uncertainty: []string{ErrNoSearchableTerms.Error()},
		}, nil
	}
	results, err := s.SearchEntries(ctx, SearchQuery{
		Query:             strings.Join(terms, " OR "),
		RequiredTags:      query.RequiredTags,
		AnyTags:           query.AnyTags,
		Audience:          query.Audience,
		IncludeDeprecated: query.IncludeDeprecated,
		IncludeUnreviewed: query.IncludeUnreviewed,
		Limit:             10,
	})
	if err != nil {
		return GuideResponse{}, err
	}
	return buildExtractiveGuide(query, results), nil
}

func (s *Service) ListRevisions(ctx context.Context, slug string) ([]RevisionSummary, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, validationFailed(ErrMissingSlug)
	}
	return s.store.ListRevisions(ctx, slug)
}

func buildExtractiveGuide(query GuideQuery, candidates []SearchResult) GuideResponse {
	budget := query.ContextBudget
	if budget <= 0 {
		budget = DefaultContextBudget
	}
	top := candidates
	if len(top) > MaxTopGuideEntries {
		top = top[:MaxTopGuideEntries]
	}
	if len(top) == 0 {
		return GuideResponse{
			Answer:         "I could not find any relevant curated knowledge entries.",
			Citations:      []GuideCitation{},
			WhatToReadNext: []NextRead{},
			Uncertainty: []string{
				"No reviewed knowledge entries matched your question.",
				"Try include_unreviewed=true or broader search terms.",
			},
		}
	}
	citations := make([]GuideCitation, 0, len(top))
	used := 0
	for _, entry := range top {
		if used >= budget {
			break
		}
		excerpt := extractBestExcerpt(entry)
		if strings.TrimSpace(excerpt) == "" {
			continue
		}
		if used+len(excerpt) > budget {
			excerpt = excerpt[:max(0, budget-used)]
		}
		citations = append(citations, GuideCitation{
			Slug:       entry.Slug,
			Title:      entry.Title,
			Excerpt:    excerpt,
			SourceRefs: entry.SourceRefs,
		})
		used += len(excerpt)
	}
	if len(citations) == 0 {
		return GuideResponse{
			Answer:      "I found relevant entries but could not extract specific excerpts. Try a more specific question.",
			Uncertainty: []string{"Relevant entries did not produce usable excerpts."},
		}
	}
	answerParts := make([]string, 0, len(citations))
	cited := make(map[string]struct{}, len(citations))
	for _, citation := range citations {
		answerParts = append(answerParts, fmt.Sprintf("- **%s**: %s", citation.Title, citation.Excerpt))
		cited[citation.Slug] = struct{}{}
	}
	nextReads := []NextRead{}
	if query.IncludeFollowUps {
		for _, entry := range top {
			if _, ok := cited[entry.Slug]; ok {
				continue
			}
			reason := entry.Title
			if entry.Summary != "" {
				reason = entry.Summary
			}
			nextReads = append(nextReads, NextRead{Slug: entry.Slug, Reason: "Related: " + reason})
		}
	}
	return GuideResponse{
		Answer:         strings.Join(answerParts, "\n\n"),
		Citations:      citations,
		WhatToReadNext: nextReads,
		Uncertainty:    []string{},
		BudgetUsed:     used,
	}
}

func extractBestExcerpt(entry SearchResult) string {
	if len(entry.Snippet) > 20 && !strings.HasPrefix(entry.Snippet, "...<b>...") {
		return entry.Snippet
	}
	if len(entry.Summary) > 10 {
		return entry.Summary
	}
	return entry.Title
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
