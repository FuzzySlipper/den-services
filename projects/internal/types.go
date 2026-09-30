package projects

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	KindProject       = "project"
	KindPersonal      = "personal"
	KindAssistant     = "assistant"
	KindKnowledgeBase = "knowledge_base"
	KindSystem        = "system"

	VisibilityNormal   = "normal"
	VisibilityHidden   = "hidden"
	VisibilityArchived = "archived"
)

var (
	ErrScopeNotFound        = errors.New("scope not found")                         //nolint:gochecknoglobals
	ErrInvalidScope         = errors.New("invalid scope")                           //nolint:gochecknoglobals
	ErrInvalidKind          = errors.New("invalid kind")                            //nolint:gochecknoglobals
	ErrInvalidVisibility    = errors.New("invalid visibility")                      //nolint:gochecknoglobals
	ErrMissingID            = errors.New("id is required")                          //nolint:gochecknoglobals
	ErrMissingName          = errors.New("name is required")                        //nolint:gochecknoglobals
	ErrArchivedScopeWrite   = errors.New("scope is archived")                       //nolint:gochecknoglobals
	ErrDuplicateScope       = errors.New("scope already exists")                    //nolint:gochecknoglobals
	ErrEmptyPatch           = errors.New("patch has no mutable fields")             //nolint:gochecknoglobals
	ErrProtectedScope       = errors.New("scope is protected")                      //nolint:gochecknoglobals
	ErrInvalidRepositoryURL = errors.New("repository_url must be a Git remote URL") //nolint:gochecknoglobals
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

func notFound(id string) error {
	return NewServiceError(fmt.Errorf("%w: %s", ErrScopeNotFound, id), "scope_not_found", http.StatusNotFound)
}

func conflict(err error, code string) error {
	return NewServiceError(err, code, http.StatusConflict)
}

type Scope struct {
	id            string
	name          string
	kind          string
	visibility    string
	owner         string
	rootPath      string
	repositoryURL string
	description   string
	settingsJSON  []byte
	createdAt     time.Time
	updatedAt     time.Time
}

type NewScopeParams struct {
	ID            string
	Name          string
	Kind          string
	Visibility    string
	Owner         string
	RootPath      string
	RepositoryURL string
	Description   string
	SettingsJSON  []byte
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewScope(params NewScopeParams) (*Scope, error) {
	id := strings.TrimSpace(params.ID)
	if id == "" {
		return nil, ErrMissingID
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, ErrMissingName
	}
	kind := defaultKind(params.Kind)
	if !validKind(kind) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidKind, kind)
	}
	visibility := defaultVisibility(params.Visibility)
	if !validVisibility(visibility) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidVisibility, visibility)
	}
	repositoryURL := strings.TrimSpace(params.RepositoryURL)
	if !validRepositoryURL(repositoryURL) {
		return nil, ErrInvalidRepositoryURL
	}
	createdAt := params.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := params.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	settingsJSON := append([]byte(nil), params.SettingsJSON...)
	return &Scope{
		id:            id,
		name:          name,
		kind:          kind,
		visibility:    visibility,
		owner:         strings.TrimSpace(params.Owner),
		rootPath:      strings.TrimSpace(params.RootPath),
		repositoryURL: repositoryURL,
		description:   strings.TrimSpace(params.Description),
		settingsJSON:  settingsJSON,
		createdAt:     createdAt,
		updatedAt:     updatedAt,
	}, nil
}

func (s *Scope) ID() string {
	return s.id
}

func (s *Scope) Name() string {
	return s.name
}

func (s *Scope) Kind() string {
	return s.kind
}

func (s *Scope) Visibility() string {
	return s.visibility
}

func (s *Scope) Owner() string {
	return s.owner
}

func (s *Scope) RootPath() string {
	return s.rootPath
}

func (s *Scope) RepositoryURL() string {
	return s.repositoryURL
}

func (s *Scope) Description() string {
	return s.description
}

func (s *Scope) SettingsJSON() []byte {
	return append([]byte(nil), s.settingsJSON...)
}

func (s *Scope) CreatedAt() time.Time {
	return s.createdAt
}

func (s *Scope) UpdatedAt() time.Time {
	return s.updatedAt
}

func (s *Scope) Writable() bool {
	return s.visibility != VisibilityArchived
}

func (s *Scope) ProtectedFromDelete() bool {
	switch {
	case protectedScopeID(s.id):
		return true
	case s.kind == KindSystem || s.kind == KindPersonal:
		return true
	default:
		return false
	}
}

func defaultKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return KindProject
	}
	return kind
}

func defaultVisibility(visibility string) string {
	visibility = strings.TrimSpace(visibility)
	if visibility == "" {
		return VisibilityNormal
	}
	return visibility
}

func validKind(kind string) bool {
	switch kind {
	case KindProject, KindPersonal, KindAssistant, KindKnowledgeBase, KindSystem:
		return true
	default:
		return false
	}
}

func validVisibility(visibility string) bool {
	switch visibility {
	case VisibilityNormal, VisibilityHidden, VisibilityArchived:
		return true
	default:
		return false
	}
}

func validRepositoryURL(repositoryURL string) bool {
	if repositoryURL == "" {
		return true
	}
	if strings.ContainsAny(repositoryURL, " \t\r\n") {
		return false
	}
	if !strings.Contains(repositoryURL, "://") {
		return validSCPStyleRepositoryURL(repositoryURL)
	}

	parsed, err := url.Parse(repositoryURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ssh", "git":
	default:
		return false
	}
	if parsed.Opaque != "" || parsed.Hostname() == "" || strings.Trim(parsed.Path, "/") == "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return false
		}
	}
	return true
}

func validSCPStyleRepositoryURL(repositoryURL string) bool {
	colon := strings.IndexByte(repositoryURL, ':')
	switch {
	case strings.HasPrefix(repositoryURL, "["):
		closeBracket := strings.Index(repositoryURL, "]:")
		if closeBracket < 0 {
			return false
		}
		colon = closeBracket + 1
	case strings.Contains(repositoryURL, "@["):
		bracketStart := strings.Index(repositoryURL, "@[") + 1
		closeBracket := strings.Index(repositoryURL[bracketStart:], "]:")
		if closeBracket < 0 {
			return false
		}
		colon = bracketStart + closeBracket + 1
	}
	if colon <= 0 || colon >= len(repositoryURL)-1 || repositoryURL[colon] != ':' {
		return false
	}

	authority := repositoryURL[:colon]
	remotePath := repositoryURL[colon+1:]
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		if at == 0 || at == len(authority)-1 {
			return false
		}
		authority = authority[at+1:]
	}
	if authority == "" || strings.ContainsAny(authority, "/\\?#") {
		return false
	}
	if strings.HasPrefix(authority, "[") {
		if len(authority) < 3 || !strings.HasSuffix(authority, "]") {
			return false
		}
	} else if strings.ContainsAny(authority, ":[]@") {
		return false
	}
	if strings.ContainsAny(remotePath, " \t\r\n?#\\") || strings.Trim(remotePath, "/") == "" {
		return false
	}
	return true
}

func protectedScopeID(id string) bool {
	switch strings.TrimSpace(id) {
	case "_global", "den-core", "core":
		return true
	default:
		return false
	}
}
