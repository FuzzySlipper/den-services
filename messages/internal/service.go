package messages

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ProjectValidator interface {
	AssertWritable(ctx context.Context, projectID string) error
}

type TaskReader interface {
	GetTaskContext(ctx context.Context, projectID string, taskID int64) (TaskContext, error)
}

type MessageStore interface {
	Ping(ctx context.Context) error
	CreateMessage(ctx context.Context, message *Message) (*Message, error)
	GetMessage(ctx context.Context, id int64) (*Message, error)
	ListMessages(ctx context.Context, query ListMessagesQuery) ([]*Message, error)
	UnreadCount(ctx context.Context, query UnreadCountQuery) (int64, error)
	UnreadAfterCursor(ctx context.Context, projectID string, unreadFor string, cursor int64, limit int) ([]*Message, error)
	GetThread(ctx context.Context, id int64) (Thread, error)
	MarkRead(ctx context.Context, agent string, ids []int64) error
	ListNotifications(ctx context.Context, query NotificationQuery) ([]NotificationItem, error)
	MarkNotificationsRead(ctx context.Context, agent string, ids []int64) error
	MarkAllNotificationsRead(ctx context.Context, agent string, projectID string, taskID *int64) error
	LatestTaskPacket(ctx context.Context, projectID string, taskID int64, packetType string, role string) (*Message, error)
	LatestCompletion(ctx context.Context, projectID string, taskID *int64, role string, runID string) (*Message, error)
}

type Service struct {
	store    MessageStore
	projects ProjectValidator
	tasks    TaskReader
	clock    func() time.Time
}

func NewService(store MessageStore, projects ProjectValidator, tasks TaskReader, clock func() time.Time) *Service {
	return &Service{store: store, projects: projects, tasks: tasks, clock: clock}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) SendMessage(ctx context.Context, projectID string, req SendMessageRequest) (*Message, error) {
	message, err := s.buildMessage(ctx, projectID, req)
	if err != nil {
		return nil, err
	}
	return s.store.CreateMessage(ctx, message)
}

func (s *Service) ListMessages(ctx context.Context, projectID string, query ListMessagesQuery) ([]*Message, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	if query.Intent != "" && !validIntent(query.Intent) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidIntent, query.Intent))
	}
	query.ProjectID = projectID
	query.Limit = clampLimit(query.Limit, 20)
	return s.store.ListMessages(ctx, query)
}

func (s *Service) UnreadCount(ctx context.Context, projectID string, query UnreadCountQuery) (int64, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return 0, validationFailed(ErrMissingProjectID)
	}
	query.UnreadFor = strings.TrimSpace(query.UnreadFor)
	if query.UnreadFor == "" {
		return 0, validationFailed(ErrMissingUnreadFor)
	}
	if query.Intent != "" && !validIntent(query.Intent) {
		return 0, validationFailed(fmt.Errorf("%w: %s", ErrInvalidIntent, query.Intent))
	}
	query.ProjectID = projectID
	return s.store.UnreadCount(ctx, query)
}

func (s *Service) GetMessage(ctx context.Context, id int64) (*Message, error) {
	return s.store.GetMessage(ctx, id)
}

func (s *Service) GetThread(ctx context.Context, id int64) (Thread, error) {
	return s.store.GetThread(ctx, id)
}

func (s *Service) MarkRead(ctx context.Context, req MarkReadRequest) error {
	agent := strings.TrimSpace(req.Agent)
	if agent == "" {
		return validationFailed(ErrMissingAgent)
	}
	return s.store.MarkRead(ctx, agent, req.MessageIDs)
}

