package board

import (
	"context"
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

	DefaultMaxProjectIDBytes      = 256
	DefaultMaxTitleBytes          = 512
	DefaultMaxBodyBytes           = 64 * 1024
	DefaultMaxAuthorIdentityBytes = MaxActorIdentity
	DefaultMaxMetadataBytes       = 16 * 1024
	DefaultMaxSearchQueryBytes    = 256
	DefaultMaxPurgeReasonBytes    = MaxPurgeReasonSize
	DefaultMaxRequestBodyBytes    = 256 * 1024
	DefaultMaxIdempotencyKeyBytes = 256
	DefaultAdapterIdentity        = "authenticated-board-adapter"
)

var (
	ErrPostNotFound           = errors.New("board post not found")                        //nolint:gochecknoglobals
	ErrCommentNotFound        = errors.New("board comment not found")                     //nolint:gochecknoglobals
	ErrMissingProjectID       = errors.New("project_id is required")                      //nolint:gochecknoglobals
	ErrMissingTitle           = errors.New("title is required")                           //nolint:gochecknoglobals
	ErrMissingBody            = errors.New("body_markdown is required")                   //nolint:gochecknoglobals
	ErrMissingAuthor          = errors.New("author_identity is required")                 //nolint:gochecknoglobals
	ErrMissingReason          = errors.New("reason is required")                          //nolint:gochecknoglobals
	ErrInvalidMetadata        = errors.New("metadata must be valid json")                 //nolint:gochecknoglobals
	ErrInvalidID              = errors.New("id must be positive")                         //nolint:gochecknoglobals
	ErrInvalidAfterID         = errors.New("after_id must not be negative")               //nolint:gochecknoglobals
	ErrInvalidLimit           = errors.New("limit must not be negative")                  //nolint:gochecknoglobals
	ErrInvalidPathLimit       = errors.New("path limit must be positive")                 //nolint:gochecknoglobals
	ErrParentPostMismatch     = errors.New("parent comment must belong to the same post") //nolint:gochecknoglobals
	ErrInvalidSearchQuery     = errors.New("q is required")                               //nolint:gochecknoglobals
	ErrProjectValidatorUnset  = errors.New("project validator is not configured")         //nolint:gochecknoglobals
	ErrInvalidLimits          = errors.New("board limits are invalid")                    //nolint:gochecknoglobals
	ErrCommentCycle           = errors.New("comment parent cycle detected")               //nolint:gochecknoglobals
	ErrMissingAdapterIdentity = errors.New("authenticated adapter identity is required")  //nolint:gochecknoglobals
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

func forbidden(err error, code string) error {
	return NewServiceError(err, code, http.StatusForbidden)
}

type Limits struct {
	DefaultPageSize        int
	MaxPageSize            int
	MaxPathComments        int
	MaxProjectIDBytes      int
	MaxTitleBytes          int
	MaxBodyBytes           int
	MaxAuthorIdentityBytes int
	MaxMetadataBytes       int
	MaxSearchQueryBytes    int
	MaxPurgeReasonBytes    int
	MaxIdempotencyKeyBytes int
}

func DefaultLimits() Limits {
	return Limits{
		DefaultPageSize:        DefaultPageSize,
		MaxPageSize:            MaxPageSize,
		MaxPathComments:        MaxPathComments,
		MaxProjectIDBytes:      DefaultMaxProjectIDBytes,
		MaxTitleBytes:          DefaultMaxTitleBytes,
		MaxBodyBytes:           DefaultMaxBodyBytes,
		MaxAuthorIdentityBytes: DefaultMaxAuthorIdentityBytes,
		MaxMetadataBytes:       DefaultMaxMetadataBytes,
		MaxSearchQueryBytes:    DefaultMaxSearchQueryBytes,
		MaxPurgeReasonBytes:    DefaultMaxPurgeReasonBytes,
		MaxIdempotencyKeyBytes: DefaultMaxIdempotencyKeyBytes,
	}
}

func (l Limits) withDefaults() Limits {
	defaults := DefaultLimits()
	if l.DefaultPageSize == 0 {
		l.DefaultPageSize = defaults.DefaultPageSize
	}
	if l.MaxPageSize == 0 {
		l.MaxPageSize = defaults.MaxPageSize
	}
	if l.MaxPathComments == 0 {
		l.MaxPathComments = defaults.MaxPathComments
	}
	if l.MaxProjectIDBytes == 0 {
		l.MaxProjectIDBytes = defaults.MaxProjectIDBytes
	}
	if l.MaxTitleBytes == 0 {
		l.MaxTitleBytes = defaults.MaxTitleBytes
	}
	if l.MaxBodyBytes == 0 {
		l.MaxBodyBytes = defaults.MaxBodyBytes
	}
	if l.MaxAuthorIdentityBytes == 0 {
		l.MaxAuthorIdentityBytes = defaults.MaxAuthorIdentityBytes
	}
	if l.MaxMetadataBytes == 0 {
		l.MaxMetadataBytes = defaults.MaxMetadataBytes
	}
	if l.MaxSearchQueryBytes == 0 {
		l.MaxSearchQueryBytes = defaults.MaxSearchQueryBytes
	}
	if l.MaxPurgeReasonBytes == 0 {
		l.MaxPurgeReasonBytes = defaults.MaxPurgeReasonBytes
	}
	if l.MaxIdempotencyKeyBytes == 0 {
		l.MaxIdempotencyKeyBytes = defaults.MaxIdempotencyKeyBytes
	}
	return l
}

func (l Limits) Validate() error {
	if l.DefaultPageSize <= 0 || l.MaxPageSize < l.DefaultPageSize || l.MaxPathComments <= 0 ||
		l.MaxProjectIDBytes <= 0 || l.MaxTitleBytes <= 0 || l.MaxBodyBytes <= 0 ||
		l.MaxAuthorIdentityBytes <= 0 || l.MaxMetadataBytes <= 0 || l.MaxSearchQueryBytes <= 0 ||
		l.MaxPurgeReasonBytes <= 0 || l.MaxIdempotencyKeyBytes <= 0 {
		return ErrInvalidLimits
	}
	return nil
}

func (l Limits) validateProjectID(projectID string) error {
	return validateStringFieldSize("project_id", projectID, l.MaxProjectIDBytes)
}

func (l Limits) validatePostFields(projectID string, req CreatePostRequest) error {
	if err := l.validateProjectID(projectID); err != nil {
		return err
	}
	if err := validateStringFieldSize("title", strings.TrimSpace(req.Title), l.MaxTitleBytes); err != nil {
		return err
	}
	if err := validateStringFieldSize("body_markdown", strings.TrimSpace(req.BodyMarkdown), l.MaxBodyBytes); err != nil {
		return err
	}
	if err := validateStringFieldSize("author_identity", strings.TrimSpace(req.AuthorIdentity), l.MaxAuthorIdentityBytes); err != nil {
		return err
	}
	if err := validateFieldSize("metadata", req.Metadata, l.MaxMetadataBytes); err != nil {
		return err
	}
	return validateStringFieldSize("idempotency_key", strings.TrimSpace(req.IdempotencyKey), l.MaxIdempotencyKeyBytes)
}

func (l Limits) validateCommentFields(req CreateCommentRequest) error {
	if err := validateStringFieldSize("body_markdown", strings.TrimSpace(req.BodyMarkdown), l.MaxBodyBytes); err != nil {
		return err
	}
	if err := validateStringFieldSize("author_identity", strings.TrimSpace(req.AuthorIdentity), l.MaxAuthorIdentityBytes); err != nil {
		return err
	}
	if err := validateFieldSize("metadata", req.Metadata, l.MaxMetadataBytes); err != nil {
		return err
	}
	return validateStringFieldSize("idempotency_key", strings.TrimSpace(req.IdempotencyKey), l.MaxIdempotencyKeyBytes)
}

func (l Limits) validateSearchQuery(query string) error {
	return validateStringFieldSize("q", query, l.MaxSearchQueryBytes)
}

func (l Limits) validatePurgeRequest(req PurgeRequest) error {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return ErrMissingReason
	}
	return validateStringFieldSize("reason", reason, l.MaxPurgeReasonBytes)
}

