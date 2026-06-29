package knowledge

import "time"

type StoreEntryRequest struct {
	Slug            string      `json:"slug"`
	Title           string      `json:"title"`
	BodyMarkdown    string      `json:"body_markdown"`
	Kind            string      `json:"kind,omitempty"`
	Status          string      `json:"status,omitempty"`
	CurationState   string      `json:"curation_state,omitempty"`
	Summary         string      `json:"summary,omitempty"`
	Tags            []string    `json:"tags,omitempty"`
	Audience        []string    `json:"audience,omitempty"`
	Aliases         []string    `json:"aliases,omitempty"`
	SourceRefs      []SourceRef `json:"source_refs,omitempty"`
	AccuracyNotes   string      `json:"accuracy_notes,omitempty"`
	ReplacementSlug string      `json:"replacement_slug,omitempty"`
	LastReviewedAt  *time.Time  `json:"last_reviewed_at,omitempty"`
	ReviewDueAt     *time.Time  `json:"review_due_at,omitempty"`
	ChangedBy       string      `json:"changed_by,omitempty"`
	ChangeNote      string      `json:"change_note,omitempty"`
}

type SearchRequest struct {
	Query             string   `json:"query"`
	RequiredTags      []string `json:"required_tags,omitempty"`
	AnyTags           []string `json:"any_tags,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	Audience          []string `json:"audience,omitempty"`
	Status            string   `json:"status,omitempty"`
	IncludeDeprecated bool     `json:"include_deprecated,omitempty"`
	IncludeUnreviewed bool     `json:"include_unreviewed,omitempty"`
	IncludeArchived   bool     `json:"include_archived,omitempty"`
	Limit             int      `json:"limit,omitempty"`
}

type GuideRequest struct {
	Question          string   `json:"question"`
	RequiredTags      []string `json:"required_tags,omitempty"`
	AnyTags           []string `json:"any_tags,omitempty"`
	Audience          []string `json:"audience,omitempty"`
	ContextBudget     int      `json:"context_budget,omitempty"`
	IncludeFollowUps  *bool    `json:"include_follow_ups,omitempty"`
	IncludeDeprecated bool     `json:"include_deprecated,omitempty"`
	IncludeUnreviewed bool     `json:"include_unreviewed,omitempty"`
}

type EntryResponse struct {
	ID              int64       `json:"id"`
	Slug            string      `json:"slug"`
	Title           string      `json:"title"`
	Summary         string      `json:"summary,omitempty"`
	BodyMarkdown    string      `json:"body_markdown"`
	Kind            string      `json:"kind"`
	Status          string      `json:"status"`
	CurationState   string      `json:"curation_state"`
	Tags            []string    `json:"tags,omitempty"`
	Audience        []string    `json:"audience,omitempty"`
	Aliases         []string    `json:"aliases,omitempty"`
	SourceRefs      []SourceRef `json:"source_refs,omitempty"`
	AccuracyNotes   string      `json:"accuracy_notes,omitempty"`
	ReplacementSlug string      `json:"replacement_slug,omitempty"`
	LastReviewedAt  *time.Time  `json:"last_reviewed_at,omitempty"`
	ReviewDueAt     *time.Time  `json:"review_due_at,omitempty"`
	CreatedBy       string      `json:"created_by,omitempty"`
	UpdatedBy       string      `json:"updated_by,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type EntrySummaryResponse struct {
	ID              int64       `json:"id"`
	Slug            string      `json:"slug"`
	Title           string      `json:"title"`
	Summary         string      `json:"summary,omitempty"`
	Kind            string      `json:"kind"`
	Status          string      `json:"status"`
	CurationState   string      `json:"curation_state"`
	Tags            []string    `json:"tags,omitempty"`
	Audience        []string    `json:"audience,omitempty"`
	Aliases         []string    `json:"aliases,omitempty"`
	SourceRefs      []SourceRef `json:"source_refs,omitempty"`
	AccuracyNotes   string      `json:"accuracy_notes,omitempty"`
	ReplacementSlug string      `json:"replacement_slug,omitempty"`
	LastReviewedAt  *time.Time  `json:"last_reviewed_at,omitempty"`
	ReviewDueAt     *time.Time  `json:"review_due_at,omitempty"`
	CreatedBy       string      `json:"created_by,omitempty"`
	UpdatedBy       string      `json:"updated_by,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type SearchResponse struct {
	Slug           string      `json:"slug"`
	Title          string      `json:"title"`
	Summary        string      `json:"summary,omitempty"`
	Kind           string      `json:"kind"`
	Status         string      `json:"status"`
	CurationState  string      `json:"curation_state"`
	Tags           []string    `json:"tags,omitempty"`
	Audience       []string    `json:"audience,omitempty"`
	Aliases        []string    `json:"aliases,omitempty"`
	SourceRefs     []SourceRef `json:"source_refs,omitempty"`
	Snippet        string      `json:"snippet"`
	Rank           float64     `json:"rank"`
	UpdatedAt      time.Time   `json:"updated_at"`
	LastReviewedAt *time.Time  `json:"last_reviewed_at,omitempty"`
}

type RevisionResponse struct {
	ID             int64     `json:"id"`
	EntryID        int64     `json:"entry_id"`
	RevisionNumber int       `json:"revision_number"`
	Title          string    `json:"title"`
	Kind           string    `json:"kind"`
	Status         string    `json:"status"`
	CurationState  string    `json:"curation_state"`
	ChangeNote     string    `json:"change_note,omitempty"`
	ChangedBy      string    `json:"changed_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type ListResponse struct {
	Items []EntrySummaryResponse `json:"items"`
	Count int                    `json:"count"`
}

type SearchResultsResponse struct {
	Results []SearchResponse `json:"results"`
	Count   int              `json:"count"`
}

type RevisionsResponse struct {
	Revisions []RevisionResponse `json:"revisions"`
	Count     int                `json:"count"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func toEntryResponse(entry *Entry) EntryResponse {
	return EntryResponse{
		ID:              entry.ID(),
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
	}
}

func toSummaryResponse(summary EntrySummary) EntrySummaryResponse {
	return EntrySummaryResponse(summary)
}

func toSummaryResponses(summaries []EntrySummary) []EntrySummaryResponse {
	responses := make([]EntrySummaryResponse, 0, len(summaries))
	for _, summary := range summaries {
		responses = append(responses, toSummaryResponse(summary))
	}
	return responses
}

func toSearchResponse(result SearchResult) SearchResponse {
	return SearchResponse(result)
}

func toSearchResponses(results []SearchResult) []SearchResponse {
	responses := make([]SearchResponse, 0, len(results))
	for _, result := range results {
		responses = append(responses, toSearchResponse(result))
	}
	return responses
}

func toRevisionResponses(revisions []RevisionSummary) []RevisionResponse {
	responses := make([]RevisionResponse, 0, len(revisions))
	for _, revision := range revisions {
		responses = append(responses, RevisionResponse(revision))
	}
	return responses
}
