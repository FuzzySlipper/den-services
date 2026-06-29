package knowledge

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	KindConcept          = "concept"
	KindReference        = "reference"
	KindGlossary         = "glossary"
	KindConvention       = "convention"
	KindServiceMap       = "service_map"
	KindToolNotes        = "tool_notes"
	KindGotcha           = "gotcha"
	KindArchitectureNote = "architecture_note"
	KindMigrationNote    = "migration_note"

	StatusDraft       = "draft"
	StatusReviewed    = "reviewed"
	StatusNeedsReview = "needs_review"
	StatusDeprecated  = "deprecated"
	StatusArchived    = "archived"

	CurationUnreviewedImport = "unreviewed_import"
	CurationHumanCurated     = "human_curated"
	CurationAgentCurated     = "agent_curated"
	CurationNeedsRecheck     = "needs_recheck"

	DefaultSearchLimit   = 10
	MaxSearchLimit       = 200
	DefaultContextBudget = 1600
	MaxTopGuideEntries   = 5
)

var (
	ErrEntryNotFound         = errors.New("knowledge entry not found")                 //nolint:gochecknoglobals
	ErrMissingSlug           = errors.New("slug is required")                          //nolint:gochecknoglobals
	ErrMissingTitle          = errors.New("title is required")                         //nolint:gochecknoglobals
	ErrMissingBody           = errors.New("body_markdown is required")                 //nolint:gochecknoglobals
	ErrSearchQueryEmpty      = errors.New("query is required")                         //nolint:gochecknoglobals
	ErrQuestionEmpty         = errors.New("question is required")                      //nolint:gochecknoglobals
	ErrNoSearchableTerms     = errors.New("no searchable terms in question")           //nolint:gochecknoglobals
	ErrInvalidKind           = errors.New("invalid knowledge kind")                    //nolint:gochecknoglobals
	ErrInvalidStatus         = errors.New("invalid knowledge status")                  //nolint:gochecknoglobals
	ErrInvalidCurationState  = errors.New("invalid knowledge curation state")          //nolint:gochecknoglobals
	ErrKnowledgeNotDocuments = errors.New("knowledge entries are not document search") //nolint:gochecknoglobals
)

type ServiceError struct {
	err    error
	code   string
	status int
}

func NewServiceError(err error, code string, status int) *ServiceError {
	return &ServiceError{err: err, code: code, status: status}
}

func (e *ServiceError) Error() string { return e.err.Error() }
func (e *ServiceError) Unwrap() error { return e.err }
func (e *ServiceError) Code() string  { return e.code }
func (e *ServiceError) HTTPStatus() int {
	return e.status
}

func validationFailed(err error) error {
	return NewServiceError(err, "validation_failed", http.StatusBadRequest)
}

func badRequest(err error) error {
	return NewServiceError(err, "bad_request", http.StatusBadRequest)
}

func entryNotFound(slug string) error {
	return NewServiceError(fmt.Errorf("%w: %s", ErrEntryNotFound, slug), "knowledge_entry_not_found", http.StatusNotFound)
}

