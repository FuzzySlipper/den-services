package documents

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	DocTypePRD        = "prd"
	DocTypeSpec       = "spec"
	DocTypeADR        = "adr"
	DocTypeConvention = "convention"
	DocTypeReference  = "reference"
	DocTypeNote       = "note"
	DocTypeMemory     = "memory"

	VisibilityNormal   = "normal"
	VisibilityHidden   = "hidden"
	VisibilityArchived = "archived"

	TargetTypeDocument = "document"

	DefaultDiscussionThreadLimit = 50

	ThreadStatusOpen     = "open"
	ThreadStatusResolved = "resolved"
	ThreadStatusArchived = "archived"

	CommentKindComment    = "comment"
	CommentKindQuestion   = "question"
	CommentKindAnswer     = "answer"
	CommentKindResolution = "resolution"
	CommentKindVersion    = "version_note"

	CommentStatusActive   = "active"
	CommentStatusResolved = "resolved"
	CommentStatusHidden   = "hidden"
	CommentStatusDeleted  = "deleted"

	DefaultThreadKey = "default"
)

var (
	ErrDocumentNotFound       = errors.New("document not found")                              //nolint:gochecknoglobals
	ErrThreadNotFound         = errors.New("discussion thread not found")                     //nolint:gochecknoglobals
	ErrCommentNotFound        = errors.New("discussion comment not found")                    //nolint:gochecknoglobals
	ErrMissingProjectID       = errors.New("project_id is required")                          //nolint:gochecknoglobals
	ErrMissingSlug            = errors.New("slug is required")                                //nolint:gochecknoglobals
	ErrMissingTitle           = errors.New("title is required")                               //nolint:gochecknoglobals
	ErrMissingContent         = errors.New("content is required")                             //nolint:gochecknoglobals
	ErrMissingAuthor          = errors.New("author_identity is required")                     //nolint:gochecknoglobals
	ErrMissingBody            = errors.New("body_markdown is required")                       //nolint:gochecknoglobals
	ErrInvalidDocType         = errors.New("invalid document type")                           //nolint:gochecknoglobals
	ErrInvalidVisibility      = errors.New("invalid document visibility")                     //nolint:gochecknoglobals
	ErrInvalidTargetType      = errors.New("target_type must be document")                    //nolint:gochecknoglobals
	ErrInvalidThreadStatus    = errors.New("invalid discussion thread status")                //nolint:gochecknoglobals
	ErrInvalidCommentKind     = errors.New("invalid discussion comment kind")                 //nolint:gochecknoglobals
	ErrInvalidCommentStatus   = errors.New("invalid discussion comment status")               //nolint:gochecknoglobals
	ErrParentThreadMismatch   = errors.New("parent comment must belong to the same thread")   //nolint:gochecknoglobals
	ErrParentDocumentMismatch = errors.New("parent comment must belong to the same document") //nolint:gochecknoglobals
	ErrInvalidJSON            = errors.New("json payload is invalid")                         //nolint:gochecknoglobals
	ErrSearchQueryEmpty       = errors.New("search query is required")                        //nolint:gochecknoglobals
	ErrProjectClientUnset     = errors.New("projects scope client is not configured")         //nolint:gochecknoglobals
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

func badRequest(err error) error {
	return NewServiceError(err, "bad_request", http.StatusBadRequest)
}

func validationFailed(err error) error {
	return NewServiceError(err, "validation_failed", http.StatusBadRequest)
}

func documentNotFound(projectID string, slug string) error {
	return NewServiceError(fmt.Errorf("%w: %s/%s", ErrDocumentNotFound, projectID, slug), "document_not_found", http.StatusNotFound)
}

func threadNotFound(id int64) error {
	return NewServiceError(fmt.Errorf("%w: %d", ErrThreadNotFound, id), "discussion_thread_not_found", http.StatusNotFound)
}

func commentNotFound(id int64) error {
	return NewServiceError(fmt.Errorf("%w: %d", ErrCommentNotFound, id), "discussion_comment_not_found", http.StatusNotFound)
}

func conflict(err error, code string) error {
	return NewServiceError(err, code, http.StatusConflict)
}

type Document struct {
	id         int64
	projectID  string
	slug       string
	title      string
	content    string
	docType    string
	visibility string
	tags       []string
	summary    string
	createdAt  time.Time
	updatedAt  time.Time
}

type NewDocumentParams struct {
	ID         int64
	ProjectID  string
	Slug       string
	Title      string
	Content    string
	DocType    string
	Visibility string
	Tags       []string
	Summary    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func NewDocument(params NewDocumentParams) (*Document, error) {
	projectID := strings.TrimSpace(params.ProjectID)
	if projectID == "" {
		return nil, ErrMissingProjectID
	}
	slug := strings.TrimSpace(params.Slug)
	if slug == "" {
		return nil, ErrMissingSlug
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		return nil, ErrMissingTitle
	}
	content := strings.TrimSpace(params.Content)
	if content == "" {
		return nil, ErrMissingContent
	}
	docType := strings.TrimSpace(params.DocType)
	if docType == "" {
		docType = DocTypeSpec
	}
	if !validDocType(docType) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDocType, docType)
	}
	visibility := strings.TrimSpace(params.Visibility)
	if visibility == "" {
		visibility = VisibilityNormal
	}
	if !validVisibility(visibility) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidVisibility, visibility)
	}
	createdAt := params.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := params.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	return &Document{
		id:         params.ID,
		projectID:  projectID,
		slug:       slug,
		title:      title,
		content:    content,
		docType:    docType,
		visibility: visibility,
		tags:       normalizeTags(params.Tags),
		summary:    strings.TrimSpace(params.Summary),
		createdAt:  createdAt,
		updatedAt:  updatedAt,
	}, nil
}

