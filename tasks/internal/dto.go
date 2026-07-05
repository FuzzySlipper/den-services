package tasks

import "time"

type CreateTaskRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	AssignedTo  string   `json:"assigned_to,omitempty"`
	DependsOn   []int64  `json:"depends_on,omitempty"`
	ParentID    *int64   `json:"parent_id,omitempty"`
}

type UpdateTaskRequest struct {
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

func (r UpdateTaskRequest) HasChanges() bool {
	return r.Title != nil ||
		r.Description != nil ||
		r.Status != nil ||
		r.Priority != nil ||
		r.AssignedTo != nil ||
		r.Tags != nil ||
		r.ParentID != nil ||
		r.ClearParent ||
		r.BlockerSummary != nil ||
		r.BlockerReason != nil ||
		r.BlockerAttemptedRemedies != nil ||
		r.BlockerSuggestedNextStep != nil ||
		r.BlockerRequiresHumanInput != nil
}

type AddDependencyRequest struct {
	DependsOn int64 `json:"depends_on"`
}

type TaskResponse struct {
	ID                        int64     `json:"id"`
	ProjectID                 string    `json:"project_id"`
	ParentID                  *int64    `json:"parent_id,omitempty"`
	Title                     string    `json:"title"`
	Description               string    `json:"description,omitempty"`
	Status                    string    `json:"status"`
	Priority                  int       `json:"priority"`
	AssignedTo                string    `json:"assigned_to,omitempty"`
	Tags                      []string  `json:"tags,omitempty"`
	BlockerSummary            string    `json:"blocker_summary,omitempty"`
	BlockerReason             string    `json:"blocker_reason,omitempty"`
	BlockerAttemptedRemedies  string    `json:"blocker_attempted_remedies,omitempty"`
	BlockerSuggestedNextStep  string    `json:"blocker_suggested_next_step,omitempty"`
	BlockerRequiresHumanInput bool      `json:"blocker_requires_human_input"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type TaskSummaryResponse struct {
	TaskResponse
	DependencyCount           int    `json:"dependency_count"`
	UnfinishedDependencyCount int    `json:"unfinished_dependency_count"`
	SubtaskCount              int    `json:"subtask_count"`
	Availability              string `json:"availability"`
}

type TaskDetailResponse struct {
	Task         TaskResponse               `json:"task"`
	Dependencies []DependencyInfoResponse   `json:"dependencies"`
	Subtasks     []TaskSummaryResponse      `json:"subtasks"`
	History      []TaskHistoryEntryResponse `json:"history"`
}

type DependencyInfoResponse struct {
	TaskID int64  `json:"task_id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type TaskHistoryEntryResponse struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Field     string    `json:"field"`
	OldValue  string    `json:"old_value,omitempty"`
	NewValue  string    `json:"new_value,omitempty"`
	ChangedBy string    `json:"changed_by,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
}

type TaskChangeEventResponse struct {
	EventID      int64               `json:"event_id"`
	Cursor       string              `json:"cursor"`
	Kind         string              `json:"kind"`
	ChangedAt    time.Time           `json:"changed_at"`
	TaskID       int64               `json:"task_id"`
	ProjectID    string              `json:"project_id"`
	Task         TaskSummaryResponse `json:"task"`
	BackfillURL  string              `json:"backfill_url,omitempty"`
	ReconnectURL string              `json:"reconnect_url,omitempty"`
}

type TaskChangesResponse struct {
	Events     []TaskChangeEventResponse `json:"events"`
	NextCursor string                    `json:"next_cursor,omitempty"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

func toTaskResponse(task *Task) TaskResponse {
	return TaskResponse{
		ID:                        task.ID(),
		ProjectID:                 task.ProjectID(),
		ParentID:                  task.ParentID(),
		Title:                     task.Title(),
		Description:               task.Description(),
		Status:                    task.Status(),
		Priority:                  task.Priority(),
		AssignedTo:                task.AssignedTo(),
		Tags:                      task.Tags(),
		BlockerSummary:            task.BlockerSummary(),
		BlockerReason:             task.BlockerReason(),
		BlockerAttemptedRemedies:  task.BlockerAttemptedRemedies(),
		BlockerSuggestedNextStep:  task.BlockerSuggestedNextStep(),
		BlockerRequiresHumanInput: task.BlockerRequiresHumanInput(),
		CreatedAt:                 task.CreatedAt(),
		UpdatedAt:                 task.UpdatedAt(),
	}
}

func toTaskSummaryResponse(summary TaskSummary) TaskSummaryResponse {
	return TaskSummaryResponse{
		TaskResponse:              toTaskResponse(summary.Task),
		DependencyCount:           summary.DependencyCount,
		UnfinishedDependencyCount: summary.UnfinishedDependencyCount,
		SubtaskCount:              summary.SubtaskCount,
		Availability:              summary.Availability(),
	}
}

func toTaskSummaryResponses(summaries []TaskSummary) []TaskSummaryResponse {
	responses := make([]TaskSummaryResponse, 0, len(summaries))
	for _, summary := range summaries {
		responses = append(responses, toTaskSummaryResponse(summary))
	}
	return responses
}

func toTaskDetailResponse(detail TaskDetail) TaskDetailResponse {
	return TaskDetailResponse{
		Task:         toTaskResponse(detail.Task),
		Dependencies: toDependencyResponses(detail.Dependencies),
		Subtasks:     toTaskSummaryResponses(detail.Subtasks),
		History:      toHistoryResponses(detail.History),
	}
}

func toDependencyResponses(dependencies []DependencyInfo) []DependencyInfoResponse {
	responses := make([]DependencyInfoResponse, 0, len(dependencies))
	for _, dependency := range dependencies {
		responses = append(responses, DependencyInfoResponse(dependency))
	}
	return responses
}

func toHistoryResponses(entries []TaskHistoryEntry) []TaskHistoryEntryResponse {
	responses := make([]TaskHistoryEntryResponse, 0, len(entries))
	for _, entry := range entries {
		responses = append(responses, TaskHistoryEntryResponse(entry))
	}
	return responses
}

func toTaskChangeResponse(event TaskChangeEvent) TaskChangeEventResponse {
	return TaskChangeEventResponse{
		EventID:   event.ID,
		Cursor:    taskChangeCursor(event.ID),
		Kind:      event.Kind,
		ChangedAt: event.Changed,
		TaskID:    event.Summary.Task.ID(),
		ProjectID: event.Summary.Task.ProjectID(),
		Task:      toTaskSummaryResponse(event.Summary),
	}
}

func toTaskChangeResponses(events []TaskChangeEvent) []TaskChangeEventResponse {
	responses := make([]TaskChangeEventResponse, 0, len(events))
	for _, event := range events {
		responses = append(responses, toTaskChangeResponse(event))
	}
	return responses
}

func taskChangeCursor(eventID int64) string {
	if eventID <= 0 {
		return ""
	}
	return int64String(&eventID)
}
