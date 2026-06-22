package docpublish

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid doc-publish request")   //nolint:gochecknoglobals
	ErrNotFound       = errors.New("publication not found")         //nolint:gochecknoglobals
	ErrRepoUnsafe     = errors.New("blog repo safety check failed") //nolint:gochecknoglobals
)

type ServiceError struct {
	err    error
	code   string
	status int
}

func newServiceError(err error, code string, status int) *ServiceError {
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

func invalidRequest(message string) error {
	return newServiceError(fmt.Errorf("%w: %s", ErrInvalidRequest, message), "invalid_doc_publish_request", http.StatusBadRequest)
}

func repoUnsafe(message string) error {
	return newServiceError(fmt.Errorf("%w: %s", ErrRepoUnsafe, message), "repo_safety_check_failed", http.StatusConflict)
}

func notFound(message string) error {
	return newServiceError(fmt.Errorf("%w: %s", ErrNotFound, message), "not_found", http.StatusNotFound)
}

type PublicationStatus string

const (
	PublicationStatusPreviewed PublicationStatus = "previewed"
	PublicationStatusPublished PublicationStatus = "published"
	PublicationStatusFailed    PublicationStatus = "failed"
)

func (s PublicationStatus) IsValid() bool {
	switch s {
	case PublicationStatusPreviewed, PublicationStatusPublished, PublicationStatusFailed:
		return true
	}
	return false
}

type DocumentSource struct {
	ProjectID         string `json:"project_id,omitempty"`
	DocumentProjectID string `json:"document_project_id"`
	DocumentSlug      string `json:"document_slug"`
}

type SourceDocument struct {
	Title     string    `json:"title"`
	Slug      string    `json:"slug,omitempty"`
	Markdown  string    `json:"markdown"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type PublicationOptions struct {
	Title     string   `json:"title,omitempty"`
	Slug      string   `json:"slug,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Overwrite bool     `json:"overwrite,omitempty"`
	DryRun    bool     `json:"dry_run,omitempty"`
}

type PublicationRecord struct {
	ID                string            `json:"id"`
	SourceProjectID   string            `json:"source_project_id,omitempty"`
	DocumentProjectID string            `json:"document_project_id"`
	DocumentSlug      string            `json:"document_slug"`
	SourceVersion     string            `json:"source_version,omitempty"`
	Title             string            `json:"title"`
	Slug              string            `json:"slug"`
	RepoID            string            `json:"repo_id"`
	Branch            string            `json:"branch"`
	PostPath          string            `json:"post_path"`
	PublicURL         string            `json:"public_url"`
	GitCommit         string            `json:"git_commit,omitempty"`
	Status            PublicationStatus `json:"status"`
	RequestedBy       string            `json:"requested_by"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	LastError         string            `json:"last_error,omitempty"`
}
