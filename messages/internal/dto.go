package messages

import "time"

type SendMessageRequest struct {
	TaskID   *int64         `json:"task_id,omitempty"`
	ThreadID *int64         `json:"thread_id,omitempty"`
	Sender   string         `json:"sender"`
	Content  string         `json:"content"`
	Intent   string         `json:"intent,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type MarkReadRequest struct {
	Agent      string  `json:"agent"`
	MessageIDs []int64 `json:"message_ids,omitempty"`
}

type SendNotificationRequest struct {
	TaskID   *int64         `json:"task_id,omitempty"`
	Sender   string         `json:"sender"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Urgency  string         `json:"urgency,omitempty"`
}

type MarkNotificationsReadRequest struct {
	Agent           string  `json:"agent"`
	NotificationIDs []int64 `json:"notification_ids,omitempty"`
	MarkAll         bool    `json:"mark_all,omitempty"`
	ScopeProjectID  string  `json:"scope_project_id,omitempty"`
	ScopeTaskID     *int64  `json:"scope_task_id,omitempty"`
}

type CreateContextPacketRequest struct {
	PacketType              string         `json:"packet_type"`
	Role                    string         `json:"role"`
	Sender                  string         `json:"sender"`
	CompletionReportingMode string         `json:"completion_reporting_mode,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
}

type AppendCompletionPacketRequest struct {
	Sender   string         `json:"sender"`
	Content  string         `json:"content"`
	Status   string         `json:"status,omitempty"`
	Role     string         `json:"role,omitempty"`
	RunID    string         `json:"run_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type MessageResponse struct {
	ID        int64          `json:"id"`
	ProjectID string         `json:"project_id"`
	TaskID    *int64         `json:"task_id,omitempty"`
	ThreadID  *int64         `json:"thread_id,omitempty"`
	Sender    string         `json:"sender"`
	Content   string         `json:"content"`
	Intent    string         `json:"intent"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type ThreadResponse struct {
	Root    MessageResponse   `json:"root"`
	Replies []MessageResponse `json:"replies"`
}

type NotificationResponse struct {
	MessageResponse
	Urgency string `json:"urgency,omitempty"`
	IsRead  *bool  `json:"is_read,omitempty"`
}

type WaitForMessagesResponse struct {
	Messages []WaitItemResponse `json:"messages"`
	TimedOut bool               `json:"timed_out"`
	Message  string             `json:"message,omitempty"`
}

type WaitItemResponse struct {
	ID        int64     `json:"id"`
	ProjectID string    `json:"project_id"`
	TaskID    *int64    `json:"task_id,omitempty"`
	ThreadID  *int64    `json:"thread_id,omitempty"`
	Sender    string    `json:"sender"`
	Intent    string    `json:"intent"`
	Preview   string    `json:"preview"`
	CreatedAt time.Time `json:"created_at"`
}

type WorkerPromptResponse struct {
	Prompt    string `json:"prompt"`
	PacketRef string `json:"packet_ref"`
}

type SimpleMessageResponse struct {
	Message string `json:"message"`
}

func toMessageResponse(message *Message) MessageResponse {
	return MessageResponse{
		ID:        message.ID(),
		ProjectID: message.ProjectID(),
		TaskID:    message.TaskID(),
		ThreadID:  message.ThreadID(),
		Sender:    message.Sender(),
		Content:   message.Content(),
		Intent:    message.Intent(),
		Metadata:  message.Metadata(),
		CreatedAt: message.CreatedAt(),
	}
}

func toMessageResponses(messages []*Message) []MessageResponse {
	responses := make([]MessageResponse, 0, len(messages))
	for _, message := range messages {
		responses = append(responses, toMessageResponse(message))
	}
	return responses
}

func toThreadResponse(thread Thread) ThreadResponse {
	return ThreadResponse{Root: toMessageResponse(thread.Root), Replies: toMessageResponses(thread.Replies)}
}

func toNotificationResponse(item NotificationItem) NotificationResponse {
	return NotificationResponse{
		MessageResponse: toMessageResponse(item.Message),
		Urgency:         item.Urgency,
		IsRead:          item.IsRead,
	}
}

func toNotificationResponses(items []NotificationItem) []NotificationResponse {
	responses := make([]NotificationResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, toNotificationResponse(item))
	}
	return responses
}

func toWaitResponse(result WaitResult) WaitForMessagesResponse {
	response := WaitForMessagesResponse{TimedOut: result.TimedOut, Message: result.Message}
	response.Messages = make([]WaitItemResponse, 0, len(result.Messages))
	for _, item := range result.Messages {
		response.Messages = append(response.Messages, WaitItemResponse(item))
	}
	return response
}
