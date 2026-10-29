package resources

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

const (
	MaxIDBytes          = 128
	MaxBodyBytes        = 4 << 20
	DefaultWorkLimit    = 250
	MaxWorkLimit        = 1000
	DefaultTTLSeconds   = 180
	DefaultPollSeconds  = 20
	busyCPUPercent      = 65
	busyGPUPercent      = 10
	defaultResourceKind = "installation"
)

var (
	ErrHostNotFound     = errors.New("host not found")     //nolint:gochecknoglobals
	ErrResourceNotFound = errors.New("resource not found") //nolint:gochecknoglobals
	ErrWorkNotFound     = errors.New("work not found")     //nolint:gochecknoglobals
	idPattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]*$`)
)

type ServiceError struct {
	err    error
	code   string
	status int
}

func (e *ServiceError) Error() string   { return e.err.Error() }
func (e *ServiceError) Unwrap() error   { return e.err }
func (e *ServiceError) Code() string    { return e.code }
func (e *ServiceError) HTTPStatus() int { return e.status }

func validationFailed(format string, args ...any) error {
	return &ServiceError{err: fmt.Errorf(format, args...), code: "validation_failed", status: http.StatusBadRequest}
}

func notFound(err error, code string, id string) error {
	return &ServiceError{err: fmt.Errorf("%w: %s", err, id), code: code, status: http.StatusNotFound}
}

func validateID(field string, value string) error {
	if value == "" || len(value) > MaxIDBytes || !idPattern.MatchString(value) {
		return validationFailed("%s must be 1-%d characters of letters, digits and ._:@- starting with a letter or digit", field, MaxIDBytes)
	}
	return nil
}

// WorkState is what a reporter says about a job. Queued and running are
// active; the rest are terminal and never revert.
type WorkState string

const (
	WorkQueued    WorkState = "queued"
	WorkRunning   WorkState = "running"
	WorkCompleted WorkState = "completed"
	WorkFailed    WorkState = "failed"
	WorkCancelled WorkState = "cancelled"
)

func (s WorkState) IsValid() bool { return s.IsActive() || s.IsTerminal() }
func (s WorkState) IsActive() bool {
	return s == WorkQueued || s == WorkRunning
}

func (s WorkState) IsTerminal() bool {
	return s == WorkCompleted || s == WorkFailed || s == WorkCancelled
}

type Lifecycle string

const (
	LifecycleMaintained   Lifecycle = "maintained"
	LifecycleExperimental Lifecycle = "experimental"
	LifecycleRetired      Lifecycle = "retired"
	LifecycleUnknown      Lifecycle = "unknown"
)

func (l Lifecycle) IsValid() bool {
	switch l {
	case LifecycleMaintained, LifecycleExperimental, LifecycleRetired, LifecycleUnknown:
		return true
	}
	return false
}

// HostStatus is the board's one-word reading of a host's latest sample.
type HostStatus string

const (
	HostUnknown HostStatus = "unknown"
	HostWorking HostStatus = "working"
	HostQueued  HostStatus = "queued"
	HostQuiet   HostStatus = "quiet"
)

type Host struct {
	ID       string
	Name     string
	Role     string
	Access   string
	Note     string
	Position int
	// Telemetry is the newest accepted probe snapshot, kept as sent.
	Telemetry     json.RawMessage
	Error         string
	AttemptedAt   *time.Time
	SampledAt     *time.Time
	ReceivedAt    *time.Time
	Apps          []App
	AppsCheckedAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type App struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	URL       string    `json:"url"`
	Resources []string  `json:"resources"`
	Online    bool      `json:"online"`
	Running   *int      `json:"running"`
	Queued    *int      `json:"queued"`
	Jobs      []AppJob  `json:"jobs"`
	Error     *string   `json:"error"`
	CheckedAt time.Time `json:"checked_at"`
}

type AppJob struct {
	ID        string   `json:"id"`
	State     string   `json:"state"`
	NodeTypes []string `json:"node_types"`
}

type Resource struct {
	ID                 string
	Host               string
	Name               string
	Kind               string
	Path               string
	ProbePath          string
	RuntimePath        string
	Access             string
	Sharing            string
	Lifecycle          Lifecycle
	Notes              string
	Guide              string
	Match              string
	UpdatedAt          time.Time
	LastUsedAt         *time.Time
	LastUsedBy         string
	ObservedActivityAt *time.Time
	ConfirmedAt        *time.Time
	ConfirmedBy        string
	PathPresent        *bool
	PathCheckedAt      *time.Time
}

// ProbedPath is the path the host probe checks for presence.
func (r *Resource) ProbedPath() string {
	if r.ProbePath != "" {
		return r.ProbePath
	}
	return r.Path
}

type Work struct {
	ID         string
	Host       string
	Source     string
	ExternalID *string
	Title      string
	Owner      string
	Project    string
	Resources  []string
	State      WorkState
	Note       string
	URL        string
	TTL        time.Duration
	CreatedAt  time.Time
	StartedAt  *time.Time
	UpdatedAt  time.Time
	ExpiresAt  time.Time
	EndedAt    *time.Time
}

// Stale is an active report whose heartbeat expired. It never means free.
func (w *Work) Stale(now time.Time) bool { return w.State.IsActive() && w.ExpiresAt.Before(now) }

// WorkFilter narrows a work listing; zero values do not filter.
type WorkFilter struct {
	ActiveOnly bool
	Source     string
	Host       string
	Limit      int
}
