package tasks

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"den-services/shared/api"
)

type TaskUseCases interface {
	CreateTask(ctx context.Context, projectID string, req CreateTaskRequest) (*Task, error)
	GetTask(ctx context.Context, taskID int64) (TaskDetail, error)
	ListTasks(ctx context.Context, projectID string, query ListTasksQuery) ([]TaskSummary, error)
	UpdateTask(ctx context.Context, taskID int64, req UpdateTaskRequest) (*Task, error)
	AddDependency(ctx context.Context, taskID int64, dependsOn int64) error
	RemoveDependency(ctx context.Context, taskID int64, dependsOn int64) error
	NextTask(ctx context.Context, projectID string, assignedTo string) (*Task, error)
	History(ctx context.Context, taskID int64) ([]TaskHistoryEntry, error)
	ListTaskChanges(ctx context.Context, projectID string, afterID int64, limit int) ([]TaskChangeEvent, error)
}

type Handler struct {
	service TaskUseCases
	config  *Config
}

func NewHandler(service TaskUseCases, config *Config) *Handler {
	return &Handler{service: service, config: config}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks", h.createTask)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks", h.listTasks)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/changes", h.listTaskChanges)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/changes/stream", h.taskChangesStream)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/next", h.nextTask)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/{task_id}", h.getProjectTask)
	mux.HandleFunc("PATCH /v1/projects/{project_id}/tasks/{task_id}", h.updateProjectTask)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/dependencies", h.addProjectDependency)
	mux.HandleFunc("DELETE /v1/projects/{project_id}/tasks/{task_id}/dependencies/{depends_on}", h.removeProjectDependency)

	mux.HandleFunc("GET /v1/tasks/{task_id}", h.getTask)
	mux.HandleFunc("PATCH /v1/tasks/{task_id}", h.updateTask)
	mux.HandleFunc("POST /v1/tasks/{task_id}/dependencies", h.addDependency)
	mux.HandleFunc("DELETE /v1/tasks/{task_id}/dependencies/{depends_on}", h.removeDependency)
	mux.HandleFunc("GET /v1/tasks/{task_id}/history", h.history)
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	task, err := h.service.CreateTask(r.Context(), r.PathValue("project_id"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toTaskResponse(task))
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	query, err := listQueryFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	tasks, err := h.service.ListTasks(r.Context(), r.PathValue("project_id"), query)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toTaskSummaryResponses(tasks))
}

func (h *Handler) nextTask(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.NextTask(r.Context(), r.PathValue("project_id"), r.URL.Query().Get("assigned_to"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if task == nil {
		api.WriteJSON(w, http.StatusOK, MessageResponse{Message: "No unblocked tasks available."})
		return
	}
	api.WriteJSON(w, http.StatusOK, toTaskResponse(task))
}

func (h *Handler) getProjectTask(w http.ResponseWriter, r *http.Request) {
	h.writeTaskDetail(w, r, true)
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request) {
	h.writeTaskDetail(w, r, false)
}

func (h *Handler) writeTaskDetail(w http.ResponseWriter, r *http.Request, checkProject bool) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	detail, err := h.service.GetTask(r.Context(), taskID)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if checkProject && detail.Task.ProjectID() != r.PathValue("project_id") {
		api.WriteServiceError(w, notFound(taskID))
		return
	}
	api.WriteJSON(w, http.StatusOK, toTaskDetailResponse(detail))
}

func (h *Handler) updateProjectTask(w http.ResponseWriter, r *http.Request) {
	h.updateTaskWithProjectCheck(w, r, true)
}

func (h *Handler) updateTask(w http.ResponseWriter, r *http.Request) {
	h.updateTaskWithProjectCheck(w, r, false)
}

func (h *Handler) updateTaskWithProjectCheck(w http.ResponseWriter, r *http.Request, checkProject bool) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if checkProject {
		detail, err := h.service.GetTask(r.Context(), taskID)
		if err != nil {
			api.WriteServiceError(w, err)
			return
		}
		if detail.Task.ProjectID() != r.PathValue("project_id") {
			api.WriteServiceError(w, notFound(taskID))
			return
		}
	}
	var req UpdateTaskRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	task, err := h.service.UpdateTask(r.Context(), taskID, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toTaskResponse(task))
}

func (h *Handler) addProjectDependency(w http.ResponseWriter, r *http.Request) {
	h.addDependencyWithProjectCheck(w, r, true)
}

