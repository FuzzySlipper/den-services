package messages

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	IntentGeneral        = "general"
	IntentNote           = "note"
	IntentStatusUpdate   = "status_update"
	IntentQuestion       = "question"
	IntentAnswer         = "answer"
	IntentHandoff        = "handoff"
	IntentReviewRequest  = "review_request"
	IntentReviewFeedback = "review_feedback"
	IntentReviewApproval = "review_approval"
	IntentTaskReady      = "task_ready"
	IntentTaskBlocked    = "task_blocked"
	IntentNotification   = "notification"

	PacketSchema          = "den_worker_packet"
	CompletionSchema      = "den_worker_completion"
	DefaultCompletionMode = "worker_mcp_tool"
	DefaultUrgency        = "normal"
)

var (
	ErrMessageNotFound    = errors.New("message not found")                       //nolint:gochecknoglobals
	ErrMissingProjectID   = errors.New("project_id is required")                  //nolint:gochecknoglobals
	ErrMissingSender      = errors.New("sender is required")                      //nolint:gochecknoglobals
	ErrMissingContent     = errors.New("content is required")                     //nolint:gochecknoglobals
	ErrInvalidIntent      = errors.New("invalid message intent")                  //nolint:gochecknoglobals
	ErrInvalidThread      = errors.New("thread must belong to the same project")  //nolint:gochecknoglobals
	ErrMissingAgent       = errors.New("agent is required")                       //nolint:gochecknoglobals
	ErrMissingTaskID      = errors.New("task_id is required")                     //nolint:gochecknoglobals
	ErrMissingPacketType  = errors.New("packet type is required")                 //nolint:gochecknoglobals
	ErrInvalidPacketType  = errors.New("invalid packet type")                     //nolint:gochecknoglobals
	ErrMissingUnreadFor   = errors.New("unread_for is required")                  //nolint:gochecknoglobals
	ErrProjectClientUnset = errors.New("projects scope client is not configured") //nolint:gochecknoglobals
	ErrTaskClientUnset    = errors.New("tasks scope client is not configured")    //nolint:gochecknoglobals
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

func notFound(id int64) error {
	return NewServiceError(fmt.Errorf("%w: %d", ErrMessageNotFound, id), "message_not_found", http.StatusNotFound)
}

func conflict(err error, code string) error {
	return NewServiceError(err, code, http.StatusConflict)
}

type Message struct {
	id        int64
	projectID string
	taskID    *int64
	threadID  *int64
	sender    string
	content   string
	intent    string
	metadata  map[string]any
	createdAt time.Time
}

type NewMessageParams struct {
	ID        int64
	ProjectID string
	TaskID    *int64
	ThreadID  *int64
	Sender    string
	Content   string
	Intent    string
	Metadata  map[string]any
	CreatedAt time.Time
}

func NewMessage(params NewMessageParams) (*Message, error) {
	projectID := strings.TrimSpace(params.ProjectID)
	if projectID == "" {
		return nil, ErrMissingProjectID
	}
	sender := strings.TrimSpace(params.Sender)
	if sender == "" {
		return nil, ErrMissingSender
	}
	content := strings.TrimSpace(params.Content)
	if content == "" {
		return nil, ErrMissingContent
	}
	intent := normalizeIntent(params.Intent, params.Metadata)
	if !validIntent(intent) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidIntent, intent)
	}
	createdAt := params.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return &Message{
		id:        params.ID,
		projectID: projectID,
		taskID:    cloneInt64(params.TaskID),
		threadID:  cloneInt64(params.ThreadID),
		sender:    sender,
		content:   content,
		intent:    intent,
		metadata:  cloneMetadata(params.Metadata),
		createdAt: createdAt,
	}, nil
}

func (m *Message) ID() int64                { return m.id }
func (m *Message) ProjectID() string        { return m.projectID }
func (m *Message) TaskID() *int64           { return cloneInt64(m.taskID) }
func (m *Message) ThreadID() *int64         { return cloneInt64(m.threadID) }
func (m *Message) Sender() string           { return m.sender }
func (m *Message) Content() string          { return m.content }
func (m *Message) Intent() string           { return m.intent }
func (m *Message) Metadata() map[string]any { return cloneMetadata(m.metadata) }
func (m *Message) CreatedAt() time.Time     { return m.createdAt }

type ListMessagesQuery struct {
	ProjectID string
	TaskID    *int64
	Since     *time.Time
	UnreadFor string
	Intent    string
	Limit     int
}

type NotificationQuery struct {
	ProjectID     string
	TaskID        *int64
	Sender        string
	MetadataType  string
	Urgency       string
	ReadForAgent  string
	HasReadFilter bool
	IsRead        bool
	Limit         int
	Offset        int
}

type NotificationItem struct {
	Message *Message
	IsRead  *bool
	Urgency string
}

type Thread struct {
	Root    *Message
	Replies []*Message
}

type WaitResult struct {
	Messages []WaitItem
	TimedOut bool
	Message  string
}

type WaitItem struct {
	ID        int64
	ProjectID string
	TaskID    *int64
	ThreadID  *int64
	Sender    string
	Intent    string
	Preview   string
	CreatedAt time.Time
}

type TaskContext struct {
	ID          int64
	ProjectID   string
	Title       string
	Description string
	Status      string
	Priority    int
}

func validIntent(intent string) bool {
	switch intent {
	case IntentGeneral, IntentNote, IntentStatusUpdate, IntentQuestion, IntentAnswer,
		IntentHandoff, IntentReviewRequest, IntentReviewFeedback, IntentReviewApproval,
		IntentTaskReady, IntentTaskBlocked, IntentNotification:
		return true
	default:
		return false
	}
}

func normalizeIntent(intent string, metadata map[string]any) string {
	intent = strings.TrimSpace(intent)
	if intent != "" {
		return intent
	}
	rawType, _ := metadata["type"].(string)
	switch strings.TrimSpace(rawType) {
	case "note":
		return IntentNote
	case "status_update", "merge_complete":
		return IntentStatusUpdate
	case "question":
		return IntentQuestion
	case "answer":
		return IntentAnswer
	case "handoff", "planning", "planning_summary":
		return IntentHandoff
	case "review_request", "review_request_packet", "rereview_packet":
		return IntentReviewRequest
	case "review_feedback", "review_findings_packet":
		return IntentReviewFeedback
	case "review_approval", "merge_request":
		return IntentReviewApproval
	case "task_ready":
		return IntentTaskReady
	case "task_blocked":
		return IntentTaskBlocked
	case "notification":
		return IntentNotification
	default:
		return IntentGeneral
	}
}

func validPacketType(packetType string) bool {
	switch packetType {
	case "coder_context_packet", "reviewer_context_packet", "validator_context_packet",
		"drift_checker_context_packet", "packet_auditor_context_packet", "scope_auditor_context_packet":
		return true
	default:
		return false
	}
}

func clampLimit(limit int, fallback int) int {
	if limit <= 0 {
		limit = fallback
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
