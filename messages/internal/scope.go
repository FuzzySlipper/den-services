package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type NoopProjectValidator struct{}

func (NoopProjectValidator) AssertWritable(context.Context, string) error { return nil }

type NoopTaskReader struct {
	Tasks map[int64]TaskContext
}

func (r NoopTaskReader) GetTaskContext(_ context.Context, projectID string, taskID int64) (TaskContext, error) {
	if r.Tasks != nil {
		if task, ok := r.Tasks[taskID]; ok {
			if task.ProjectID == "" {
				task.ProjectID = projectID
			}
			return task, nil
		}
	}
	return TaskContext{ID: taskID, ProjectID: projectID, Title: fmt.Sprintf("Task %d", taskID), Status: "planned", Priority: 3}, nil
}

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
		return NewServiceError(ErrProjectClientUnset, "project_scope_client_unconfigured", http.StatusInternalServerError)
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
	message := errorMessage(resp)
	if resp.StatusCode == http.StatusConflict {
		return conflict(fmt.Errorf("project scope is not writable: %s", message), "project_scope_not_writable")
	}
	if resp.StatusCode == http.StatusNotFound {
		return validationFailed(fmt.Errorf("project scope not found: %s", projectID))
	}
	return fmt.Errorf("project scope writable check failed: %s", message)
}

type TaskClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewTaskClient(baseURL string, token string) *TaskClient {
	return &TaskClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), token: strings.TrimSpace(token), client: &http.Client{Timeout: 5 * time.Second}}
}

func (c *TaskClient) GetTaskContext(ctx context.Context, projectID string, taskID int64) (TaskContext, error) {
	if c.baseURL == "" {
		return TaskContext{}, NewServiceError(ErrTaskClientUnset, "task_scope_client_unconfigured", http.StatusInternalServerError)
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
		return TaskContext{}, validationFailed(fmt.Errorf("task not found: %d", taskID))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TaskContext{}, fmt.Errorf("task context lookup failed: %s", errorMessage(resp))
	}
	var envelope struct {
		Task struct {
			ID          int64  `json:"id"`
			ProjectID   string `json:"project_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Status      string `json:"status"`
			Priority    int    `json:"priority"`
		} `json:"task"`
		ID          int64  `json:"id"`
		ProjectID   string `json:"project_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
		Priority    int    `json:"priority"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return TaskContext{}, fmt.Errorf("decoding task context: %w", err)
	}
	task := envelope.Task
	if task.ID == 0 {
		task.ID = envelope.ID
		task.ProjectID = envelope.ProjectID
		task.Title = envelope.Title
		task.Description = envelope.Description
		task.Status = envelope.Status
		task.Priority = envelope.Priority
	}
	if task.ProjectID != projectID {
		return TaskContext{}, validationFailed(fmt.Errorf("task %d is not in project %s", taskID, projectID))
	}
	return TaskContext{ID: task.ID, ProjectID: task.ProjectID, Title: task.Title, Description: task.Description, Status: task.Status, Priority: task.Priority}, nil
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
