package review

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	VerdictChangesRequested    = "changes_requested"
	VerdictLooksGood           = "looks_good"
	VerdictFollowUpNeeded      = "follow_up_needed"
	VerdictBlockedByDependency = "blocked_by_dependency"

	CategoryBlockingBug       = "blocking_bug"
	CategoryAcceptanceGap     = "acceptance_gap"
	CategoryTestWeakness      = "test_weakness"
	CategoryFollowUpCandidate = "follow_up_candidate"

	StatusOpen            = "open"
	StatusClaimedFixed    = "claimed_fixed"
	StatusVerifiedFixed   = "verified_fixed"
	StatusNotFixed        = "not_fixed"
	StatusSuperseded      = "superseded"
	StatusSplitToFollowUp = "split_to_follow_up"

	PacketSchema              = "den_review_packet"
	PacketKindReviewRequest   = "review_request"
	PacketKindRereviewRequest = "rereview_request"
	PacketKindReviewFindings  = "review_findings"
	PacketKindResponse        = "implementer_response"
	PacketKindCompletion      = "completion_evidence"

	PacketStatusValid                = "valid"
	PacketStatusAccepted             = "accepted"
	PacketStatusPendingMessageAppend = "pending_message_append"

	GitHubCheckGateStatusPending    = "pending"
	GitHubCheckGateStatusPassed     = "passed"
	GitHubCheckGateStatusFailed     = "failed"
	GitHubCheckGateStatusTimedOut   = "timed_out"
	GitHubCheckGateStatusSuperseded = "superseded"

	GitHubCheckTerminalReasonChecksPassed          = "checks_passed"
	GitHubCheckTerminalReasonChecksFailed          = "checks_failed"
	GitHubCheckTerminalReasonRequiredChecksMissing = "required_checks_missing"
	GitHubCheckTerminalReasonTimedOut              = "timeout"
	GitHubCheckTerminalReasonSuperseded            = "superseded"

	GitHubCheckGateTerminalEventSchema        = "den_review.github_check_gate_terminal_event"
	GitHubCheckGateTerminalEventSchemaVersion = 1

	GitHubCheckEvidenceStatusNotRequired = "not_required"
	GitHubCheckEvidenceStatusPending     = "pending"
	GitHubCheckEvidenceStatusPosted      = "posted"
	GitHubCheckEvidenceStatusError       = "error"

	TaskStatusInProgress = "in_progress"
	TaskStatusReview     = "review"
	TaskStatusDone       = "done"
	TaskStatusCancelled  = "cancelled"
)

var (
	ErrMissingProjectID        = errors.New("project_id is required")                 //nolint:gochecknoglobals
	ErrMissingTaskID           = errors.New("task_id is required")                    //nolint:gochecknoglobals
	ErrMissingActor            = errors.New("actor is required")                      //nolint:gochecknoglobals
	ErrMissingRound            = errors.New("review round not found")                 //nolint:gochecknoglobals
	ErrMissingFinding          = errors.New("review finding not found")               //nolint:gochecknoglobals
	ErrInvalidVerdict          = errors.New("invalid verdict")                        //nolint:gochecknoglobals
	ErrInvalidCategory         = errors.New("invalid category")                       //nolint:gochecknoglobals
	ErrInvalidStatus           = errors.New("invalid status")                         //nolint:gochecknoglobals
	ErrInvalidPacketKind       = errors.New("invalid packet_kind")                    //nolint:gochecknoglobals
	ErrInvalidTaskState        = errors.New("task is not reviewable for packet kind") //nolint:gochecknoglobals
	ErrMissingReviewedCommit   = errors.New("reviewed_head_commit is required")       //nolint:gochecknoglobals
	ErrUncheckedVerify         = errors.New("required verify item is unchecked")      //nolint:gochecknoglobals
	ErrFollowUpStatusMismatch  = errors.New("follow_up_task_id requires split_to_follow_up status")
	ErrMessageClientUnset      = errors.New("messages client is not configured") //nolint:gochecknoglobals
	ErrTaskClientUnset         = errors.New("tasks client is not configured")    //nolint:gochecknoglobals
	ErrProjectScopeClientUnset = errors.New("projects client is not configured") //nolint:gochecknoglobals
	ErrGitHubChecksUnset       = errors.New("github check provider is not configured")
)

