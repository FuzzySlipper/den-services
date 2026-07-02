package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"den-services/mcp/internal/config"
)

const (
	taskWorkflowTasksBackend    = "tasks"
	taskWorkflowReviewBackend   = "review"
	taskWorkflowMessagesBackend = "messages"
)

type taskWorkflowSummaryArguments struct {
	TaskID  int64 `json:"task_id"`
	Verbose bool  `json:"verbose"`
}

type taskWorkflowTaskDetail struct {
	Task         taskWorkflowTask           `json:"task"`
	Dependencies json.RawMessage            `json:"dependencies,omitempty"`
	Subtasks     json.RawMessage            `json:"subtasks,omitempty"`
	History      json.RawMessage            `json:"history,omitempty"`
	Extra        map[string]json.RawMessage `json:"-"`
}

type taskWorkflowTask struct {
	ID         int64  `json:"id"`
	ProjectID  string `json:"project_id"`
	ParentID   *int64 `json:"parent_id,omitempty"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Priority   int    `json:"priority,omitempty"`
	AssignedTo string `json:"assigned_to,omitempty"`
}

type taskWorkflowReviewSummary struct {
	CurrentRound           json.RawMessage `json:"current_round,omitempty"`
	CurrentVerdict         json.RawMessage `json:"current_verdict,omitempty"`
	ReviewRoundCount       int64           `json:"review_round_count"`
	UnresolvedFindingCount int64           `json:"unresolved_finding_count"`
	ResolvedFindingCount   int64           `json:"resolved_finding_count"`
	AddressedFindingCount  int64           `json:"addressed_finding_count"`
	OpenFindings           json.RawMessage `json:"open_findings,omitempty"`
	ResolvedFindings       json.RawMessage `json:"resolved_findings,omitempty"`
	Timeline               json.RawMessage `json:"timeline,omitempty"`
}

type taskWorkflowPacketHeader struct {
	ID         int64          `json:"id"`
	ProjectID  string         `json:"project_id"`
	TaskID     *int64         `json:"task_id,omitempty"`
	ThreadID   *int64         `json:"thread_id,omitempty"`
	Sender     string         `json:"sender"`
	Intent     string         `json:"intent"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	PacketType string         `json:"packet_type,omitempty"`
	Role       string         `json:"role,omitempty"`
}

type taskWorkflowPacketWarning struct {
	PacketType string `json:"packet_type"`
	Role       string `json:"role"`
	Message    string `json:"message"`
}

type taskWorkflowSummaryResponse struct {
	TaskID                 int64                                `json:"task_id"`
	ProjectID              string                               `json:"project_id"`
	Status                 string                               `json:"status"`
	AssignedTo             string                               `json:"assigned_to,omitempty"`
	Task                   taskWorkflowTask                     `json:"task"`
	Dependencies           json.RawMessage                      `json:"dependencies,omitempty"`
	Subtasks               json.RawMessage                      `json:"subtasks,omitempty"`
	CurrentReviewRound     json.RawMessage                      `json:"current_review_round,omitempty"`
	CurrentVerdict         json.RawMessage                      `json:"current_verdict,omitempty"`
	ReviewRoundCount       int64                                `json:"review_round_count"`
	UnresolvedFindingCount int64                                `json:"unresolved_finding_count"`
	ResolvedFindingCount   int64                                `json:"resolved_finding_count"`
	AddressedFindingCount  int64                                `json:"addressed_finding_count"`
	OpenFindings           json.RawMessage                      `json:"open_findings,omitempty"`
	LatestPackets          map[string]*taskWorkflowPacketHeader `json:"latest_packets"`
	PacketWarnings         []taskWorkflowPacketWarning          `json:"packet_warnings,omitempty"`
	Links                  map[string]string                    `json:"links"`
	History                json.RawMessage                      `json:"history,omitempty"`
	ResolvedFindings       json.RawMessage                      `json:"resolved_findings,omitempty"`
	ReviewTimeline         json.RawMessage                      `json:"review_timeline,omitempty"`
	TaskDetail             json.RawMessage                      `json:"task_detail,omitempty"`
	Review                 json.RawMessage                      `json:"review,omitempty"`
}

type taskWorkflowPacketQuery struct {
	Key        string
	PacketType string
	Role       string
}

