package messages

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"den-services/shared/api"
)

type MessageUseCases interface {
	SendMessage(ctx context.Context, projectID string, req SendMessageRequest) (*Message, error)
	ListMessages(ctx context.Context, projectID string, query ListMessagesQuery) ([]*Message, error)
	UnreadCount(ctx context.Context, projectID string, query UnreadCountQuery) (int64, error)
	GetMessage(ctx context.Context, id int64) (*Message, error)
	GetThread(ctx context.Context, id int64) (Thread, error)
	MarkRead(ctx context.Context, req MarkReadRequest) error
	WaitForMessages(ctx context.Context, projectID string, unreadFor string, cursor int64, limit int, timeout time.Duration) (WaitResult, error)
	SendNotification(ctx context.Context, projectID string, req SendNotificationRequest) (*Message, error)
	ListNotifications(ctx context.Context, query NotificationQuery) ([]NotificationItem, error)
	MarkNotificationsRead(ctx context.Context, req MarkNotificationsReadRequest) error
	MarkProjectNotificationsRead(ctx context.Context, agent string, projectID string) error
	MarkTaskNotificationsRead(ctx context.Context, agent string, projectID string, taskID int64) error
	CreateContextPacket(ctx context.Context, projectID string, taskID int64, req CreateContextPacketRequest) (*Message, error)
	LatestTaskPacket(ctx context.Context, projectID string, taskID int64, packetType string, role string) (*Message, error)
	RenderWorkerPrompt(ctx context.Context, projectID string, messageID int64, mode string) (WorkerPromptResponse, error)
	LatestCompletion(ctx context.Context, projectID string, taskID *int64, role string, runID string) (*Message, error)
	AppendCompletionPacket(ctx context.Context, projectID string, taskID int64, req AppendCompletionPacketRequest) (*Message, error)
}

type Handler struct {
	service MessageUseCases
}

func NewHandler(service MessageUseCases) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects/{project_id}/messages", h.sendMessage)
	mux.HandleFunc("GET /v1/projects/{project_id}/messages", h.listMessages)
	mux.HandleFunc("GET /v1/projects/{project_id}/messages/unread-count", h.unreadCount)
	mux.HandleFunc("GET /v1/projects/{project_id}/messages/wait", h.waitForMessages)
	mux.HandleFunc("GET /v1/projects/{project_id}/messages/threads/{thread_id}", h.getThread)
	mux.HandleFunc("GET /v1/projects/{project_id}/packets/{message_id}/worker-prompt", h.renderWorkerPrompt)
	mux.HandleFunc("POST /v1/projects/{project_id}/notifications", h.sendNotification)
	mux.HandleFunc("GET /v1/projects/{project_id}/user-notifications", h.listProjectNotifications)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/packets/context", h.createContextPacket)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/{task_id}/packets/latest", h.latestTaskPacket)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/completions", h.appendCompletionPacket)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/{task_id}/completions/latest", h.latestProjectTaskCompletion)

	mux.HandleFunc("GET /v1/messages/{message_id}", h.getMessage)
	mux.HandleFunc("GET /v1/messages/{message_id}/thread", h.getThreadByMessage)
	mux.HandleFunc("GET /v1/threads/{thread_id}", h.getThread)
	mux.HandleFunc("POST /v1/messages/read", h.markRead)
	mux.HandleFunc("GET /v1/user-notifications", h.listNotifications)
	mux.HandleFunc("POST /v1/user-notifications/read", h.markNotificationsRead)
	mux.HandleFunc("POST /v1/projects/{project_id}/user-notifications/read", h.markProjectNotificationsRead)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/user-notifications/read", h.markTaskNotificationsRead)
}

func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req SendMessageRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	message, err := h.service.SendMessage(r.Context(), r.PathValue("project_id"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toMessageResponse(message))
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) {
	query, err := listQueryFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	messages, err := h.service.ListMessages(r.Context(), r.PathValue("project_id"), query)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toMessageResponses(messages))
}

func (h *Handler) unreadCount(w http.ResponseWriter, r *http.Request) {
	query, err := unreadCountQueryFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	count, err := h.service.UnreadCount(r.Context(), r.PathValue("project_id"), query)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, UnreadCountResponse{UnreadMessageCount: count})
}

func (h *Handler) getMessage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "message_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	message, err := h.service.GetMessage(r.Context(), id)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toMessageResponse(message))
}

func (h *Handler) getThreadByMessage(w http.ResponseWriter, r *http.Request) {
	h.getThread(w, r)
}

