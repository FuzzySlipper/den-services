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
	arguments, err := decodeTaskScopedArguments(call)
	if err != nil {
		return ToolCall{}, nil, err
	}
	stated, exists := arguments["project_id"]
	if exists && call.Operation != "submit_task_for_review" {
		return ToolCall{}, nil, fmt.Errorf("decoding task-scoped arguments: unknown field %q: the project comes from task_id, so omit it", "project_id")
	}
	taskID, err := taskScopedTaskID(arguments)
	if err != nil {
		return ToolCall{}, nil, err
	}
	if taskID <= 0 {
		return ToolCall{}, nil, fmt.Errorf("task-scoped route requires task_id")
	}
	call, failure, err := c.withTaskProject(ctx, backends, call, arguments, taskID)
	if err != nil || failure != nil || !exists {
		return call, failure, err
	}
	// submit_task_for_review required project_id until November 2026, and
	// sessions keep the schema they started with: accept the task's own project.
	var statedProject, derivedProject string
	_ = json.Unmarshal(arguments["project_id"], &derivedProject) // withTaskProject replaced it
	if json.Unmarshal(stated, &statedProject) != nil || strings.TrimSpace(statedProject) != derivedProject {
		return ToolCall{}, nil, fmt.Errorf("project_id %s is not task %d's project; omit it", stated, taskID)
	}
	return call, nil, nil
}

// withOptionalTaskProject fills in an omitted project_id from task_id for a
// project-scoped tool. A call with project_id, or without a task, is unchanged.
func (c *Client) withOptionalTaskProject(ctx context.Context, backends map[string]config.BackendConfig, call ToolCall) (ToolCall, *Failure, error) {
	arguments, err := decodeTaskScopedArguments(call)
	if err != nil {
		return ToolCall{}, nil, err
	}
	var projectID string
	if raw, exists := arguments["project_id"]; exists && json.Unmarshal(raw, &projectID) == nil && strings.TrimSpace(projectID) != "" {
		return call, nil, nil
	}
	taskID, err := taskScopedTaskID(arguments)
	if err != nil || taskID <= 0 {
		return call, nil, err
	}
	return c.withTaskProject(ctx, backends, call, arguments, taskID)
}

func decodeTaskScopedArguments(call ToolCall) (map[string]json.RawMessage, error) {
	arguments := make(map[string]json.RawMessage)
	if len(call.Arguments) == 0 {
		return arguments, nil
	}
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return nil, fmt.Errorf("decoding task-scoped arguments: %w", err)
	}
	return arguments, nil
}

func taskScopedTaskID(arguments map[string]json.RawMessage) (int64, error) {
	var taskID int64
	if rawTaskID, exists := arguments["task_id"]; exists && string(rawTaskID) != "null" {
		if err := json.Unmarshal(rawTaskID, &taskID); err != nil {
			return 0, fmt.Errorf("decoding task-scoped task_id: %w", err)
		}
	}
	return taskID, nil
}

func (c *Client) withTaskProject(ctx context.Context, backends map[string]config.BackendConfig, call ToolCall, arguments map[string]json.RawMessage, taskID int64) (ToolCall, *Failure, error) {
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