var taskWorkflowPacketQueries = []taskWorkflowPacketQuery{
	{Key: "coder", PacketType: "coder_context_packet", Role: "coder"},
	{Key: "reviewer", PacketType: "reviewer_context_packet", Role: "reviewer"},
	{Key: "validator", PacketType: "validator_context_packet", Role: "validator"},
	{Key: "drift_checker", PacketType: "drift_checker_context_packet", Role: "drift_checker"},
	{Key: "packet_auditor", PacketType: "packet_auditor_context_packet", Role: "packet_auditor"},
	{Key: "scope_auditor", PacketType: "scope_auditor_context_packet", Role: "scope_auditor"},
}

func (c *Client) callTaskWorkflowSummaryCompose(ctx context.Context, backends map[string]config.BackendConfig, _ Route, call ToolCall) (Result, *Failure, error) {
	arguments, err := decodeTaskWorkflowSummaryArguments(call.Arguments)
	if err != nil {
		return Result{}, nil, err
	}
	tasksBackend, reviewBackend, messagesBackend, err := taskWorkflowBackends(backends)
	if err != nil {
		return Result{}, nil, err
	}

	taskBody, failure, err := c.taskWorkflowGET(ctx, tasksBackend, "/v1/tasks/"+strconv.FormatInt(arguments.TaskID, 10), call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}
	var taskDetail taskWorkflowTaskDetail
	if err := json.Unmarshal(taskBody, &taskDetail); err != nil {
		return Result{}, nil, fmt.Errorf("parsing task workflow task detail: %w", err)
	}
	if taskDetail.Task.ID == 0 || strings.TrimSpace(taskDetail.Task.ProjectID) == "" {
		return Result{}, nil, fmt.Errorf("task workflow task detail missing task id or project_id")
	}

	reviewPath := "/v1/projects/" + url.PathEscape(taskDetail.Task.ProjectID) + "/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/review/workflow-summary"
	reviewBody, failure, err := c.taskWorkflowGET(ctx, reviewBackend, reviewPath, call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}
	var reviewSummary taskWorkflowReviewSummary
	if err := json.Unmarshal(reviewBody, &reviewSummary); err != nil {
		return Result{}, nil, fmt.Errorf("parsing task workflow review summary: %w", err)
	}

	packets, warnings := c.taskWorkflowLatestPackets(ctx, messagesBackend, taskDetail.Task.ProjectID, arguments.TaskID, call)
	response := taskWorkflowSummaryResponse{
		TaskID:                 taskDetail.Task.ID,
		ProjectID:              taskDetail.Task.ProjectID,
		Status:                 taskDetail.Task.Status,
		AssignedTo:             taskDetail.Task.AssignedTo,
		Task:                   taskDetail.Task,
		Dependencies:           taskDetail.Dependencies,
		Subtasks:               taskDetail.Subtasks,
		CurrentReviewRound:     reviewSummary.CurrentRound,
		CurrentVerdict:         reviewSummary.CurrentVerdict,
		ReviewRoundCount:       reviewSummary.ReviewRoundCount,
		UnresolvedFindingCount: reviewSummary.UnresolvedFindingCount,
		ResolvedFindingCount:   reviewSummary.ResolvedFindingCount,
		AddressedFindingCount:  reviewSummary.AddressedFindingCount,
		OpenFindings:           reviewSummary.OpenFindings,
		LatestPackets:          packets,
		PacketWarnings:         warnings,
		Links: map[string]string{
			"task":                    "/v1/tasks/" + strconv.FormatInt(arguments.TaskID, 10),
			"review_workflow_summary": reviewPath,
			"latest_packets":          "/v1/projects/" + url.PathEscape(taskDetail.Task.ProjectID) + "/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/packets/latest",
		},
	}
	if arguments.Verbose {
		response.History = taskDetail.History
		response.ResolvedFindings = reviewSummary.ResolvedFindings
		response.ReviewTimeline = reviewSummary.Timeline
		response.TaskDetail = taskBody
		response.Review = reviewBody
	}

	responseBody, err := json.Marshal(response)
	if err != nil {
		return Result{}, nil, fmt.Errorf("encoding task workflow summary response: %w", err)
	}
	result, err := buildRESTToolResult(responseBody)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func decodeTaskWorkflowSummaryArguments(raw json.RawMessage) (taskWorkflowSummaryArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments taskWorkflowSummaryArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return taskWorkflowSummaryArguments{}, fmt.Errorf("decoding task workflow summary arguments: %w", err)
	}
	if arguments.TaskID <= 0 {
		return taskWorkflowSummaryArguments{}, fmt.Errorf("task workflow summary route requires task_id")
	}
	return arguments, nil
}

