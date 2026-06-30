package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"den-services/mcp/internal/config"
)

type tasksToolArguments struct {
	ProjectID                    string          `json:"project_id"`
	TaskID                       int64           `json:"task_id"`
	DependsOn                    json.RawMessage `json:"depends_on"`
	Title                        *string         `json:"title"`
	Description                  *string         `json:"description"`
	Priority                     *int            `json:"priority"`
	Tags                         json.RawMessage `json:"tags"`
	AssignedTo                   *string         `json:"assigned_to"`
	ParentID                     *int64          `json:"parent_id"`
	Agent                        string          `json:"agent"`
	Status                       *string         `json:"status"`
	BlockerSummary               *string         `json:"blocker_summary"`
	BlockerReason                *string         `json:"blocker_reason"`
	BlockerAttemptedRemedies     *string         `json:"blocker_attempted_remedies"`
	BlockerSuggestedNextStep     *string         `json:"blocker_suggested_next_step"`
	BlockerRequiresHumanInput    *bool           `json:"blocker_requires_human_input"`
	ClearParent                  bool            `json:"clear_parent"`
	AssignedToFilter             *string         `json:"-"`
	StatusFilter                 *string         `json:"-"`
	TagsFilter                   *string         `json:"-"`
	ParentIDFilter               *int64          `json:"-"`
	PriorityFilter               *int            `json:"-"`
	IncludeVerboseCompatibility  bool            `json:"verbose"`
	UnneededCompatibilityPadding string          `json:"-"`
}

type createTaskBody struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	AssignedTo  string   `json:"assigned_to,omitempty"`
	DependsOn   []int64  `json:"depends_on,omitempty"`
	ParentID    *int64   `json:"parent_id,omitempty"`
}

type updateTaskBody struct {
	Agent                     string   `json:"agent"`
	Title                     *string  `json:"title,omitempty"`
	Description               *string  `json:"description,omitempty"`
	Status                    *string  `json:"status,omitempty"`
	Priority                  *int     `json:"priority,omitempty"`
	AssignedTo                *string  `json:"assigned_to,omitempty"`
	Tags                      []string `json:"tags,omitempty"`
	ParentID                  *int64   `json:"parent_id,omitempty"`
	ClearParent               bool     `json:"clear_parent,omitempty"`
	BlockerSummary            *string  `json:"blocker_summary,omitempty"`
	BlockerReason             *string  `json:"blocker_reason,omitempty"`
	BlockerAttemptedRemedies  *string  `json:"blocker_attempted_remedies,omitempty"`
	BlockerSuggestedNextStep  *string  `json:"blocker_suggested_next_step,omitempty"`
	BlockerRequiresHumanInput *bool    `json:"blocker_requires_human_input,omitempty"`
}

type addDependencyBody struct {
	DependsOn int64 `json:"depends_on"`
}

