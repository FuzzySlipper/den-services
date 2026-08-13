package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	PostStatusActive  = "active"
	PostStatusDeleted = "deleted"

	CommentStatusActive  = "active"
	CommentStatusDeleted = "deleted"

	SearchResultPost    = "post"
	SearchResultComment = "comment"

	DefaultPageSize    = 50
	MaxPageSize        = 100
	MaxPathComments    = 50
	MaxActorIdentity   = 256
	MaxPurgeReasonSize = 2000
)

var (
	ErrPostNotFound          = errors.New("board post not found")                        //nolint:gochecknoglobals
	ErrCommentNotFound       = errors.New("board comment not found")                     //nolint:gochecknoglobals
	ErrMissingProjectID      = errors.New("project_id is required")                      //nolint:gochecknoglobals
	ErrMissingTitle          = errors.New("title is required")                           //nolint:gochecknoglobals
	ErrMissingBody           = errors.New("body_markdown is required")                   //nolint:gochecknoglobals
	ErrMissingAuthor         = errors.New("author_identity is required")                 //nolint:gochecknoglobals
	ErrMissingActor          = errors.New("actor_identity is required")                  //nolint:gochecknoglobals
	ErrMissingReason         = errors.New("reason is required")                          //nolint:gochecknoglobals
	ErrInvalidMetadata       = errors.New("metadata must be valid json")                 //nolint:gochecknoglobals
	ErrInvalidID             = errors.New("id must be positive")                         //nolint:gochecknoglobals
	ErrInvalidAfterID        = errors.New("after_id must not be negative")               //nolint:gochecknoglobals
	ErrInvalidLimit          = errors.New("limit must not be negative")                  //nolint:gochecknoglobals
	ErrInvalidPathLimit      = errors.New("path limit must be positive")                 //nolint:gochecknoglobals
	ErrParentPostMismatch    = errors.New("parent comment must belong to the same post") //nolint:gochecknoglobals
	ErrInvalidSearchQuery    = errors.New("q is required")                               //nolint:gochecknoglobals
	ErrProjectValidatorUnset = errors.New("project validator is not configured")         //nolint:gochecknoglobals
	ErrInvalidLimits         = errors.New("board limits are invalid")                    //nolint:gochecknoglobals
	ErrCommentCycle          = errors.New("comment parent cycle detected")               //nolint:gochecknoglobals
)

type ServiceError struct {
	err    error
	code   string
	status int
}

func NewServiceError(err error, code string, status int) *ServiceError {
	return &ServiceError{err: err, code: code, status: status}
}

func (e *ServiceError) Error() string {
	return e.err.Error()
}

func (e *ServiceError) Unwrap() error {
	return e.err
}

func (e *ServiceError) Code() string {
	return e.code
}

func (e *ServiceError) HTTPStatus() int {
	return e.status
}

func badRequest(err error) error {
	return NewServiceError(err, "bad_request", http.StatusBadRequest)
}

func validationFailed(err error) error {
	return NewServiceError(err, "validation_failed", http.StatusBadRequest)
}

func postNotFound() error {
	return NewServiceError(ErrPostNotFound, "board_post_not_found", http.StatusNotFound)
}

func commentNotFound() error {
	return NewServiceError(ErrCommentNotFound, "board_comment_not_found", http.StatusNotFound)
}

func conflict(err error, code string) error {
	return NewServiceError(err, code, http.StatusConflict)
}

type Limits struct {
	DefaultPageSize int
	MaxPageSize     int
	MaxPathComments int
}

func DefaultLimits() Limits {
	return Limits{
		DefaultPageSize: DefaultPageSize,
		MaxPageSize:     MaxPageSize,
		MaxPathComments: MaxPathComments,
	}
}

func (l Limits) Validate() error {
	if l.DefaultPageSize <= 0 || l.MaxPageSize < l.DefaultPageSize || l.MaxPathComments <= 0 {
		return ErrInvalidLimits
	}
	return nil
}

