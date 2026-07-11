package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type NoopProjectValidator struct{}

func (NoopProjectValidator) AssertWritable(context.Context, string) error { return nil }

type ProjectScopeClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewProjectScopeClient(baseURL string, token string) *ProjectScopeClient {
	return &ProjectScopeClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *ProjectScopeClient) AssertWritable(ctx context.Context, projectID string) error {
	if c.baseURL == "" {
		return NewServiceError(ErrProjectScopeClientUnset, "project_scope_client_unconfigured", http.StatusInternalServerError)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/scopes/"+url.PathEscape(projectID)+"/assert-writable", bytes.NewBufferString(`{}`))
	if err != nil {
		return fmt.Errorf("building project assert-writable request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("asserting project scope writable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusConflict {
		return conflict(fmt.Errorf("project scope is not writable: %s", errorMessage(resp)), "project_scope_not_writable")
	}
	if resp.StatusCode == http.StatusNotFound {
		return validationError(fmt.Errorf("project scope not found: %s", projectID), "project_not_found", "project_id", "common.project_id")
	}
	return fmt.Errorf("project scope writable check failed: %s", errorMessage(resp))
}

type HTTPTaskClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewTaskClient(baseURL string, token string) *HTTPTaskClient {
	return &HTTPTaskClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *HTTPTaskClient) GetTaskContext(ctx context.Context, projectID string, taskID int64) (TaskContext, error) {
	if c.baseURL == "" {
		return TaskContext{}, NewServiceError(ErrTaskClientUnset, "task_client_unconfigured", http.StatusInternalServerError)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/projects/%s/tasks/%d", c.baseURL, url.PathEscape(projectID), taskID), nil)
	if err != nil {
		return TaskContext{}, fmt.Errorf("building task context request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return TaskContext{}, fmt.Errorf("getting task context: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return TaskContext{}, validationError(fmt.Errorf("task not found: %d", taskID), "task_not_found", "task_id", "common.task_id")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TaskContext{}, fmt.Errorf("task context lookup failed: %s", errorMessage(resp))
	}
	task, err := decodeTaskContext(resp.Body)
	if err != nil {
		return TaskContext{}, err
	}
	if task.ProjectID != projectID {
		return TaskContext{}, validationError(fmt.Errorf("task %d is not in project %s", taskID, projectID), "project_mismatch", "task_id", "common.task_id")
	}
	return task, nil
}

func (c *HTTPTaskClient) GetTask(ctx context.Context, taskID int64) (TaskContext, error) {
	if c.baseURL == "" {
		return TaskContext{}, NewServiceError(ErrTaskClientUnset, "task_client_unconfigured", http.StatusInternalServerError)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v1/tasks/%d", c.baseURL, taskID), nil)
	if err != nil {
		return TaskContext{}, fmt.Errorf("building task lookup request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return TaskContext{}, fmt.Errorf("getting task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return TaskContext{}, validationError(fmt.Errorf("task not found: %d", taskID), "task_not_found", "task_id", "common.task_id")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TaskContext{}, fmt.Errorf("task lookup failed: %s", errorMessage(resp))
	}
	task, err := decodeTaskContext(resp.Body)
	if err != nil {
		return TaskContext{}, err
	}
	return task, nil
}

func (c *HTTPTaskClient) SetTaskStatus(ctx context.Context, projectID string, taskID int64, agent string, status string) (TaskContext, error) {
	if c.baseURL == "" {
		return TaskContext{}, NewServiceError(ErrTaskClientUnset, "task_client_unconfigured", http.StatusInternalServerError)
	}
	body := struct {
		Agent  string `json:"agent"`
		Status string `json:"status"`
	}{Agent: strings.TrimSpace(agent), Status: strings.TrimSpace(status)}
	data, err := json.Marshal(body)
	if err != nil {
		return TaskContext{}, fmt.Errorf("encoding task status update: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("%s/v1/projects/%s/tasks/%d", c.baseURL, url.PathEscape(projectID), taskID), bytes.NewReader(data))
	if err != nil {
		return TaskContext{}, fmt.Errorf("building task status update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return TaskContext{}, fmt.Errorf("updating task status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TaskContext{}, fmt.Errorf("task status update failed: %s", errorMessage(resp))
	}
	task, err := decodeTaskContext(resp.Body)
	if err != nil {
		return TaskContext{}, err
	}
	if task.ProjectID != projectID {
		return TaskContext{}, validationError(fmt.Errorf("task %d is not in project %s", taskID, projectID), "project_mismatch", "task_id", "common.task_id")
	}
	return task, nil
}

func (c *HTTPTaskClient) CreateFollowUpTask(ctx context.Context, projectID string, req CreateFollowUpTaskRequest) (CreatedTask, error) {
	if c.baseURL == "" {
		return CreatedTask{}, NewServiceError(ErrTaskClientUnset, "task_client_unconfigured", http.StatusInternalServerError)
	}
	body := map[string]any{"title": req.Title, "description": req.Description, "priority": req.Priority, "assigned_to": req.AssignedTo, "tags": req.Tags, "parent_id": req.ParentID}
	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/projects/"+url.PathEscape(projectID)+"/tasks", bytes.NewReader(data))
	if err != nil {
		return CreatedTask{}, fmt.Errorf("building follow-up task request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.IdempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return CreatedTask{}, fmt.Errorf("creating follow-up task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CreatedTask{}, fmt.Errorf("creating follow-up task failed: %s", errorMessage(resp))
	}
	var task CreatedTask
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return CreatedTask{}, fmt.Errorf("decoding follow-up task: %w", err)
	}
	return task, nil
}

func decodeTaskContext(body io.Reader) (TaskContext, error) {
	var envelope struct {
		Task TaskContext `json:"task"`
		TaskContext
	}
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return TaskContext{}, fmt.Errorf("decoding task context: %w", err)
	}
	task := envelope.Task
	if task.ID == 0 {
		task = envelope.TaskContext
	}
	return task, nil
}

type HTTPMessageClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewMessageClient(baseURL string, token string) *HTTPMessageClient {
	return &HTTPMessageClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *HTTPMessageClient) AppendTaskMessage(ctx context.Context, projectID string, req AppendMessageRequest) (AppendedMessage, error) {
	if c.baseURL == "" {
		return AppendedMessage{}, NewServiceError(ErrMessageClientUnset, "message_client_unconfigured", http.StatusInternalServerError)
	}
	taskID := req.TaskID
	body := map[string]any{"task_id": taskID, "thread_id": req.ThreadID, "sender": req.Sender, "content": req.Content, "intent": req.Intent, "metadata": req.Metadata}
	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/projects/"+url.PathEscape(projectID)+"/messages", bytes.NewReader(data))
	if err != nil {
		return AppendedMessage{}, fmt.Errorf("building message append request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return AppendedMessage{}, fmt.Errorf("appending review packet message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AppendedMessage{}, fmt.Errorf("appending review packet message failed: %s", errorMessage(resp))
	}
	var message AppendedMessage
	if err := json.NewDecoder(resp.Body).Decode(&message); err != nil {
		return AppendedMessage{}, fmt.Errorf("decoding appended message: %w", err)
	}
	return message, nil
}

func errorMessage(resp *http.Response) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&envelope)
	if strings.TrimSpace(envelope.Error.Message) != "" {
		return strings.TrimSpace(envelope.Error.Message)
	}
	return resp.Status
}
