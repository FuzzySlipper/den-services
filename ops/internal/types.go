package ops

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

const (
	MaxIDBytes         = 128
	MaxReportBodyBytes = 4 << 20
	MaxListLimit       = 500
)

var (
	ErrJobNotFound          = errors.New("job not found")          //nolint:gochecknoglobals
	ErrRunNotFound          = errors.New("run not found")          //nolint:gochecknoglobals
	ErrHostNotFound         = errors.New("host not found")         //nolint:gochecknoglobals
	ErrVerificationNotFound = errors.New("verification not found") //nolint:gochecknoglobals
	idPattern               = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]*$`)
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

// RunState is what a reporter says happened. Only running is non-terminal.
type RunState string

const (
	RunRunning RunState = "running"
	RunSuccess RunState = "success"
	RunWarning RunState = "warning"
	RunFailed  RunState = "failed"
	RunSkipped RunState = "skipped"
)

func (s RunState) IsValid() bool {
	switch s {
	case RunRunning, RunSuccess, RunWarning, RunFailed, RunSkipped:
		return true
	}
	return false
}

func (s RunState) IsTerminal() bool { return s.IsValid() && s != RunRunning }

type VerificationKind string

const (
	VerificationCheck       VerificationKind = "verification"
	VerificationRestoreTest VerificationKind = "restore_test"
)

func (k VerificationKind) IsValid() bool {
	return k == VerificationCheck || k == VerificationRestoreTest
}

// VerificationState is the outcome of a check or restore test.
type VerificationState string

const (
	VerificationSuccess VerificationState = "success"
	VerificationWarning VerificationState = "warning"
	VerificationFailed  VerificationState = "failed"
)

func (s VerificationState) IsValid() bool {
	return s == VerificationSuccess || s == VerificationWarning || s == VerificationFailed
}

// JobStatus is the rolled-up condition of one expected job, worst first.
type JobStatus string

const (
	StatusUnknown       JobStatus = "unknown"
	StatusFailed        JobStatus = "failed"
	StatusOverdue       JobStatus = "overdue"
	StatusNeverReported JobStatus = "never_reported"
	StatusWarning       JobStatus = "warning"
	StatusRunning       JobStatus = "running"
	StatusOK            JobStatus = "ok"
	StatusDisabled      JobStatus = "disabled"
)

// AllJobStatuses lists every status in severity order, worst first.
func AllJobStatuses() []JobStatus {
	return []JobStatus{
		StatusUnknown, StatusFailed, StatusOverdue, StatusNeverReported,
		StatusWarning, StatusRunning, StatusOK, StatusDisabled,
	}
}

type Freshness string

const (
	FreshnessFresh       Freshness = "fresh"
	FreshnessOverdue     Freshness = "overdue"
	FreshnessUnscheduled Freshness = "unscheduled"
)

type HeartbeatState string

const (
	HeartbeatFresh HeartbeatState = "fresh"
	HeartbeatStale HeartbeatState = "stale"
)

type Host struct {
	HostID      string
	DisplayName string
	Role        string
	Notes       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Job is an expectation: something that should report on a schedule. A job
// created implicitly by a report for an unknown ID has Registered=false.
type Job struct {
	JobID               string
	HostID              string
	DisplayName         string
	Engine              string
	Kind                string
	Source              string
	Scope               string
	Target              string
	Schedule            *Schedule
	HeartbeatStaleAfter time.Duration
	Retention           string
	RecoveryPointer     string
	Notes               string
	Registered          bool
	Enabled             bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ArchiveStats struct {
	OriginalBytes     *int64
	CompressedBytes   *int64
	DeduplicatedBytes *int64
	FileCount         *int64
}

// Run is one observed execution of a job. Ops records it; it never runs it.
type Run struct {
	JobID           string
	RunID           string
	Host            string
	Kind            string
	Engine          string
	Runner          string
	State           RunState
	Phase           string
	StartedAt       time.Time
	UpdatedAt       time.Time
	FinishedAt      *time.Time
	ExitCode        *int
	Error           string
	Reason          string
	Warnings        []string
	Notices         []string
	Target          string
	ArchiveID       string
	ArchiveName     string
	Stats           ArchiveStats
	DurationSeconds *float64
	// Complete means this run produced a usable restore point (backups) or a
	// clean result (maintenance). Failure or warning never clears an older one.
	Complete        bool
	LogPath         string
	ManifestPath    string
	RunDirectory    string
	RecoveryPointer string
	RawReport       []byte
	ReceivedAt      time.Time
	ReportCount     int64
}

func (r *Run) IsTerminal() bool { return r.FinishedAt != nil }

type Verification struct {
	JobID          string
	VerificationID string
	RunID          string
	Kind           VerificationKind
	// CheckType names the check, such as repository-and-latest-archive.
	CheckType    string
	State        VerificationState
	At           time.Time
	Scope        string
	Archive      string
	FullDataRead *bool
	EvidencePath string
	Checked      []string
	RawReport    []byte
	ReceivedAt   time.Time
}
