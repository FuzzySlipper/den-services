package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"den-services/mcp/internal/config"
)

const (
	projectSummaryProjectsBackend = "projects"
	projectSummaryTasksBackend    = "tasks"
	projectSummaryMessagesBackend = "messages"
)

type projectSummaryArguments struct {
	ProjectID string `json:"project_id"`
	SpaceID   string `json:"space_id"`
	Agent     string `json:"agent"`
}

type projectSummaryResponse struct {
	Project            json.RawMessage `json:"project"`
	TaskCountsByStatus statusCounts    `json:"task_counts_by_status"`
	UnreadMessageCount int64           `json:"unread_message_count"`
}

type statusCounts struct {
	Planned    int64 `json:"planned"`
	InProgress int64 `json:"in_progress"`
	Review     int64 `json:"review"`
	Blocked    int64 `json:"blocked"`
	Done       int64 `json:"done"`
	Cancelled  int64 `json:"cancelled"`
}

type summaryTask struct {
	Status string `json:"status"`
}

type unreadCountResponse struct {
	UnreadMessageCount int64 `json:"unread_message_count"`
}

func (c *Client) callProjectSummaryCompose(ctx context.Context, backends map[string]config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	arguments, err := decodeProjectSummaryArguments(call.Arguments)
	if err != nil {
		return Result{}, nil, err
	}
	scopeID, metadataPath, err := projectSummaryScope(route.Operation, arguments)
	if err != nil {
		return Result{}, nil, err
	}
	projectsBackend, tasksBackend, messagesBackend, err := projectSummaryBackends(backends)
	if err != nil {
		return Result{}, nil, err
	}

	metadata, failure, err := c.projectSummaryGET(ctx, projectsBackend, metadataPath, call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}
	counts, failure, err := c.projectTaskCounts(ctx, tasksBackend, scopeID, call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}
	unreadCount, failure, err := c.projectUnreadCount(ctx, messagesBackend, scopeID, arguments.Agent, call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}

	response := projectSummaryResponse{
		Project:            metadata,
		TaskCountsByStatus: counts,
		UnreadMessageCount: unreadCount,
	}
	responseBody, err := json.Marshal(response)
	if err != nil {
		return Result{}, nil, fmt.Errorf("encoding project summary response: %w", err)
	}
	result, err := buildRESTToolResult(responseBody)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func decodeProjectSummaryArguments(raw json.RawMessage) (projectSummaryArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments projectSummaryArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return projectSummaryArguments{}, fmt.Errorf("decoding project summary arguments: %w", err)
	}
	return arguments, nil
}

func projectSummaryScope(operation string, arguments projectSummaryArguments) (string, string, error) {
	switch operation {
	case "get_project":
		projectID := strings.TrimSpace(arguments.ProjectID)
		if projectID == "" {
			return "", "", fmt.Errorf("project summary route requires project_id")
		}
		return projectID, "/v1/projects/" + url.PathEscape(projectID), nil
	case "get_space":
		spaceID := strings.TrimSpace(arguments.SpaceID)
		if spaceID == "" {
			return "", "", fmt.Errorf("project summary route requires space_id")
		}
		return spaceID, "/v1/spaces/" + url.PathEscape(spaceID), nil
	default:
		return "", "", fmt.Errorf("%w: project summary operation %s", ErrUnsupportedAdapter, operation)
	}
}

func projectSummaryBackends(backends map[string]config.BackendConfig) (config.BackendConfig, config.BackendConfig, config.BackendConfig, error) {
	projectsBackend, ok := backends[projectSummaryProjectsBackend]
	if !ok {
		return config.BackendConfig{}, config.BackendConfig{}, config.BackendConfig{}, fmt.Errorf("%w: %s", ErrBackendNotFound, projectSummaryProjectsBackend)
	}
	tasksBackend, ok := backends[projectSummaryTasksBackend]
	if !ok {
		return config.BackendConfig{}, config.BackendConfig{}, config.BackendConfig{}, fmt.Errorf("%w: %s", ErrBackendNotFound, projectSummaryTasksBackend)
	}
	messagesBackend, ok := backends[projectSummaryMessagesBackend]
	if !ok {
		return config.BackendConfig{}, config.BackendConfig{}, config.BackendConfig{}, fmt.Errorf("%w: %s", ErrBackendNotFound, projectSummaryMessagesBackend)
	}
	return projectsBackend, tasksBackend, messagesBackend, nil
}

func (c *Client) projectTaskCounts(ctx context.Context, backend config.BackendConfig, scopeID string, call ToolCall) (statusCounts, *Failure, error) {
	path := "/v1/projects/" + url.PathEscape(scopeID) + "/tasks?tree=true"
	body, failure, err := c.projectSummaryGET(ctx, backend, path, call)
	if err != nil || failure != nil {
		return statusCounts{}, failure, err
	}
	var tasks []summaryTask
	if err := json.Unmarshal(body, &tasks); err != nil {
		return statusCounts{}, nil, fmt.Errorf("parsing task summary response: %w", err)
	}
	var counts statusCounts
	for _, task := range tasks {
		switch task.Status {
		case "planned":
			counts.Planned++
		case "in_progress":
			counts.InProgress++
		case "review":
			counts.Review++
		case "blocked":
			counts.Blocked++
		case "done":
			counts.Done++
		case "cancelled":
			counts.Cancelled++
		}
	}
	return counts, nil, nil
}

func (c *Client) projectUnreadCount(ctx context.Context, backend config.BackendConfig, scopeID string, agent string, call ToolCall) (int64, *Failure, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return 0, nil, nil
	}
	query := url.Values{}
	query.Set("unread_for", agent)
	path := "/v1/projects/" + url.PathEscape(scopeID) + "/messages/unread-count?" + query.Encode()
	body, failure, err := c.projectSummaryGET(ctx, backend, path, call)
	if err != nil || failure != nil {
		return 0, failure, err
	}
	var response unreadCountResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, nil, fmt.Errorf("parsing unread count response: %w", err)
	}
	return response.UnreadMessageCount, nil, nil
}

func (c *Client) projectSummaryGET(ctx context.Context, backend config.BackendConfig, path string, call ToolCall) ([]byte, *Failure, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, backend.BaseURL+path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("building project summary request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	response, cancel, err := c.doRESTRequest(request, backend)
	if err != nil {
		return nil, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading project summary response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, body), nil
	}
	return body, nil, nil
}