func taskWorkflowBackends(backends map[string]config.BackendConfig) (config.BackendConfig, config.BackendConfig, config.BackendConfig, error) {
	tasksBackend, ok := backends[taskWorkflowTasksBackend]
	if !ok {
		return config.BackendConfig{}, config.BackendConfig{}, config.BackendConfig{}, fmt.Errorf("%w: %s", ErrBackendNotFound, taskWorkflowTasksBackend)
	}
	reviewBackend, ok := backends[taskWorkflowReviewBackend]
	if !ok {
		return config.BackendConfig{}, config.BackendConfig{}, config.BackendConfig{}, fmt.Errorf("%w: %s", ErrBackendNotFound, taskWorkflowReviewBackend)
	}
	messagesBackend, ok := backends[taskWorkflowMessagesBackend]
	if !ok {
		return config.BackendConfig{}, config.BackendConfig{}, config.BackendConfig{}, fmt.Errorf("%w: %s", ErrBackendNotFound, taskWorkflowMessagesBackend)
	}
	return tasksBackend, reviewBackend, messagesBackend, nil
}

func (c *Client) taskWorkflowLatestPackets(ctx context.Context, backend config.BackendConfig, projectID string, taskID int64, call ToolCall) (map[string]*taskWorkflowPacketHeader, []taskWorkflowPacketWarning) {
	packets := make(map[string]*taskWorkflowPacketHeader, len(taskWorkflowPacketQueries))
	var warnings []taskWorkflowPacketWarning
	for _, query := range taskWorkflowPacketQueries {
		header, warning := c.taskWorkflowLatestPacket(ctx, backend, projectID, taskID, query, call)
		packets[query.Key] = header
		if warning != nil {
			warnings = append(warnings, *warning)
		}
	}
	return packets, warnings
}

func (c *Client) taskWorkflowLatestPacket(ctx context.Context, backend config.BackendConfig, projectID string, taskID int64, query taskWorkflowPacketQuery, call ToolCall) (*taskWorkflowPacketHeader, *taskWorkflowPacketWarning) {
	values := url.Values{}
	values.Set("packet_type", query.PacketType)
	values.Set("role", query.Role)
	path := "/v1/projects/" + url.PathEscape(projectID) + "/tasks/" + strconv.FormatInt(taskID, 10) + "/packets/latest?" + values.Encode()
	body, failure, err := c.taskWorkflowOptionalGET(ctx, backend, path, call)
	if err != nil {
		return nil, &taskWorkflowPacketWarning{PacketType: query.PacketType, Role: query.Role, Message: err.Error()}
	}
	if failure != nil {
		if failure.StatusCode != nil && *failure.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, &taskWorkflowPacketWarning{PacketType: query.PacketType, Role: query.Role, Message: failure.Message}
	}
	var header taskWorkflowPacketHeader
	if err := json.Unmarshal(body, &header); err != nil {
		return nil, &taskWorkflowPacketWarning{PacketType: query.PacketType, Role: query.Role, Message: fmt.Sprintf("parsing packet header: %v", err)}
	}
	header.PacketType = query.PacketType
	header.Role = query.Role
	return &header, nil
}

func (c *Client) taskWorkflowGET(ctx context.Context, backend config.BackendConfig, path string, call ToolCall) ([]byte, *Failure, error) {
	request, err := newTaskWorkflowRequest(ctx, backend, path)
	if err != nil {
		return nil, nil, err
	}
	response, cancel, err := c.doRESTRequest(request, backend)
	if err != nil {
		return nil, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading task workflow response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, body), nil
	}
	return body, nil, nil
}

func (c *Client) taskWorkflowOptionalGET(ctx context.Context, backend config.BackendConfig, path string, call ToolCall) ([]byte, *Failure, error) {
	request, err := newTaskWorkflowRequest(ctx, backend, path)
	if err != nil {
		return nil, nil, err
	}
	response, cancel, err := c.doRESTRequest(request, backend)
	if err != nil {
		return nil, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading task workflow optional response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, body), nil
	}
	return body, nil, nil
}

func newTaskWorkflowRequest(ctx context.Context, backend config.BackendConfig, path string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, backend.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building task workflow request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}