type ServiceError struct {
	err     error
	code    string
	field   string
	docsRef string
	status  int
}

func NewServiceError(err error, code string, status int) *ServiceError {
	return &ServiceError{err: err, code: code, status: status}
}

func validationError(err error, code string, field string, topic string) *ServiceError {
	return &ServiceError{
		err:     err,
		code:    code,
		field:   field,
		docsRef: fmt.Sprintf("tool_docs(\"post_review_packet_markdown\", \"%s\")", topic),
		status:  http.StatusBadRequest,
	}
}

func (e *ServiceError) Error() string       { return e.err.Error() }
func (e *ServiceError) Unwrap() error       { return e.err }
func (e *ServiceError) Code() string        { return e.code }
func (e *ServiceError) Field() string       { return e.field }
func (e *ServiceError) DocsRef() string     { return e.docsRef }
func (e *ServiceError) HTTPStatus() int     { return e.status }
func badRequest(err error) error            { return NewServiceError(err, "bad_request", http.StatusBadRequest) }
func notFound(err error, code string) error { return NewServiceError(err, code, http.StatusNotFound) }
func conflict(err error, code string) error { return NewServiceError(err, code, http.StatusConflict) }

type ReviewRound struct {
	ID                      int64
	ProjectID               string
	TaskID                  int64
	RoundNumber             int
	RequestedBy             string
	Branch                  string
	BaseBranch              string
	BaseCommit              string
	HeadCommit              string
	LastReviewedHeadCommit  string
	CommitsSinceLastReview  *int
	TestsRun                []string
	Notes                   string
	PreferredDiffBaseRef    string
	PreferredDiffBaseCommit string
	PreferredDiffHeadRef    string
	PreferredDiffHeadCommit string
	AlternateDiffBaseRef    string
	AlternateDiffBaseCommit string
	AlternateDiffHeadRef    string
	AlternateDiffHeadCommit string
	DeltaBaseCommit         string
	InheritedCommitCount    *int
	TaskLocalCommitCount    *int
	Verdict                 string
	VerdictBy               string
	VerdictNotes            string
	RequestedAt             time.Time
	VerdictAt               *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type ReviewFinding struct {
	ID              int64
	ProjectID       string
	FindingKey      string
	TaskID          int64
	ReviewRoundID   int64
	RoundNumber     int
	FindingNumber   int
	CreatedBy       string
	Category        string
	Summary         string
	Notes           string
	FileReferences  []string
	TestCommands    []string
	Status          string
	StatusUpdatedBy string
	StatusNotes     string
	StatusUpdatedAt *time.Time
	ResponseBy      string
	ResponseNotes   string
	ResponseAt      *time.Time
	FollowUpTaskID  *int64
	RunID           string
	SubagentRole    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ReviewPacket struct {
	ID               int64
	ProjectID        string
	TaskID           int64
	ReviewRoundID    *int64
	PacketKind       string
	Sender           string
	MessageID        *int64
	FrontMatter      map[string]any
	TypedEnvelope    map[string]any
	MarkdownBody     string
	SourceMarkdown   string
	ValidationStatus string
	ValidationErrors []ValidationIssue
	IdempotencyKey   string
	CreatedAt        time.Time
	AcceptedAt       *time.Time
}

type ValidationIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
	DocsRef string `json:"docs_ref"`
}

type TaskContext struct {
	ID          int64  `json:"id"`
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    int    `json:"priority"`
}

type CreatedTask struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"project_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
}

type AppendedMessage struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"project_id"`
	TaskID    *int64 `json:"task_id,omitempty"`
	Intent    string `json:"intent"`
}

type GitHubCheckGate struct {
	ID                         int64
	ProjectID                  string
	TaskID                     int64
	Repository                 string
	CommitSHA                  string
	Ref                        string
	RequiredChecks             []string
	Status                     string
	RequestedBy                string
	AgentProfile               string
	AgentInstanceID            string
	SessionKey                 string
	TimeoutAt                  time.Time
	PollIntervalSeconds        int
	NextPollAt                 time.Time
	LastCheckedAt              *time.Time
	CompletedAt                *time.Time
	StatusURL                  string
	Summary                    string
	CheckRuns                  []GitHubCheckRun
	ObservedCheckRuns          []GitHubCheckRun
	MissingRequiredChecks      []string
	TerminalReason             string
	FailureSummary             string
	EvidenceMessageStatus      string
	EvidenceMessageID          *int64
	EvidenceMessageError       string
	EvidenceMessageAttemptedAt *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type GitHubCheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	URL        string `json:"url,omitempty"`
	DetailsURL string `json:"details_url,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