func (h *Handler) getThread(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("thread_id")
	if rawID == "" {
		rawID = r.PathValue("message_id")
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	thread, err := h.service.GetThread(r.Context(), id)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if projectID := r.PathValue("project_id"); projectID != "" && thread.Root.ProjectID() != projectID {
		api.WriteServiceError(w, notFound(id))
		return
	}
	api.WriteJSON(w, http.StatusOK, toThreadResponse(thread))
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	var req MarkReadRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.MarkRead(r.Context(), req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, SimpleMessageResponse{Message: "Messages marked read."})
}

func (h *Handler) waitForMessages(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	cursor, err := optionalInt64(query.Get("cursor"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	limit, err := optionalInt(query.Get("limit"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	timeoutMs, err := optionalInt(query.Get("timeout_ms"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	result, err := h.service.WaitForMessages(r.Context(), r.PathValue("project_id"), query.Get("unread_for"), cursor, limit, time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toWaitResponse(result))
}

func (h *Handler) sendNotification(w http.ResponseWriter, r *http.Request) {
	var req SendNotificationRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	message, err := h.service.SendNotification(r.Context(), r.PathValue("project_id"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toMessageResponse(message))
}

func (h *Handler) listProjectNotifications(w http.ResponseWriter, r *http.Request) {
	query, err := notificationQueryFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	query.ProjectID = r.PathValue("project_id")
	h.writeNotifications(w, r, query)
}

func (h *Handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	query, err := notificationQueryFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	h.writeNotifications(w, r, query)
}

func (h *Handler) writeNotifications(w http.ResponseWriter, r *http.Request, query NotificationQuery) {
	items, err := h.service.ListNotifications(r.Context(), query)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toNotificationResponses(items))
}

func (h *Handler) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var req MarkNotificationsReadRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.MarkNotificationsRead(r.Context(), req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, SimpleMessageResponse{Message: "Notifications marked read."})
}

func (h *Handler) markProjectNotificationsRead(w http.ResponseWriter, r *http.Request) {
	var req MarkScopedNotificationsReadRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.MarkProjectNotificationsRead(r.Context(), req.Agent, r.PathValue("project_id")); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, SimpleMessageResponse{Message: "Project notifications marked read."})
}

func (h *Handler) markTaskNotificationsRead(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	var req MarkScopedNotificationsReadRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.MarkTaskNotificationsRead(r.Context(), req.Agent, r.PathValue("project_id"), taskID); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, SimpleMessageResponse{Message: "Task notifications marked read."})
}

func (h *Handler) createContextPacket(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	var req CreateContextPacketRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	message, err := h.service.CreateContextPacket(r.Context(), r.PathValue("project_id"), taskID, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toMessageResponse(message))
}

func (h *Handler) latestTaskPacket(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	message, err := h.service.LatestTaskPacket(r.Context(), r.PathValue("project_id"), taskID, r.URL.Query().Get("packet_type"), r.URL.Query().Get("role"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toMessageResponse(message))
}

func (h *Handler) renderWorkerPrompt(w http.ResponseWriter, r *http.Request) {
	messageID, err := pathInt64(r, "message_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	response, err := h.service.RenderWorkerPrompt(r.Context(), r.PathValue("project_id"), messageID, r.URL.Query().Get("completion_reporting_mode"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) latestProjectTaskCompletion(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	message, err := h.service.LatestCompletion(r.Context(), r.PathValue("project_id"), &taskID, r.URL.Query().Get("role"), r.URL.Query().Get("run_id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toMessageResponse(message))
}

func (h *Handler) appendCompletionPacket(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	var req AppendCompletionPacketRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	message, err := h.service.AppendCompletionPacket(r.Context(), r.PathValue("project_id"), taskID, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toMessageResponse(message))
}

func listQueryFromRequest(r *http.Request) (ListMessagesQuery, error) {
	query := r.URL.Query()
	limit, err := optionalInt(query.Get("limit"))
	if err != nil {
		return ListMessagesQuery{}, err
	}
	taskID, err := optionalInt64(query.Get("task_id"))
	if err != nil {
		return ListMessagesQuery{}, err
	}
	var since *time.Time
	if raw := strings.TrimSpace(query.Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return ListMessagesQuery{}, badRequest(err)
		}
		since = &parsed
	}
	return ListMessagesQuery{TaskID: int64Ptr(taskID), Since: since, UnreadFor: strings.TrimSpace(query.Get("unread_for")), Intent: strings.TrimSpace(query.Get("intent")), Limit: limit}, nil
}

func unreadCountQueryFromRequest(r *http.Request) (UnreadCountQuery, error) {
	query := r.URL.Query()
	taskID, err := optionalInt64(query.Get("task_id"))
	if err != nil {
		return UnreadCountQuery{}, err
	}
	afterCursor, err := optionalInt64(query.Get("after_cursor"))
	if err != nil {
		return UnreadCountQuery{}, err
	}
	return UnreadCountQuery{
		TaskID:      int64Ptr(taskID),
		UnreadFor:   strings.TrimSpace(query.Get("unread_for")),
		Intent:      strings.TrimSpace(query.Get("intent")),
		AfterCursor: int64Ptr(afterCursor),
	}, nil
}

func notificationQueryFromRequest(r *http.Request) (NotificationQuery, error) {
	query := r.URL.Query()
	limit, err := optionalInt(query.Get("limit"))
	if err != nil {
		return NotificationQuery{}, err
	}
	offset, err := optionalInt(query.Get("offset"))
	if err != nil {
		return NotificationQuery{}, err
	}
	taskID, err := optionalInt64(query.Get("task_id"))
	if err != nil {
		return NotificationQuery{}, err
	}
	result := NotificationQuery{
		TaskID:       int64Ptr(taskID),
		Sender:       strings.TrimSpace(query.Get("sender")),
		MetadataType: strings.TrimSpace(query.Get("metadata_type")),
		Urgency:      strings.TrimSpace(query.Get("urgency")),
		ReadForAgent: strings.TrimSpace(query.Get("read_for_agent")),
		Limit:        limit,
		Offset:       offset,
	}
	if raw := strings.TrimSpace(query.Get("is_read")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return NotificationQuery{}, badRequest(err)
		}
		result.HasReadFilter = true
		result.IsRead = parsed
	}
	return result, nil
}

func pathInt64(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest(err)
	}
	return id, nil
}

func optionalInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, badRequest(err)
	}
	return parsed, nil
}

func optionalInt64(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, badRequest(err)
	}
	return parsed, nil
}

func int64Ptr(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}