func (h *Handler) addDependency(w http.ResponseWriter, r *http.Request) {
	h.addDependencyWithProjectCheck(w, r, false)
}

func (h *Handler) addDependencyWithProjectCheck(w http.ResponseWriter, r *http.Request, checkProject bool) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if checkProject && !h.projectMatches(w, r, taskID) {
		return
	}
	var req AddDependencyRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.AddDependency(r.Context(), taskID, req.DependsOn); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, MessageResponse{Message: "Task dependency added."})
}

func (h *Handler) removeProjectDependency(w http.ResponseWriter, r *http.Request) {
	h.removeDependencyWithProjectCheck(w, r, true)
}

func (h *Handler) removeDependency(w http.ResponseWriter, r *http.Request) {
	h.removeDependencyWithProjectCheck(w, r, false)
}

func (h *Handler) removeDependencyWithProjectCheck(w http.ResponseWriter, r *http.Request, checkProject bool) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if checkProject && !h.projectMatches(w, r, taskID) {
		return
	}
	dependsOn, err := pathInt64(r, "depends_on")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.RemoveDependency(r.Context(), taskID, dependsOn); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, MessageResponse{Message: "Task dependency removed."})
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathInt64(r, "task_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	entries, err := h.service.History(r.Context(), taskID)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toHistoryResponses(entries))
}

func (h *Handler) listTaskChanges(w http.ResponseWriter, r *http.Request) {
	afterID, err := h.parseAfter(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	limit, err := h.parseChangeLimit(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	events, err := h.service.ListTaskChanges(r.Context(), r.PathValue("project_id"), afterID, limit)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	responses := toTaskChangeResponses(events)
	api.WriteJSON(w, http.StatusOK, TaskChangesResponse{
		Events:     responses,
		NextCursor: nextTaskChangeCursor(responses),
	})
}

func (h *Handler) taskChangesStream(w http.ResponseWriter, r *http.Request) {
	afterID, err := h.parseAfter(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	limit, err := h.parseChangeLimit(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	streamTaskChanges(w, r, h.service, h.config, taskChangeStreamQuery{
		ProjectID: r.PathValue("project_id"),
		AfterID:   afterID,
		Limit:     limit,
	})
}

func (h *Handler) projectMatches(w http.ResponseWriter, r *http.Request, taskID int64) bool {
	detail, err := h.service.GetTask(r.Context(), taskID)
	if err != nil {
		api.WriteServiceError(w, err)
		return false
	}
	if detail.Task.ProjectID() != r.PathValue("project_id") {
		api.WriteServiceError(w, notFound(taskID))
		return false
	}
	return true
}

func listQueryFromRequest(r *http.Request) (ListTasksQuery, error) {
	query := r.URL.Query()
	var result ListTasksQuery
	result.Statuses = splitCSV(query.Get("status"))
	result.AssignedTo = strings.TrimSpace(query.Get("assigned_to"))
	result.Tags = splitCSV(query.Get("tags"))
	result.IncludeAll = query.Get("tree") == "true"
	if raw := strings.TrimSpace(query.Get("priority")); raw != "" {
		priority, err := strconv.Atoi(raw)
		if err != nil {
			return ListTasksQuery{}, badRequest(err)
		}
		result.MaxPriority = &priority
	}
	if raw := strings.TrimSpace(query.Get("parent_id")); raw != "" {
		parentID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return ListTasksQuery{}, badRequest(err)
		}
		result.ParentID = &parentID
	}
	return result, nil
}

func (h *Handler) parseAfter(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("after"))
	if raw == "" {
		raw = strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	}
	if raw == "" {
		return 0, nil
	}
	afterID, err := parseTaskChangeCursor(raw)
	if err != nil {
		return 0, badRequest(err)
	}
	return afterID, nil
}

func (h *Handler) parseChangeLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return h.config.Stream.DefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, badRequest(err)
	}
	if limit <= 0 || limit > h.config.Stream.MaxLimit {
		return 0, badRequest(ErrInvalidTask)
	}
	return limit, nil
}

func nextTaskChangeCursor(events []TaskChangeEventResponse) string {
	if len(events) == 0 {
		return ""
	}
	return events[len(events)-1].Cursor
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func pathInt64(r *http.Request, key string) (int64, error) {
	value, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || value <= 0 {
		if err == nil {
			err = ErrInvalidTask
		}
		return 0, badRequest(err)
	}
	return value, nil
}