type Post struct {
	ID             int64
	ProjectID      string
	Title          string
	BodyMarkdown   string
	AuthorIdentity string
	MetadataJSON   []byte
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

type PostSummary struct {
	ID             int64
	ProjectID      string
	Title          string
	AuthorIdentity string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Comment struct {
	ID              int64
	PostID          int64
	ParentCommentID *int64
	AuthorIdentity  string
	BodyMarkdown    string
	MetadataJSON    []byte
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

type SearchResult struct {
	Kind           string
	ID             int64
	PostID         int64
	ProjectID      string
	Title          string
	AuthorIdentity string
	Snippet        string
	Rank           float64
	CreatedAt      time.Time
}

type PostPage struct {
	Posts       []PostSummary
	NextAfterID *int64
}

type CommentPage struct {
	Comments    []Comment
	NextAfterID *int64
}

type SearchPage struct {
	Results     []SearchResult
	NextAfterID *int64
}

type CommentPath struct {
	Post      *Post
	Comments  []Comment
	Truncated bool
}

type ListPostsQuery struct {
	ProjectID string
	AfterID   int64
	Limit     int
}

type SearchQuery struct {
	ProjectID string
	Query     string
	AfterID   int64
	Limit     int
}

type ListCommentsQuery struct {
	PostID          int64
	ParentCommentID *int64
	AfterID         int64
	Limit           int
}

type CreatePostRequest struct {
	Title          string          `json:"title"`
	BodyMarkdown   string          `json:"body_markdown"`
	AuthorIdentity string          `json:"author_identity"`
	Metadata       json.RawMessage `json:"metadata"`
}

type CreateCommentRequest struct {
	ParentCommentID *int64          `json:"parent_comment_id"`
	BodyMarkdown    string          `json:"body_markdown"`
	AuthorIdentity  string          `json:"author_identity"`
	Metadata        json.RawMessage `json:"metadata"`
}

// PurgeRequest is intentionally typed even though its caller supplied values
// are not retained. Actor and reason are validated at the destructive boundary
// so callers cannot accidentally perform an anonymous or context-free purge.
type PurgeRequest struct {
	ActorIdentity string `json:"actor_identity"`
	Reason        string `json:"reason"`
}

func (r PurgeRequest) Validate() error {
	actor := strings.TrimSpace(r.ActorIdentity)
	if actor == "" {
		return ErrMissingActor
	}
	if len(actor) > MaxActorIdentity {
		return fmt.Errorf("actor_identity exceeds %d characters", MaxActorIdentity)
	}
	reason := strings.TrimSpace(r.Reason)
	if reason == "" {
		return ErrMissingReason
	}
	if len(reason) > MaxPurgeReasonSize {
		return fmt.Errorf("reason exceeds %d characters", MaxPurgeReasonSize)
	}
	return nil
}

func NewPost(params Post) (*Post, error) {
	projectID := strings.TrimSpace(params.ProjectID)
	if projectID == "" {
		return nil, ErrMissingProjectID
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		return nil, ErrMissingTitle
	}
	body := strings.TrimSpace(params.BodyMarkdown)
	if body == "" {
		return nil, ErrMissingBody
	}
	author := strings.TrimSpace(params.AuthorIdentity)
	if author == "" {
		return nil, ErrMissingAuthor
	}
	metadata, err := normalizeMetadata(params.MetadataJSON)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	createdAt := params.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := params.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	status := strings.TrimSpace(params.Status)
	if status == "" {
		status = PostStatusActive
	}
	if status != PostStatusActive {
		return nil, fmt.Errorf("invalid new post status %q", status)
	}
	return &Post{
		ID:             params.ID,
		ProjectID:      projectID,
		Title:          title,
		BodyMarkdown:   body,
		AuthorIdentity: author,
		MetadataJSON:   metadata,
		Status:         status,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}

func NewComment(params Comment) (*Comment, error) {
	if params.PostID <= 0 {
		return nil, ErrInvalidID
	}
	body := strings.TrimSpace(params.BodyMarkdown)
	if body == "" {
		return nil, ErrMissingBody
	}
	author := strings.TrimSpace(params.AuthorIdentity)
	if author == "" {
		return nil, ErrMissingAuthor
	}
	metadata, err := normalizeMetadata(params.MetadataJSON)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	createdAt := params.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := params.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	status := strings.TrimSpace(params.Status)
	if status == "" {
		status = CommentStatusActive
	}
	if status != CommentStatusActive {
		return nil, fmt.Errorf("invalid new comment status %q", status)
	}
	return &Comment{
		ID:              params.ID,
		PostID:          params.PostID,
		ParentCommentID: cloneInt64Pointer(params.ParentCommentID),
		AuthorIdentity:  author,
		BodyMarkdown:    body,
		MetadataJSON:    metadata,
		Status:          status,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

func normalizeMetadata(raw []byte) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if !json.Valid(raw) {
		return nil, ErrInvalidMetadata
	}
	return cloneBytes(raw), nil
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func validateID(id int64) error {
	if id <= 0 {
		return ErrInvalidID
	}
	return nil
}

func validateAfterID(afterID int64) error {
	if afterID < 0 {
		return ErrInvalidAfterID
	}
	return nil
}
