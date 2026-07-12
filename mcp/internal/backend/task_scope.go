package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"den-services/mcp/internal/config"
)

func (c *Client) withCanonicalTaskProject(ctx context.Context, backends map[string]config.BackendConfig, call ToolCall) (ToolCall, *Failure, error) {
	arguments := make(map[string]json.RawMessage)
	if len(call.Arguments) == 0 {
		call.Arguments = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return ToolCall{}, nil, fmt.Errorf("decoding task-scoped arguments: %w", err)
	}
	if _, exists := arguments["project_id"]; exists {
		return ToolCall{}, nil, fmt.Errorf("decoding task-scoped arguments: unknown field %q", "project_id")
	}
	var taskID int64
	if rawTaskID, exists := arguments["task_id"]; exists {
		if err := json.Unmarshal(rawTaskID, &taskID); err != nil {
			return ToolCall{}, nil, fmt.Errorf("decoding task-scoped task_id: %w", err)
		}
	}
	if taskID <= 0 {
		return ToolCall{}, nil, fmt.Errorf("task-scoped route requires task_id")
	}
	tasksBackend, exists := backends[taskWorkflowTasksBackend]
	if !exists {
		return ToolCall{}, nil, fmt.Errorf("%w: %s", ErrBackendNotFound, taskWorkflowTasksBackend)
	}
	taskHandle := "/v1/tasks/" + strconv.FormatInt(taskID, 10)
	body, failure, err := c.taskContextGET(ctx, tasksBackend, taskHandle, call)
	if err != nil || failure != nil {
		return ToolCall{}, failure, err
	}
	var detail taskContextTaskDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return ToolCall{}, nil, fmt.Errorf("parsing canonical task scope: %w", err)
	}
	projectID := strings.TrimSpace(detail.Task.ProjectID)
	if detail.Task.ID != taskID || projectID == "" {
		return ToolCall{}, nil, fmt.Errorf("canonical task scope missing task identity")
	}
	projectJSON, err := json.Marshal(projectID)
	if err != nil {
		return ToolCall{}, nil, fmt.Errorf("encoding canonical task project: %w", err)
	}
	arguments["project_id"] = projectJSON
	call.Arguments, err = json.Marshal(arguments)
	if err != nil {
		return ToolCall{}, nil, fmt.Errorf("encoding task-scoped arguments: %w", err)
	}
	return call, nil, nil
}