type SourceRef struct {
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`
	ProjectID  string `json:"project_id,omitempty"`
	TaskID     *int64 `json:"task_id,omitempty"`
	MessageID  *int64 `json:"message_id,omitempty"`
	URL        string `json:"url,omitempty"`
	Note       string `json:"note,omitempty"`
}

type Entry struct {
	id              int64
	slug            string
	title           string
	summary         string
	bodyMarkdown    string
	kind            string
	status          string
	curationState   string
	tags            []string
	audience        []string
	aliases         []string
	sourceRefs      []SourceRef
	accuracyNotes   string
	replacementSlug string
	lastReviewedAt  *time.Time
	reviewDueAt     *time.Time
	createdBy       string
	updatedBy       string
	createdAt       time.Time
	updatedAt       time.Time
}

type NewEntryParams struct {
	ID              int64
	Slug            string
	Title           string
	Summary         string
	BodyMarkdown    string
	Kind            string
	Status          string
	CurationState   string
	Tags            []string
	Audience        []string
	Aliases         []string
	SourceRefs      []SourceRef
	AccuracyNotes   string
	ReplacementSlug string
	LastReviewedAt  *time.Time
	ReviewDueAt     *time.Time
	CreatedBy       string
	UpdatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func NewEntry(params NewEntryParams) (*Entry, error) {
	slug := strings.TrimSpace(params.Slug)
	if slug == "" {
		return nil, ErrMissingSlug
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		return nil, ErrMissingTitle
	}
	body := strings.TrimSpace(params.BodyMarkdown)
	if body == "" {
		return nil, ErrMissingBody
	}
	kind := strings.TrimSpace(params.Kind)
	if kind == "" {
		kind = KindReference
	}
	if !validKind(kind) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidKind, kind)
	}
	status := strings.TrimSpace(params.Status)
	if status == "" {
		status = StatusDraft
	}
	if !validStatus(status) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}
	curationState := strings.TrimSpace(params.CurationState)
	if curationState == "" {
		curationState = CurationUnreviewedImport
	}
	if !validCurationState(curationState) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidCurationState, curationState)
	}
	createdAt := params.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := params.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	return &Entry{
		id:              params.ID,
		slug:            slug,
		title:           title,
		summary:         strings.TrimSpace(params.Summary),
		bodyMarkdown:    body,
		kind:            kind,
		status:          status,
		curationState:   curationState,
		tags:            normalizeLabels(params.Tags),
		audience:        normalizeLabels(params.Audience),
		aliases:         normalizeLabels(params.Aliases),
		sourceRefs:      append([]SourceRef(nil), params.SourceRefs...),
		accuracyNotes:   strings.TrimSpace(params.AccuracyNotes),
		replacementSlug: strings.TrimSpace(params.ReplacementSlug),
		lastReviewedAt:  utcTimePtr(params.LastReviewedAt),
		reviewDueAt:     utcTimePtr(params.ReviewDueAt),
		createdBy:       strings.TrimSpace(params.CreatedBy),
		updatedBy:       strings.TrimSpace(params.UpdatedBy),
		createdAt:       createdAt,
		updatedAt:       updatedAt,
	}, nil
}

func (e *Entry) ID() int64                  { return e.id }
func (e *Entry) Slug() string               { return e.slug }
func (e *Entry) Title() string              { return e.title }
func (e *Entry) Summary() string            { return e.summary }
func (e *Entry) BodyMarkdown() string       { return e.bodyMarkdown }
func (e *Entry) Kind() string               { return e.kind }
func (e *Entry) Status() string             { return e.status }
func (e *Entry) CurationState() string      { return e.curationState }
func (e *Entry) Tags() []string             { return append([]string(nil), e.tags...) }
func (e *Entry) Audience() []string         { return append([]string(nil), e.audience...) }
func (e *Entry) Aliases() []string          { return append([]string(nil), e.aliases...) }
func (e *Entry) SourceRefs() []SourceRef    { return append([]SourceRef(nil), e.sourceRefs...) }
func (e *Entry) AccuracyNotes() string      { return e.accuracyNotes }
func (e *Entry) ReplacementSlug() string    { return e.replacementSlug }
func (e *Entry) LastReviewedAt() *time.Time { return utcTimePtr(e.lastReviewedAt) }
func (e *Entry) ReviewDueAt() *time.Time    { return utcTimePtr(e.reviewDueAt) }
func (e *Entry) CreatedBy() string          { return e.createdBy }
func (e *Entry) UpdatedBy() string          { return e.updatedBy }
func (e *Entry) CreatedAt() time.Time       { return e.createdAt }
func (e *Entry) UpdatedAt() time.Time       { return e.updatedAt }

type EntrySummary struct {
	ID              int64
	Slug            string
	Title           string
	Summary         string
	Kind            string
	Status          string
	CurationState   string
	Tags            []string
	Audience        []string
	Aliases         []string
	SourceRefs      []SourceRef
	AccuracyNotes   string
	ReplacementSlug string
	LastReviewedAt  *time.Time
	ReviewDueAt     *time.Time
	CreatedBy       string
	UpdatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SearchResult struct {
	Slug           string
	Title          string
	Summary        string
	Kind           string
	Status         string
	CurationState  string
	Tags           []string
	Audience       []string
	Aliases        []string
	SourceRefs     []SourceRef
	Snippet        string
	Rank           float64
	UpdatedAt      time.Time
	LastReviewedAt *time.Time
}

type RevisionSummary struct {
	ID             int64
	EntryID        int64
	RevisionNumber int
	Title          string
	Kind           string
	Status         string
	CurationState  string
	ChangeNote     string
	ChangedBy      string
	CreatedAt      time.Time
}

type ListQuery struct {
	Kind              string
	Status            string
	RequiredTags      []string
	AnyTags           []string
	Audience          []string
	IncludeDeprecated bool
	IncludeUnreviewed bool
	IncludeArchived   bool
	Limit             int
	Offset            int
}

type SearchQuery struct {
	Query             string
	RequiredTags      []string
	AnyTags           []string
	Kind              string
	Audience          []string
	Status            string
	IncludeDeprecated bool
	IncludeUnreviewed bool
	IncludeArchived   bool
	Limit             int
}

type GuideQuery struct {
	Question          string
	RequiredTags      []string
	AnyTags           []string
	Audience          []string
	ContextBudget     int
	IncludeFollowUps  bool
	IncludeDeprecated bool
	IncludeUnreviewed bool
}

type GuideResponse struct {
	Answer         string          `json:"answer"`
	Citations      []GuideCitation `json:"citations"`
	WhatToReadNext []NextRead      `json:"what_to_read_next"`
	Uncertainty    []string        `json:"uncertainty"`
	BudgetUsed     int             `json:"budget_used"`
}

type GuideCitation struct {
	Slug       string      `json:"slug"`
	Title      string      `json:"title"`
	Excerpt    string      `json:"excerpt"`
	SourceRefs []SourceRef `json:"source_refs,omitempty"`
}

type NextRead struct {
	Slug   string `json:"slug"`
	Reason string `json:"reason"`
}

var termCleanup = regexp.MustCompile(`[^a-z0-9_\s-]+`) //nolint:gochecknoglobals

func extractTerms(question string) []string {
	cleaned := strings.ToLower(termCleanup.ReplaceAllString(question, " "))
	parts := strings.Fields(cleaned)
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "-_")
		if len(part) < 3 || isStopword(part) {
			continue
		}
		if !slices.Contains(terms, part) {
			terms = append(terms, part)
		}
	}
	return terms
}

func isStopword(term string) bool {
	switch term {
	case "the", "and", "for", "with", "that", "this", "from", "what", "when", "where", "which", "about", "into", "does", "have", "need", "how":
		return true
	default:
		return false
	}
}

func normalizeLabels(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	labels := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		labels = append(labels, value)
	}
	slices.Sort(labels)
	return labels
}

func validKind(kind string) bool {
	return slices.Contains([]string{KindConcept, KindReference, KindGlossary, KindConvention, KindServiceMap, KindToolNotes, KindGotcha, KindArchitectureNote, KindMigrationNote}, kind)
}

func validStatus(status string) bool {
	return slices.Contains([]string{StatusDraft, StatusReviewed, StatusNeedsReview, StatusDeprecated, StatusArchived}, status)
}

func validCurationState(state string) bool {
	return slices.Contains([]string{CurationUnreviewedImport, CurationHumanCurated, CurationAgentCurated, CurationNeedsRecheck}, state)
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func clampLimit(limit int, fallback int) int {
	if limit <= 0 {
		return fallback
	}
	return min(limit, MaxSearchLimit)
}
