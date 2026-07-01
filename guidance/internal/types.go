package guidance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	GlobalProjectID = "_global"

	ImportanceRequired  = "required"
	ImportanceImportant = "important"

	VisibilityNormal   = "normal"
	VisibilityHidden   = "hidden"
	VisibilityArchived = "archived"
)

var (
	ErrMissingProjectID       = errors.New("project_id is required")                  //nolint:gochecknoglobals
	ErrMissingDocumentSlug    = errors.New("document_slug is required")               //nolint:gochecknoglobals
	ErrMissingDocumentProject = errors.New("document_project_id is required")         //nolint:gochecknoglobals
	ErrInvalidImportance      = errors.New("invalid importance")                      //nolint:gochecknoglobals
	ErrInvalidAudience        = errors.New("invalid audience")                        //nolint:gochecknoglobals
	ErrEntryNotFound          = errors.New("agent guidance entry not found")          //nolint:gochecknoglobals
	ErrDocumentUnavailable    = errors.New("guidance document is unavailable")        //nolint:gochecknoglobals
	ErrDocumentNotVisible     = errors.New("guidance document is not normal-visible") //nolint:gochecknoglobals
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

func entryNotFound(projectID string, entryID int64) error {
	return NewServiceError(fmt.Errorf("%w: %s/%d", ErrEntryNotFound, projectID, entryID), "agent_guidance_entry_not_found", http.StatusNotFound)
}

type Entry struct {
	ID                int64
	ProjectID         string
	DocumentProjectID string
	DocumentSlug      string
	Importance        string
	Audience          []string
	SortOrder         int
	Notes             string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EntryParams struct {
	ID                int64
	ProjectID         string
	DocumentProjectID string
	DocumentSlug      string
	Importance        string
	Audience          []string
	SortOrder         int
	Notes             string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func NewEntry(params EntryParams) (*Entry, error) {
	projectID := strings.TrimSpace(params.ProjectID)
	if projectID == "" {
		return nil, ErrMissingProjectID
	}
	documentProjectID := strings.TrimSpace(params.DocumentProjectID)
	if documentProjectID == "" {
		return nil, ErrMissingDocumentProject
	}
	documentSlug := strings.TrimSpace(params.DocumentSlug)
	if documentSlug == "" {
		return nil, ErrMissingDocumentSlug
	}
	importance := strings.TrimSpace(params.Importance)
	if importance == "" {
		importance = ImportanceImportant
	}
	if !validImportance(importance) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidImportance, importance)
	}
	audience, err := normalizeAudience(params.Audience)
	if err != nil {
		return nil, err
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
		ID:                params.ID,
		ProjectID:         projectID,
		DocumentProjectID: documentProjectID,
		DocumentSlug:      documentSlug,
		Importance:        importance,
		Audience:          audience,
		SortOrder:         params.SortOrder,
		Notes:             strings.TrimSpace(params.Notes),
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}, nil
}

type Document struct {
	ProjectID  string
	Slug       string
	Title      string
	Content    string
	DocType    string
	Visibility string
	Tags       []string
	Summary    string
	UpdatedAt  time.Time
}

type ResolveQuery struct {
	ProjectID      string
	IncludeContent bool
	MaxBytes       int
	Audience       []string
	IncludeHidden  bool
}

type GuidancePacket struct {
	ProjectID       string
	ResolvedAt      time.Time
	Sources         []GuidanceSource
	SkippedSources  []SkippedSource
	ContentMarkdown string
	ContentSHA256   string
	ContentBytes    int
	Truncated       bool
	Incomplete      bool
}

type GuidanceSource struct {
	EntryID           int64
	SourceScope       string
	DocumentProjectID string
	DocumentSlug      string
	DocumentTitle     string
	DocumentType      string
	DocumentUpdatedAt time.Time
	Visibility        string
	Tags              []string
	Importance        string
	Audience          []string
	SortOrder         int
	Notes             string
	ContentBytes      int
}

type SkippedSource struct {
	EntryID           int64
	SourceScope       string
	DocumentProjectID string
	DocumentSlug      string
	Importance        string
	Reason            string
	Required          bool
}

type DocumentReference struct {
	RefKind        string
	Description    string
	ScopeProjectID string
	EntryID        int64
	Importance     string
	Audience       []string
	SortOrder      int
	Notes          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func validImportance(value string) bool {
	switch value {
	case ImportanceRequired, ImportanceImportant:
		return true
	default:
		return false
	}
}

func normalizeAudience(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, ErrInvalidAudience
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result, nil
}

func packetDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