func validateFieldSize(name string, value []byte, maximum int) error {
	if len(value) > maximum {
		return fmt.Errorf("%s exceeds configured maximum of %d bytes", name, maximum)
	}
	return nil
}

func validateStringFieldSize(name string, value string, maximum int) error {
	return validateFieldSize(name, []byte(value), maximum)
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
	IdempotencyKey string          `json:"idempotency_key"`
}

type CreateCommentRequest struct {
	ParentCommentID *int64          `json:"parent_comment_id"`
	BodyMarkdown    string          `json:"body_markdown"`
	AuthorIdentity  string          `json:"author_identity"`
	Metadata        json.RawMessage `json:"metadata"`
	IdempotencyKey  string          `json:"idempotency_key"`
}

// PurgeRequest is intentionally typed even though its caller supplied values
// are not retained. ActorIdentity is accepted for wire compatibility with
// existing clients, but it is never authoritative. The authenticated adapter
// identity is bound by NewHTTPServer after service-token authentication.
type PurgeRequest struct {
	ActorIdentity string `json:"actor_identity"`
	Reason        string `json:"reason"`
}

func (r PurgeRequest) Validate() error {
	reason := strings.TrimSpace(r.Reason)
	if reason == "" {
		return ErrMissingReason
	}
	if len(reason) > MaxPurgeReasonSize {
		return fmt.Errorf("reason exceeds %d characters", MaxPurgeReasonSize)
	}
	return nil
}

// PurgeAudit is the private store-bound audit context for a destructive action.
// Board's current schema has no audit table, so this context prevents actor
// impersonation at the service/store boundary while keeping the authenticated
// adapter identity and reason available to a future audit sink.
type PurgeAudit struct {
	AdapterIdentity string
	Reason          string
}

type (
	adapterIdentityContextKey struct{}
	purgeAuditContextKey      struct{}
)

// WithAuthenticatedAdapterIdentity binds the server-configured adapter
// identity after transport authentication. HTTP request JSON cannot set it.
func WithAuthenticatedAdapterIdentity(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, adapterIdentityContextKey{}, strings.TrimSpace(identity))
}

func authenticatedAdapterIdentity(ctx context.Context) (string, bool) {
	identity, ok := ctx.Value(adapterIdentityContextKey{}).(string)
	identity = strings.TrimSpace(identity)
	return identity, ok && identity != ""
}

func withPurgeAudit(ctx context.Context, audit PurgeAudit) context.Context {
	audit.AdapterIdentity = strings.TrimSpace(audit.AdapterIdentity)
	audit.Reason = strings.TrimSpace(audit.Reason)
	return context.WithValue(ctx, purgeAuditContextKey{}, audit)
}

func purgeAuditFromContext(ctx context.Context) (PurgeAudit, bool) {
	audit, ok := ctx.Value(purgeAuditContextKey{}).(PurgeAudit)
	audit.AdapterIdentity = strings.TrimSpace(audit.AdapterIdentity)
	return audit, ok && audit.AdapterIdentity != ""
}

func requirePurgeAudit(ctx context.Context) error {
	if _, ok := purgeAuditFromContext(ctx); !ok {
		return forbidden(ErrMissingAdapterIdentity, "purge_adapter_identity_required")
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