func (c *Client) callTasksREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildTasksRESTRequest(ctx, backend, route, call)
	if err != nil {
		return Result{}, nil, err
	}
	response, cancel, err := c.doRESTRequest(request, backend)
	if err != nil {
		return Result{}, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return Result{}, nil, fmt.Errorf("reading tasks backend response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, responseBody), nil
	}
	result, err := buildRESTToolResult(responseBody)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func buildTasksRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeTasksToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestBody, err := tasksRESTRequestBody(route.Operation, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := tasksRESTURL(backend.BaseURL, route, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("building tasks backend request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}

func decodeTasksToolArguments(raw json.RawMessage) (tasksToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments tasksToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return tasksToolArguments{}, fmt.Errorf("decoding tasks tool arguments: %w", err)
	}
	var filters struct {
		AssignedTo *string `json:"assigned_to"`
		Status     *string `json:"status"`
		Tags       json.RawMessage
		ParentID   *int64 `json:"parent_id"`
		Priority   *int   `json:"priority"`
	}
	if err := json.Unmarshal(raw, &filters); err != nil {
		return tasksToolArguments{}, fmt.Errorf("decoding task list filters: %w", err)
	}
	arguments.AssignedToFilter = filters.AssignedTo
	arguments.StatusFilter = filters.Status
	if tagsFilter := stringFromRaw(filters.Tags); tagsFilter != "" {
		arguments.TagsFilter = &tagsFilter
	}
	arguments.ParentIDFilter = filters.ParentID
	arguments.PriorityFilter = filters.Priority
	return arguments, nil
}

func tasksRESTRequestBody(operation string, arguments tasksToolArguments) ([]byte, error) {
	switch operation {
	case "create_task":
		tags, err := parseStringList(arguments.Tags)
		if err != nil {
			return nil, err
		}
		dependsOn, err := parseInt64List(arguments.DependsOn)
		if err != nil {
			return nil, err
		}
		return json.Marshal(createTaskBody{
			Title:       stringValue(arguments.Title),
			Description: stringValue(arguments.Description),
			Priority:    intValue(arguments.Priority),
			Tags:        tags,
			AssignedTo:  stringValue(arguments.AssignedTo),
			DependsOn:   dependsOn,
			ParentID:    arguments.ParentID,
		})
	case "update_task":
		tags, err := parseStringList(arguments.Tags)
		if err != nil {
			return nil, err
		}
		return json.Marshal(updateTaskBody{
			Agent:                     strings.TrimSpace(arguments.Agent),
			Title:                     arguments.Title,
			Description:               arguments.Description,
			Status:                    arguments.Status,
			Priority:                  arguments.Priority,
			AssignedTo:                arguments.AssignedTo,
			Tags:                      tags,
			ParentID:                  arguments.ParentID,
			ClearParent:               arguments.ClearParent,
			BlockerSummary:            arguments.BlockerSummary,
			BlockerReason:             arguments.BlockerReason,
			BlockerAttemptedRemedies:  arguments.BlockerAttemptedRemedies,
			BlockerSuggestedNextStep:  arguments.BlockerSuggestedNextStep,
			BlockerRequiresHumanInput: arguments.BlockerRequiresHumanInput,
		})
	case "add_dependency":
		dependsOn, err := singleDependsOn(arguments.DependsOn)
		if err != nil {
			return nil, err
		}
		return json.Marshal(addDependencyBody{DependsOn: dependsOn})
	case "get_task", "list_tasks", "next_task", "remove_dependency":
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: tasks operation %s", ErrUnsupportedAdapter, operation)
	}
}

func tasksRESTURL(baseURL string, route Route, arguments tasksToolArguments) (string, error) {
	routePath, err := expandTasksPath(route.Path, arguments)
	if err != nil {
		return "", err
	}
	parsedURL, err := url.Parse(baseURL + routePath)
	if err != nil {
		return "", fmt.Errorf("parsing tasks backend URL: %w", err)
	}
	query := parsedURL.Query()
	switch route.Operation {
	case "list_tasks":
		setStringQuery(query, "assigned_to", arguments.AssignedToFilter)
		setStringQuery(query, "status", arguments.StatusFilter)
		setStringQuery(query, "tags", arguments.TagsFilter)
		setInt64Query(query, "parent_id", arguments.ParentIDFilter)
		setIntQuery(query, "priority", arguments.PriorityFilter)
	case "next_task":
		setStringQuery(query, "assigned_to", arguments.AssignedToFilter)
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func expandTasksPath(path string, arguments tasksToolArguments) (string, error) {
	result := path
	if strings.Contains(result, "{project_id}") {
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("tasks route requires project_id")
		}
		result = strings.ReplaceAll(result, "{project_id}", url.PathEscape(strings.TrimSpace(arguments.ProjectID)))
	}
	if strings.Contains(result, "{task_id}") {
		if arguments.TaskID == 0 {
			return "", fmt.Errorf("tasks route requires task_id")
		}
		result = strings.ReplaceAll(result, "{task_id}", strconv.FormatInt(arguments.TaskID, 10))
	}
	if strings.Contains(result, "{depends_on}") {
		dependsOn, err := singleDependsOn(arguments.DependsOn)
		if err != nil {
			return "", err
		}
		result = strings.ReplaceAll(result, "{depends_on}", strconv.FormatInt(dependsOn, 10))
	}
	return result, nil
}

func parseStringList(raw json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var direct []string
	if err := json.Unmarshal(raw, &direct); err == nil {
		return trimStrings(direct), nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("decoding string list: %w", err)
	}
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	if strings.HasPrefix(strings.TrimSpace(encoded), "[") {
		if err := json.Unmarshal([]byte(encoded), &direct); err != nil {
			return nil, fmt.Errorf("decoding JSON-encoded string list: %w", err)
		}
		return trimStrings(direct), nil
	}
	return splitCSV(encoded), nil
}

func parseInt64List(raw json.RawMessage) ([]int64, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var single int64
	if err := json.Unmarshal(raw, &single); err == nil {
		return []int64{single}, nil
	}
	var direct []int64
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("decoding int64 list: %w", err)
	}
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	parts := splitCSV(encoded)
	values := make([]int64, 0, len(parts))
	for _, part := range parts {
		parsed, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing depends_on %q: %w", part, err)
		}
		values = append(values, parsed)
	}
	return values, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func singleDependsOn(raw json.RawMessage) (int64, error) {
	values, err := parseInt64List(raw)
	if err != nil {
		return 0, err
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("tasks route requires depends_on")
	}
	return values[0], nil
}

func setStringQuery(query url.Values, key string, value *string) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return
	}
	query.Set(key, strings.TrimSpace(*value))
}

func setInt64Query(query url.Values, key string, value *int64) {
	if value == nil {
		return
	}
	query.Set(key, strconv.FormatInt(*value, 10))
}

func setIntQuery(query url.Values, key string, value *int) {
	if value == nil {
		return
	}
	query.Set(key, strconv.Itoa(*value))
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func stringFromRaw(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func trimStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
