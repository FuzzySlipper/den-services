package boardrelay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	itemKindPost    = "post"
	itemKindComment = "comment"

	metadataSchema = "den-board-relay/v1"
	markerPrefix   = "<!-- den-board-relay:v1"
)

var (
	ErrMissingProjectID   = errors.New("project_id is required")               //nolint:gochecknoglobals
	ErrInvalidRepository  = errors.New("github repository must be owner/name") //nolint:gochecknoglobals
	ErrInvalidVisibility  = errors.New("visibility must be public or private") //nolint:gochecknoglobals
	ErrBoardPostNotFound  = errors.New("board post not found")                 //nolint:gochecknoglobals
	ErrBoardCommentAbsent = errors.New("board comment not found")              //nolint:gochecknoglobals
)

type BoardPost struct {
	ID             int64     `json:"id"`
	ProjectID      string    `json:"project_id"`
	Title          string    `json:"title"`
	BodyMarkdown   string    `json:"body_markdown"`
	AuthorIdentity string    `json:"author_identity"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type BoardComment struct {
	ID              int64     `json:"id"`
	PostID          int64     `json:"post_id"`
	ParentCommentID *int64    `json:"parent_comment_id"`
	BodyMarkdown    string    `json:"body_markdown"`
	AuthorIdentity  string    `json:"author_identity"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type BoardPage struct {
	Posts       []BoardPost
	NextAfterID *int64
}

type CommentPage struct {
	Comments    []BoardComment
	NextAfterID *int64
}

type BoardCreatePostRequest struct {
	Title          string            `json:"title"`
	BodyMarkdown   string            `json:"body_markdown"`
	AuthorIdentity string            `json:"author_identity"`
	Metadata       map[string]string `json:"metadata"`
	IdempotencyKey string            `json:"idempotency_key"`
}

type BoardCreateCommentRequest struct {
	ParentCommentID *int64            `json:"parent_comment_id"`
	BodyMarkdown    string            `json:"body_markdown"`
	AuthorIdentity  string            `json:"author_identity"`
	Metadata        map[string]string `json:"metadata"`
	IdempotencyKey  string            `json:"idempotency_key"`
}

type BoardClient interface {
	ListPosts(ctx context.Context, projectID string, afterID *int64, limit int) (BoardPage, error)
	GetPost(ctx context.Context, postID int64) (*BoardPost, error)
	GetComment(ctx context.Context, commentID int64) (*BoardComment, error)
	ListComments(ctx context.Context, postID int64, parentCommentID *int64, afterID *int64, limit int) (CommentPage, error)
	CreatePost(ctx context.Context, projectID string, request BoardCreatePostRequest) (*BoardPost, error)
	CreateComment(ctx context.Context, postID int64, request BoardCreateCommentRequest) (*BoardComment, error)
}

type GitHubIssue struct {
	ID        int64
	Number    int64
	Title     string
	Body      string
	Login     string
	HTMLURL   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GitHubComment struct {
	ID        int64
	Body      string
	Login     string
	HTMLURL   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GitHubClient interface {
	ListIssues(ctx context.Context, repository string) ([]GitHubIssue, error)
	CreateIssue(ctx context.Context, repository string, title string, body string) (GitHubIssue, error)
	ListIssueComments(ctx context.Context, repository string, issueNumber int64) ([]GitHubComment, error)
	CreateIssueComment(ctx context.Context, repository string, issueNumber int64, body string) (GitHubComment, error)
	SetRepositoryVisibility(ctx context.Context, repository string, visibility string) error
}

type Mapping struct {
	ProjectID       string
	BoardKind       string
	BoardID         int64
	GitHubKind      string
	GitHubID        int64
	IssueNumber     int64
	GitHubURL       string
	Origin          string
	GitHubUpdatedAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type MappingStore interface {
	Ping(ctx context.Context) error
	FindByBoard(ctx context.Context, projectID string, boardKind string, boardID int64) (*Mapping, error)
	FindByGitHub(ctx context.Context, projectID string, githubKind string, githubID int64) (*Mapping, error)
	Save(ctx context.Context, mapping Mapping) error
}

type SyncReceipt struct {
	ProjectID              string        `json:"project_id"`
	Repository             string        `json:"repository"`
	ImportedPosts          int           `json:"imported_posts"`
	ImportedComments       int           `json:"imported_comments"`
	ExportedPosts          int           `json:"exported_posts"`
	ExportedComments       int           `json:"exported_comments"`
	RecoveredMappings      int           `json:"recovered_mappings"`
	UnsupportedRemoteEdits int           `json:"unsupported_remote_edits"`
	SkippedItems           int           `json:"skipped_items"`
	ConflictedItems        int           `json:"conflicted_items"`
	ErrorItems             int           `json:"error_items"`
	ItemURLs               []string      `json:"item_urls"`
	OmittedItemURLs        int           `json:"omitted_item_urls"`
	Failures               []SyncFailure `json:"failures"`
}

// SyncFailure identifies the phase that stopped a manual sync. The receipt
// remains useful after a partial run: counts and links describe work that was
// already committed before this failure.
type SyncFailure struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

// SyncRunError marks an incomplete run while preserving the original cause.
// Callers must use the accompanying SyncReceipt rather than discarding it.
type SyncRunError struct {
	Phase string
	Cause error
}

func (e *SyncRunError) Error() string { return fmt.Sprintf("sync %s: %v", e.Phase, e.Cause) }
func (e *SyncRunError) Unwrap() error { return e.Cause }

type VisibilityRequest struct {
	Visibility string `json:"visibility"`
}

func normalizeProjectID(projectID string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", ErrMissingProjectID
	}
	return projectID, nil
}

func normalizeRepository(repository string) (string, error) {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if len(strings.Split(repository, "/")) != 2 {
		return "", ErrInvalidRepository
	}
	return repository, nil
}

func normalizeVisibility(visibility string) (string, error) {
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	if visibility != "public" && visibility != "private" {
		return "", ErrInvalidVisibility
	}
	return visibility, nil
}

func relayMetadata(kind string, githubID int64, githubURL string, login string, sourceTimestamp time.Time) map[string]string {
	return map[string]string{
		"schema":            metadataSchema,
		"kind":              kind,
		"github_id":         fmt.Sprintf("%d", githubID),
		"github_url":        githubURL,
		"github_login":      login,
		"github_created_at": sourceTimestamp.UTC().Format(time.RFC3339Nano),
	}
}
