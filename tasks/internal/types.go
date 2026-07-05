package tasks

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	StatusPlanned    = "planned"
	StatusInProgress = "in_progress"
	StatusReview     = "review"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
	StatusCancelled  = "cancelled"

	AvailabilityAvailable             = "available"
	AvailabilityWaitingOnDependencies = "waiting_on_dependencies"
)

var (
	ErrTaskNotFound              = errors.New("task not found")                                                 //nolint:gochecknoglobals
	ErrInvalidTask               = errors.New("invalid task")                                                   //nolint:gochecknoglobals
	ErrMissingProjectID          = errors.New("project_id is required")                                         //nolint:gochecknoglobals
	ErrMissingTitle              = errors.New("title is required")                                              //nolint:gochecknoglobals
	ErrInvalidStatus             = errors.New("invalid status")                                                 //nolint:gochecknoglobals
	ErrInvalidPriority           = errors.New("priority must be 1 through 5")                                   //nolint:gochecknoglobals
	ErrMissingAgent              = errors.New("agent is required")                                              //nolint:gochecknoglobals
	ErrEmptyPatch                = errors.New("patch has no mutable fields")                                    //nolint:gochecknoglobals
	ErrBlockedContextMissing     = errors.New("blocked transition requires blocker_summary and blocker_reason") //nolint:gochecknoglobals
	ErrParentProjectMismatch     = errors.New("parent task must be in the same project")                        //nolint:gochecknoglobals
	ErrDependencyProjectMismatch = errors.New("dependency task must be in the same project")                    //nolint:gochecknoglobals
	ErrDependencyCycle           = errors.New("dependency would create a cycle")                                //nolint:gochecknoglobals
	ErrParentCycle               = errors.New("parent relationship would create a cycle")                       //nolint:gochecknoglobals
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

func notFound(id int64) error {
	return NewServiceError(fmt.Errorf("%w: %d", ErrTaskNotFound, id), "task_not_found", http.StatusNotFound)
}

func conflict(err error, code string) error {
	return NewServiceError(err, code, http.StatusConflict)
}

type Task struct {
	id                        int64
	projectID                 string
	parentID                  *int64
	title                     string
	description               string
	status                    string
	priority                  int
	assignedTo                string
	tags                      []string
	blockerSummary            string
	blockerReason             string
	blockerAttemptedRemedies  string
	blockerSuggestedNextStep  string
	blockerRequiresHumanInput bool
	createdAt                 time.Time
	updatedAt                 time.Time
}

type NewTaskParams struct {
	ID                        int64
	ProjectID                 string
	ParentID                  *int64
	Title                     string
	Description               string
	Status                    string
	Priority                  int
	AssignedTo                string
	Tags                      []string
	BlockerSummary            string
	BlockerReason             string
	BlockerAttemptedRemedies  string
	BlockerSuggestedNextStep  string
	BlockerRequiresHumanInput bool
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

func NewTask(params NewTaskParams) (*Task, error) {
	projectID := strings.TrimSpace(params.ProjectID)
	if projectID == "" {
		return nil, ErrMissingProjectID
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		return nil, ErrMissingTitle
	}
	status := defaultStatus(params.Status)
	if !validStatus(status) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}
	priority := params.Priority
	if priority == 0 {
		priority = 3
	}
	if priority < 1 || priority > 5 {
		return nil, ErrInvalidPriority
	}
	createdAt := params.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := params.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	return &Task{
		id:                        params.ID,
		projectID:                 projectID,
		parentID:                  cloneInt64(params.ParentID),
		title:                     title,
		description:               strings.TrimSpace(params.Description),
		status:                    status,
		priority:                  priority,
		assignedTo:                strings.TrimSpace(params.AssignedTo),
		tags:                      normalizeTags(params.Tags),
		blockerSummary:            strings.TrimSpace(params.BlockerSummary),
		blockerReason:             strings.TrimSpace(params.BlockerReason),
		blockerAttemptedRemedies:  strings.TrimSpace(params.BlockerAttemptedRemedies),
		blockerSuggestedNextStep:  strings.TrimSpace(params.BlockerSuggestedNextStep),
		blockerRequiresHumanInput: params.BlockerRequiresHumanInput,
		createdAt:                 createdAt,
		updatedAt:                 updatedAt,
	}, nil
}

func (t *Task) ID() int64                        { return t.id }
func (t *Task) ProjectID() string                { return t.projectID }
func (t *Task) ParentID() *int64                 { return cloneInt64(t.parentID) }
func (t *Task) Title() string                    { return t.title }
func (t *Task) Description() string              { return t.description }
func (t *Task) Status() string                   { return t.status }
func (t *Task) Priority() int                    { return t.priority }
func (t *Task) AssignedTo() string               { return t.assignedTo }
func (t *Task) Tags() []string                   { return append([]string(nil), t.tags...) }
func (t *Task) BlockerSummary() string           { return t.blockerSummary }
func (t *Task) BlockerReason() string            { return t.blockerReason }
func (t *Task) BlockerAttemptedRemedies() string { return t.blockerAttemptedRemedies }
func (t *Task) BlockerSuggestedNextStep() string { return t.blockerSuggestedNextStep }
func (t *Task) BlockerRequiresHumanInput() bool  { return t.blockerRequiresHumanInput }
func (t *Task) CreatedAt() time.Time             { return t.createdAt }
func (t *Task) UpdatedAt() time.Time             { return t.updatedAt }

type DependencyInfo struct {
	TaskID int64
	Title  string
	Status string
}

type TaskSummary struct {
	Task                      *Task
	DependencyCount           int
	UnfinishedDependencyCount int
	SubtaskCount              int
}

func (s TaskSummary) Availability() string {
	return availability(s.Task.Status(), s.UnfinishedDependencyCount)
}

type TaskDetail struct {
	Task         *Task
	Dependencies []DependencyInfo
	Subtasks     []TaskSummary
	History      []TaskHistoryEntry
}

type TaskHistoryEntry struct {
	ID        int64
	TaskID    int64
	Field     string
	OldValue  string
	NewValue  string
	ChangedBy string
	ChangedAt time.Time
}

type TaskChangeEvent struct {
	ID      int64
	Kind    string
	Changed time.Time
	Summary TaskSummary
}

func defaultStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return StatusPlanned
	}
	return status
}

func validStatus(status string) bool {
	switch status {
	case StatusPlanned, StatusInProgress, StatusReview, StatusBlocked, StatusDone, StatusCancelled:
		return true
	default:
		return false
	}
}

func terminalStatus(status string) bool {
	return status == StatusDone || status == StatusCancelled
}

func dependencySatisfiedStatus(status string) bool {
	return status == StatusReview || terminalStatus(status)
}

func availability(status string, unfinishedDependencies int) string {
	switch status {
	case StatusPlanned:
		if unfinishedDependencies > 0 {
			return AvailabilityWaitingOnDependencies
		}
		return AvailabilityAvailable
	case StatusInProgress, StatusReview, StatusBlocked, StatusDone, StatusCancelled:
		return status
	default:
		return status
	}
}

func normalizeTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		normalized = append(normalized, tag)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