type GitHubCheckResult struct {
	Status                    string
	Summary                   string
	FailureSummary            string
	TerminalReason            string
	CheckRuns                 []GitHubCheckRun
	ObservedCheckRuns         []GitHubCheckRun
	MissingRequiredChecks     []string
	AllObservedChecksTerminal bool
}

// GitHubCheckGateTerminalEvent is the immutable machine wake fact emitted once
// for each gate that leaves pending. Its ID is the durable consumption cursor.
type GitHubCheckGateTerminalEvent struct {
	ID                    int64            `json:"id"`
	Schema                string           `json:"schema"`
	SchemaVersion         int              `json:"schema_version"`
	GateID                int64            `json:"gate_id"`
	ProjectID             string           `json:"project_id"`
	TaskID                int64            `json:"task_id"`
	Repository            string           `json:"repository"`
	CommitSHA             string           `json:"commit_sha"`
	Ref                   string           `json:"ref"`
	Status                string           `json:"status"`
	TerminalReason        string           `json:"terminal_reason"`
	RequiredChecks        []string         `json:"required_checks"`
	CheckRuns             []GitHubCheckRun `json:"check_runs"`
	ObservedCheckRuns     []GitHubCheckRun `json:"observed_check_runs"`
	MissingRequiredChecks []string         `json:"missing_required_checks"`
	Summary               string           `json:"summary,omitempty"`
	FailureSummary        string           `json:"failure_summary,omitempty"`
	RequestedBy           string           `json:"requested_by"`
	AgentProfile          string           `json:"agent_profile,omitempty"`
	AgentInstanceID       string           `json:"agent_instance_id,omitempty"`
	SessionKey            string           `json:"session_key,omitempty"`
	GateCreatedAt         time.Time        `json:"gate_created_at"`
	CompletedAt           time.Time        `json:"completed_at"`
	CreatedAt             time.Time        `json:"created_at"`
}

type ListGitHubCheckGateEventsQuery struct {
	ProjectID string
	TaskID    int64
	AfterID   int64
	Limit     int
}

type GitHubCheckGateEventPage struct {
	Events     []*GitHubCheckGateTerminalEvent `json:"events"`
	NextCursor int64                           `json:"next_cursor"`
	TimedOut   bool                            `json:"timed_out"`
}

func validGitHubCheckGateStatus(status string) bool {
	switch status {
	case GitHubCheckGateStatusPending, GitHubCheckGateStatusPassed, GitHubCheckGateStatusFailed,
		GitHubCheckGateStatusTimedOut, GitHubCheckGateStatusSuperseded:
		return true
	default:
		return false
	}
}

func terminalGitHubCheckGateStatus(status string) bool {
	switch status {
	case GitHubCheckGateStatusPassed, GitHubCheckGateStatusFailed, GitHubCheckGateStatusTimedOut, GitHubCheckGateStatusSuperseded:
		return true
	default:
		return false
	}
}

func validVerdict(verdict string) bool {
	switch verdict {
	case VerdictChangesRequested, VerdictLooksGood, VerdictFollowUpNeeded, VerdictBlockedByDependency:
		return true
	default:
		return false
	}
}

func validCategory(category string) bool {
	switch category {
	case CategoryBlockingBug, CategoryAcceptanceGap, CategoryTestWeakness, CategoryFollowUpCandidate:
		return true
	default:
		return false
	}
}

func validFindingStatus(status string) bool {
	switch status {
	case StatusOpen, StatusClaimedFixed, StatusVerifiedFixed, StatusNotFixed, StatusSuperseded, StatusSplitToFollowUp:
		return true
	default:
		return false
	}
}

func resolvedStatus(status string) bool {
	return status == StatusVerifiedFixed || status == StatusSuperseded || status == StatusSplitToFollowUp
}

func validPacketKind(kind string) bool {
	switch kind {
	case PacketKindReviewRequest, PacketKindRereviewRequest, PacketKindReviewFindings, PacketKindResponse, PacketKindCompletion:
		return true
	default:
		return false
	}
}

func trimSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
