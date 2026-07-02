package librarian

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	SourceTasks     = "tasks"
	SourceMessages  = "messages"
	SourceDocuments = "documents"
	SourceKnowledge = "knowledge"

	ItemTypeTask      = "task"
	ItemTypeMessage   = "message"
	ItemTypeDocument  = "document"
	ItemTypeKnowledge = "knowledge"

	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"

	GlobalProjectID = "_global"
)

var (
	ErrMissingProjectID = errors.New("project_id is required") //nolint:gochecknoglobals
	ErrMissingQuery     = errors.New("query is required")      //nolint:gochecknoglobals
	ErrTaskNotFound     = errors.New("task not found")         //nolint:gochecknoglobals
	ErrSourceNotFound   = errors.New("source not found")       //nolint:gochecknoglobals
	ErrProjectMismatch  = errors.New("task belongs to a different project")
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

func notFound(err error) error {
	return NewServiceError(err, "not_found", http.StatusNotFound)
}

func sourceUnavailable(source string, err error) Warning {
	return Warning{Source: source, Message: err.Error()}
}

type QueryRequest struct {
	ProjectID     string       `json:"project_id,omitempty"`
	Query         string       `json:"query"`
	TaskID        *int64       `json:"task_id,omitempty"`
	IncludeGlobal *bool        `json:"include_global,omitempty"`
	SourceLimits  SourceLimits `json:"source_limits,omitempty"`
}

type SourceLimits struct {
	Tasks     int `json:"tasks,omitempty"`
	Messages  int `json:"messages,omitempty"`
	Documents int `json:"documents,omitempty"`
	Knowledge int `json:"knowledge,omitempty"`
}

func (l SourceLimits) withDefaults() SourceLimits {
	if l.Tasks <= 0 {
		l.Tasks = defaultTaskLimit
	}
	if l.Messages <= 0 {
		l.Messages = defaultMessageLimit
	}
	if l.Documents <= 0 {
		l.Documents = defaultDocumentLimit
	}
	if l.Knowledge <= 0 {
		l.Knowledge = defaultKnowledgeLimit
	}
	return l.clamped()
}

func (l SourceLimits) merged(defaults SourceLimits) SourceLimits {
	if l.Tasks <= 0 {
		l.Tasks = defaults.Tasks
	}
	if l.Messages <= 0 {
		l.Messages = defaults.Messages
	}
	if l.Documents <= 0 {
		l.Documents = defaults.Documents
	}
	if l.Knowledge <= 0 {
		l.Knowledge = defaults.Knowledge
	}
	return l.clamped()
}

func (l SourceLimits) clamped() SourceLimits {
	l.Tasks = clamp(l.Tasks, 1, 20)
	l.Messages = clamp(l.Messages, 1, 30)
	l.Documents = clamp(l.Documents, 1, 30)
	l.Knowledge = clamp(l.Knowledge, 1, 20)
	return l
}

type QueryResponse struct {
	Query           string         `json:"query"`
	ProjectID       string         `json:"project_id"`
	TaskID          *int64         `json:"task_id,omitempty"`
	RelevantItems   []RelevantItem `json:"relevant_items"`
	Recommendations []string       `json:"recommendations"`
	Confidence      string         `json:"confidence"`
	Warnings        []Warning      `json:"warnings,omitempty"`
	Budget          SourceLimits   `json:"budget"`
}

type RelevantItem struct {
	Type        string  `json:"type"`
	Source      string  `json:"source,omitempty"`
	SourceID    string  `json:"source_id"`
	ProjectID   string  `json:"project_id,omitempty"`
	Title       string  `json:"title,omitempty"`
	Summary     string  `json:"summary"`
	WhyRelevant string  `json:"why_relevant"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score,omitempty"`
}

type Warning struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

type TaskDetail struct {
	Task TaskSummary `json:"task"`
}

type TaskSummary struct {
	ID          int64    `json:"id"`
	ProjectID   string   `json:"project_id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	AssignedTo  string   `json:"assigned_to,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type MessageSummary struct {
	ID        int64     `json:"id"`
	ProjectID string    `json:"project_id"`
	TaskID    *int64    `json:"task_id,omitempty"`
	Sender    string    `json:"sender"`
	Content   string    `json:"content"`
	Intent    string    `json:"intent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type DocumentSearchResult struct {
	ProjectID string  `json:"project_id"`
	Slug      string  `json:"slug"`
	Title     string  `json:"title"`
	Summary   string  `json:"summary,omitempty"`
	Snippet   string  `json:"snippet"`
	Rank      float64 `json:"rank"`
}

type KnowledgeSearchResponse struct {
	Results []KnowledgeSearchResult `json:"results"`
	Count   int                     `json:"count"`
}

type KnowledgeSearchResult struct {
	Slug    string  `json:"slug"`
	Title   string  `json:"title"`
	Summary string  `json:"summary,omitempty"`
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"`
}

type Candidate struct {
	ItemType  string
	Source    string
	SourceID  string
	ProjectID string
	Title     string
	Summary   string
	Snippet   string
	Text      string
	Score     float64
}

func (c Candidate) toRelevantItem(queryTerms []string) RelevantItem {
	return RelevantItem{
		Type:        c.ItemType,
		Source:      c.Source,
		SourceID:    c.SourceID,
		ProjectID:   c.ProjectID,
		Title:       c.Title,
		Summary:     c.Summary,
		WhyRelevant: whyRelevant(c.Source, c.Text, queryTerms),
		Snippet:     c.Snippet,
		Score:       c.Score,
	}
}

func whyRelevant(source string, text string, queryTerms []string) string {
	matches := make([]string, 0, 3)
	lowerText := strings.ToLower(text)
	for _, term := range queryTerms {
		if len(matches) >= 3 {
			break
		}
		if strings.Contains(lowerText, term) {
			matches = append(matches, term)
		}
	}
	if len(matches) == 0 {
		return fmt.Sprintf("%s source returned this as nearby context.", source)
	}
	return fmt.Sprintf("Matches %s in %s context.", strings.Join(matches, ", "), source)
}

func clamp(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