func (d *Document) ID() int64          { return d.id }
func (d *Document) ProjectID() string  { return d.projectID }
func (d *Document) Slug() string       { return d.slug }
func (d *Document) Title() string      { return d.title }
func (d *Document) Content() string    { return d.content }
func (d *Document) DocType() string    { return d.docType }
func (d *Document) Visibility() string { return d.visibility }
func (d *Document) Tags() []string     { return append([]string(nil), d.tags...) }
func (d *Document) Summary() string    { return d.summary }
func (d *Document) CreatedAt() time.Time {
	return d.createdAt
}
func (d *Document) UpdatedAt() time.Time {
	return d.updatedAt
}

type DocumentSummary struct {
	ID         int64
	ProjectID  string
	Slug       string
	Title      string
	DocType    string
	Visibility string
	Tags       []string
	Summary    string
	UpdatedAt  time.Time
}

type DocumentSearchResult struct {
	ProjectID  string
	Slug       string
	Title      string
	DocType    string
	Visibility string
	Summary    string
	Snippet    string
	Rank       float64
}

type DocumentReference struct {
	RefKind        string
	Description    string
	ScopeProjectID string
}

type ArchivePreflightResult struct {
	ProjectID                   string
	Slug                        string
	CanArchive                  bool
	ReferencedBy                []DocumentReference
	GuidanceReferenceCheckReady bool
}

type DiscussionThread struct {
	ID                int64
	TargetType        string
	TargetProjectID   string
	TargetID          *int64
	TargetSlug        string
	TargetAnchor      string
	ThreadKey         string
	Title             string
	Status            string
	CreatedBy         string
	Summary           string
	ResolutionSummary string
	MetadataJSON      []byte
	LastCommentAt     *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type DiscussionComment struct {
	ID              int64
	ThreadID        int64
	ParentCommentID *int64
	AuthorIdentity  string
	BodyMarkdown    string
	CommentKind     string
	Status          string
	MentionsJSON    []byte
	SourceRefsJSON  []byte
	MetadataJSON    []byte
	CreatedAt       time.Time
	EditedAt        *time.Time
	UpdatedAt       time.Time
}

type DiscussionDetail struct {
	DocumentID    int64
	ProjectID     string
	Slug          string
	Threads       []DiscussionThread
	DefaultThread *DiscussionThread
	Comments      []DiscussionComment
}

type ListDocumentsQuery struct {
	ProjectID     string
	DocType       string
	Tags          []string
	Visibility    string
	HasVisibility bool
}

type ListThreadsQuery struct {
	TargetType      string
	TargetProjectID string
	TargetSlug      string
	Status          string
	Limit           int
}

func normalizeDiscussionThreadLimit(limit int) int {
	if limit <= 0 {
		return DefaultDiscussionThreadLimit
	}
	return limit
}

func validDocType(value string) bool {
	switch value {
	case DocTypePRD, DocTypeSpec, DocTypeADR, DocTypeConvention, DocTypeReference, DocTypeNote, DocTypeMemory:
		return true
	default:
		return false
	}
}

func validVisibility(value string) bool {
	switch value {
	case VisibilityNormal, VisibilityHidden, VisibilityArchived:
		return true
	default:
		return false
	}
}

func validThreadStatus(value string) bool {
	switch value {
	case ThreadStatusOpen, ThreadStatusResolved, ThreadStatusArchived:
		return true
	default:
		return false
	}
}

func validCommentKind(value string) bool {
	switch value {
	case CommentKindComment, CommentKindQuestion, CommentKindAnswer, CommentKindResolution, CommentKindVersion:
		return true
	default:
		return false
	}
}

func validCommentStatus(value string) bool {
	switch value {
	case CommentStatusActive, CommentStatusResolved, CommentStatusHidden, CommentStatusDeleted:
		return true
	default:
		return false
	}
}

func threadKeyForAnchor(anchor string) string {
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return DefaultThreadKey
	}
	return "section:" + anchor
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		normalized = append(normalized, tag)
	}
	return normalized
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}