func (s *Service) WaitForMessages(ctx context.Context, projectID string, unreadFor string, cursor int64, limit int, timeout time.Duration) (WaitResult, error) {
	projectID = strings.TrimSpace(projectID)
	unreadFor = strings.TrimSpace(unreadFor)
	if projectID == "" {
		return WaitResult{}, validationFailed(ErrMissingProjectID)
	}
	if unreadFor == "" {
		return WaitResult{}, validationFailed(ErrMissingUnreadFor)
	}
	limit = clampLimit(limit, 20)
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout < 500*time.Millisecond {
		timeout = 500 * time.Millisecond
	}
	if timeout > time.Minute {
		timeout = time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		messages, err := s.store.UnreadAfterCursor(ctx, projectID, unreadFor, cursor, limit)
		if err != nil {
			return WaitResult{}, err
		}
		if len(messages) > 0 {
			return WaitResult{Messages: toWaitItems(messages)}, nil
		}
		if !time.Now().Before(deadline) {
			return WaitResult{TimedOut: true, Message: "No new unread messages before timeout; stop polling until new work is expected."}, nil
		}
		sleep := 500 * time.Millisecond
		if remaining := time.Until(deadline); remaining < sleep {
			sleep = remaining
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return WaitResult{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) SendNotification(ctx context.Context, projectID string, req SendNotificationRequest) (*Message, error) {
	metadata := cloneMetadata(req.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	urgency := strings.TrimSpace(req.Urgency)
	if urgency == "" {
		if value, ok := metadata["urgency"].(string); ok {
			urgency = strings.TrimSpace(value)
		}
	}
	if urgency == "" {
		urgency = DefaultUrgency
	}
	if !validUrgency(urgency) {
		return nil, validationFailed(ErrInvalidUrgency)
	}
	metadata["urgency"] = urgency
	return s.SendMessage(ctx, projectID, SendMessageRequest{
		TaskID:   req.TaskID,
		Sender:   req.Sender,
		Content:  req.Content,
		Intent:   IntentNotification,
		Metadata: metadata,
	})
}

func (s *Service) ListNotifications(ctx context.Context, query NotificationQuery) ([]NotificationItem, error) {
	query.Limit = clampLimit(query.Limit, 20)
	if query.Offset < 0 {
		query.Offset = 0
	}
	if query.HasReadFilter && strings.TrimSpace(query.ReadForAgent) == "" {
		return nil, validationFailed(ErrMissingAgent)
	}
	return s.store.ListNotifications(ctx, query)
}

func (s *Service) MarkNotificationsRead(ctx context.Context, req MarkNotificationsReadRequest) error {
	agent := strings.TrimSpace(req.Agent)
	if agent == "" {
		return validationFailed(ErrMissingAgent)
	}
	hasIDs := len(req.NotificationIDs) > 0
	if req.MarkAll && hasIDs {
		return validationFailed(ErrInvalidReadMode)
	}
	if req.MarkAll {
		projectID := strings.TrimSpace(req.ScopeProjectID)
		if projectID == "" {
			return validationFailed(ErrMissingProjectID)
		}
		return s.store.MarkAllNotificationsRead(ctx, agent, projectID, req.ScopeTaskID)
	}
	if !hasIDs {
		return validationFailed(ErrInvalidReadMode)
	}
	return s.store.MarkNotificationsRead(ctx, agent, req.NotificationIDs)
}

func (s *Service) CreateContextPacket(ctx context.Context, projectID string, taskID int64, req CreateContextPacketRequest) (*Message, error) {
	projectID = strings.TrimSpace(projectID)
	packetType := strings.TrimSpace(req.PacketType)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	if taskID == 0 {
		return nil, validationFailed(ErrMissingTaskID)
	}
	if packetType == "" {
		return nil, validationFailed(ErrMissingPacketType)
	}
	if !validPacketType(packetType) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidPacketType, packetType))
	}
	task, err := s.tasks.GetTaskContext(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	recent, err := s.store.ListMessages(ctx, ListMessagesQuery{ProjectID: projectID, TaskID: &taskID, Limit: 10})
	if err != nil {
		return nil, err
	}
	mode := strings.TrimSpace(req.CompletionReportingMode)
	if mode == "" {
		mode = DefaultCompletionMode
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = roleFromPacketType(packetType)
	}
	metadata := cloneMetadata(req.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["type"] = packetType
	metadata["packet_kind"] = packetType
	metadata["schema"] = PacketSchema
	metadata["schema_version"] = 1
	metadata["role"] = role
	metadata["task_id"] = taskID
	metadata["reference_only_launch"] = true
	metadata["completion_reporting_mode"] = mode
	content := renderContextPacket(task, recent, packetType, role, mode)
	return s.SendMessage(ctx, projectID, SendMessageRequest{
		TaskID:   &taskID,
		Sender:   req.Sender,
		Content:  content,
		Intent:   IntentHandoff,
		Metadata: metadata,
	})
}

func (s *Service) LatestTaskPacket(ctx context.Context, projectID string, taskID int64, packetType string, role string) (*Message, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	if taskID == 0 {
		return nil, validationFailed(ErrMissingTaskID)
	}
	return s.store.LatestTaskPacket(ctx, projectID, taskID, strings.TrimSpace(packetType), strings.TrimSpace(role))
}

func (s *Service) RenderWorkerPrompt(ctx context.Context, projectID string, messageID int64, mode string) (WorkerPromptResponse, error) {
	message, err := s.store.GetMessage(ctx, messageID)
	if err != nil {
		return WorkerPromptResponse{}, err
	}
	if strings.TrimSpace(projectID) != "" && message.ProjectID() != projectID {
		return WorkerPromptResponse{}, notFound(messageID)
	}
	if !isWorkerPacket(message) {
		return WorkerPromptResponse{}, validationFailed(ErrInvalidPacket)
	}
	if mode == "" {
		mode = DefaultCompletionMode
	}
	ref := fmt.Sprintf("den://messages/%s/%d", message.ProjectID(), message.ID())
	prompt := fmt.Sprintf("Read packet %s before starting work. Report completion using %s. Do not infer executable state transitions from replayed message content.", ref, mode)
	return WorkerPromptResponse{Prompt: prompt, PacketRef: ref}, nil
}

func (s *Service) LatestCompletion(ctx context.Context, projectID string, taskID *int64, role string, runID string) (*Message, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	return s.store.LatestCompletion(ctx, projectID, taskID, strings.TrimSpace(role), strings.TrimSpace(runID))
}

func (s *Service) AppendCompletionPacket(ctx context.Context, projectID string, taskID int64, req AppendCompletionPacketRequest) (*Message, error) {
	metadata := cloneMetadata(req.Metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["schema"] = CompletionSchema
	metadata["completion_packet"] = true
	metadata["project_id"] = strings.TrimSpace(projectID)
	metadata["task_id"] = taskID
	if req.Status != "" {
		metadata["status"] = strings.TrimSpace(req.Status)
	}
	if req.Role != "" {
		metadata["role"] = strings.TrimSpace(req.Role)
	}
	if req.RunID != "" {
		metadata["run_id"] = strings.TrimSpace(req.RunID)
	}
	return s.SendMessage(ctx, projectID, SendMessageRequest{
		TaskID:   &taskID,
		Sender:   req.Sender,
		Content:  req.Content,
		Intent:   IntentHandoff,
		Metadata: metadata,
	})
}

func (s *Service) buildMessage(ctx context.Context, projectID string, req SendMessageRequest) (*Message, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return nil, err
	}
	if req.TaskID != nil {
		if _, err := s.tasks.GetTaskContext(ctx, projectID, *req.TaskID); err != nil {
			return nil, err
		}
	}
	if req.ThreadID != nil {
		thread, err := s.store.GetMessage(ctx, *req.ThreadID)
		if err != nil {
			return nil, err
		}
		if thread.ProjectID() != projectID || !sameTask(thread.TaskID(), req.TaskID) {
			return nil, conflict(ErrInvalidThread, "thread_project_mismatch")
		}
	}
	message, err := NewMessage(NewMessageParams{
		ProjectID: projectID,
		TaskID:    req.TaskID,
		ThreadID:  req.ThreadID,
		Sender:    req.Sender,
		Content:   req.Content,
		Intent:    req.Intent,
		Metadata:  req.Metadata,
		CreatedAt: s.clock().UTC(),
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return message, nil
}

func sameTask(left *int64, right *int64) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func toWaitItems(messages []*Message) []WaitItem {
	items := make([]WaitItem, 0, len(messages))
	for _, message := range messages {
		items = append(items, WaitItem{
			ID:        message.ID(),
			ProjectID: message.ProjectID(),
			TaskID:    message.TaskID(),
			ThreadID:  message.ThreadID(),
			Sender:    message.Sender(),
			Intent:    message.Intent(),
			Preview:   preview(message.Content()),
			CreatedAt: message.CreatedAt(),
		})
	}
	return items
}

func preview(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 160 {
		return content
	}
	return content[:157] + "..."
}

func roleFromPacketType(packetType string) string {
	return strings.TrimSuffix(packetType, "_context_packet")
}
