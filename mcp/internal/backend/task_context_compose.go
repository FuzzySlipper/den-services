package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"den-services/mcp/internal/config"
)

const (
	taskContextSchemaVersion = "1"
	taskContextMessageLimit  = 12
	taskContextFindingLimit  = 12
	taskContextItemLimit     = 12
	taskContextPacketLimit   = 6
	taskContextTextLimit     = 8000
	taskContextMessageBytes  = 4000
)

type taskContextArguments struct {
	ProjectID string `json:"project_id"`
	TaskID    int64  `json:"task_id"`
}

type taskContextTaskDetail struct {
	Task         taskContextTask   `json:"task"`
	Dependencies []json.RawMessage `json:"dependencies"`
	Subtasks     []json.RawMessage `json:"subtasks"`
}

type taskContextTask struct {
	ID                   int64    `json:"id"`
	ProjectID            string   `json:"project_id"`
	ParentID             *int64   `json:"parent_id,omitempty"`
	Title                string   `json:"title"`
	Description          string   `json:"description,omitempty"`
	DescriptionTruncated bool     `json:"description_truncated,omitempty"`
	Status               string   `json:"status"`
	Priority             int      `json:"priority,omitempty"`
	AssignedTo           string   `json:"assigned_to,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	CreatedAt            string   `json:"created_at,omitempty"`
	UpdatedAt            string   `json:"updated_at,omitempty"`
}

type taskContextMessage struct {
	ID               int64           `json:"id"`
	Sender           string          `json:"sender"`
	Intent           string          `json:"intent,omitempty"`
	Content          string          `json:"content"`
	ContentTruncated bool            `json:"content_truncated,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	CreatedAt        string          `json:"created_at"`
}

type taskContextGuidance struct {
	ProjectID      string                 `json:"project_id"`
	ResolvedAt     string                 `json:"resolved_at"`
	Sources        []taskContextDocHandle `json:"sources"`
	Incomplete     bool                   `json:"incomplete,omitempty"`
	Truncated      bool                   `json:"truncated,omitempty"`
	SkippedSources json.RawMessage        `json:"skipped_sources,omitempty"`
}

type taskContextDocHandle struct {
	SourceScope       string   `json:"source_scope,omitempty"`
	DocumentProjectID string   `json:"document_project_id"`
	DocumentSlug      string   `json:"document_slug"`
	DocumentTitle     string   `json:"document_title"`
	DocumentType      string   `json:"document_type,omitempty"`
	DocumentUpdatedAt string   `json:"document_updated_at,omitempty"`
	Visibility        string   `json:"visibility,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Importance        string   `json:"importance,omitempty"`
	Audience          []string `json:"audience,omitempty"`
	SortOrder         int      `json:"sort_order,omitempty"`
	Notes             string   `json:"notes,omitempty"`
}

type taskContextGuidanceWire struct {
	ProjectID      string                   `json:"project_id"`
	ResolvedAt     string                   `json:"resolved_at"`
	Sources        []guidanceSourceResponse `json:"sources"`
	Incomplete     bool                     `json:"incomplete,omitempty"`
	Truncated      bool                     `json:"truncated,omitempty"`
	SkippedSources json.RawMessage          `json:"skipped_sources,omitempty"`
}

type taskContextLibrarianWire struct {
	Query           string            `json:"query"`
	RelevantItems   []json.RawMessage `json:"relevant_items"`
	Recommendations []string          `json:"recommendations"`
	Confidence      string            `json:"confidence,omitempty"`
}

type taskContextSourceStatus struct {
	Source    string `json:"source"`
	State     string `json:"state"`
	Handle    string `json:"handle"`
	ErrorCode string `json:"error_code,omitempty"`
	Retryable bool   `json:"retryable"`
}

type taskContextLimits struct {
	RecentMessages int `json:"recent_messages"`
	OpenFindings   int `json:"open_findings"`
	LatestPackets  int `json:"latest_packets"`
	LibrarianItems int `json:"librarian_items"`
}

type taskContextTruncation struct {
	TaskDescription bool `json:"task_description,omitempty"`
	Dependencies    bool `json:"dependencies,omitempty"`
	Subtasks        bool `json:"subtasks,omitempty"`
	RecentMessages  bool `json:"recent_messages,omitempty"`
	OpenFindings    bool `json:"open_findings,omitempty"`
	GuidanceSources bool `json:"guidance_sources,omitempty"`
	LibrarianItems  bool `json:"librarian_items,omitempty"`
}

type taskContextWorkflow struct {
	CurrentReviewRound     json.RawMessage                      `json:"current_review_round,omitempty"`
	CurrentVerdict         json.RawMessage                      `json:"current_verdict,omitempty"`
	ReviewRoundCount       int64                                `json:"review_round_count"`
	UnresolvedFindingCount int64                                `json:"unresolved_finding_count"`
	OpenFindings           []json.RawMessage                    `json:"open_findings"`
	LatestPackets          map[string]*taskWorkflowPacketHeader `json:"latest_packets"`
	PacketWarnings         []taskWorkflowPacketWarning          `json:"packet_warnings,omitempty"`
}

type taskContextLibrarian struct {
	Query           string            `json:"query"`
	RelevantItems   []json.RawMessage `json:"relevant_items"`
	Recommendations []string          `json:"recommendations"`
	Confidence      string            `json:"confidence,omitempty"`
}

type taskContextResponse struct {
	SchemaVersion  string                    `json:"schema_version"`
	GeneratedAt    string                    `json:"generated_at"`
	ProjectID      string                    `json:"project_id"`
	TaskID         int64                     `json:"task_id"`
	Task           taskContextTask           `json:"task"`
	Dependencies   []json.RawMessage         `json:"dependencies"`
	Subtasks       []json.RawMessage         `json:"subtasks"`
	RecentMessages []taskContextMessage      `json:"recent_messages"`
	Workflow       taskContextWorkflow       `json:"workflow"`
	Guidance       taskContextGuidance       `json:"guidance"`
	Librarian      taskContextLibrarian      `json:"librarian"`
	SearchHints    []string                  `json:"search_hints"`
	Limits         taskContextLimits         `json:"limits"`
	Truncated      taskContextTruncation     `json:"truncated,omitempty"`
	SourceStatus   []taskContextSourceStatus `json:"source_status"`
}

func (c *Client) callTaskContextCompose(ctx context.Context, backends map[string]config.BackendConfig, _ Route, call ToolCall) (Result, *Failure, error) {
	arguments, err := decodeTaskContextArguments(call.Arguments)
	if err != nil {
		return Result{}, nil, err
	}
	tasksBackend, ok := backends[taskWorkflowTasksBackend]
	if !ok {
		return Result{}, nil, fmt.Errorf("%w: %s", ErrBackendNotFound, taskWorkflowTasksBackend)
	}

	taskHandle := "/v1/tasks/" + strconv.FormatInt(arguments.TaskID, 10)
	taskBody, failure, err := c.taskContextGET(ctx, tasksBackend, taskHandle, call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}
	var taskDetail taskContextTaskDetail
	if err := json.Unmarshal(taskBody, &taskDetail); err != nil {
		return Result{}, nil, fmt.Errorf("parsing task context task detail: %w", err)
	}
	if taskDetail.Task.ID != arguments.TaskID || strings.TrimSpace(taskDetail.Task.ProjectID) == "" {
		return Result{}, nil, fmt.Errorf("task context task detail missing canonical task identity")
	}
	if taskDetail.Task.ProjectID != arguments.ProjectID {
		return Result{}, nil, fmt.Errorf("task context project_id does not match canonical task project")
	}

	response := taskContextResponse{
		SchemaVersion:  taskContextSchemaVersion,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ProjectID:      arguments.ProjectID,
		TaskID:         arguments.TaskID,
		Task:           taskDetail.Task,
		Dependencies:   sortRawMessages(taskDetail.Dependencies, taskContextItemLimit),
		Subtasks:       sortRawMessages(taskDetail.Subtasks, taskContextItemLimit),
		RecentMessages: []taskContextMessage{},
		Workflow: taskContextWorkflow{
			OpenFindings:  []json.RawMessage{},
			LatestPackets: make(map[string]*taskWorkflowPacketHeader, taskContextPacketLimit),
		},
		Guidance:  taskContextGuidance{ProjectID: arguments.ProjectID, Sources: []taskContextDocHandle{}},
		Librarian: taskContextLibrarian{RelevantItems: []json.RawMessage{}, Recommendations: []string{}},
		Limits: taskContextLimits{
			RecentMessages: taskContextMessageLimit,
			OpenFindings:   taskContextFindingLimit,
			LatestPackets:  taskContextPacketLimit,
			LibrarianItems: taskContextItemLimit,
		},
		SourceStatus: []taskContextSourceStatus{{Source: "task", State: "ok", Handle: taskHandle, Retryable: false}},
	}
	response.Task.Description, response.Task.DescriptionTruncated = truncateTaskContextText(response.Task.Description, taskContextTextLimit)
	response.Truncated.TaskDescription = response.Task.DescriptionTruncated
	response.Truncated.Dependencies = len(taskDetail.Dependencies) > taskContextItemLimit
	response.Truncated.Subtasks = len(taskDetail.Subtasks) > taskContextItemLimit

	var wg sync.WaitGroup
	var sourceMu sync.Mutex
	addStatus := func(status taskContextSourceStatus) {
		sourceMu.Lock()
		defer sourceMu.Unlock()
		response.SourceStatus = append(response.SourceStatus, status)
	}

	if backend, exists := backends[taskWorkflowReviewBackend]; exists {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handle := "/v1/projects/" + url.PathEscape(arguments.ProjectID) + "/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/review/workflow-summary"
			body, downstreamFailure, downstreamErr := c.taskContextGET(ctx, backend, handle, call)
			if downstreamErr != nil || downstreamFailure != nil {
				addStatus(taskContextStatus("workflow", handle, downstreamFailure, downstreamErr))
				return
			}
			var summary taskWorkflowReviewSummary
			if err := json.Unmarshal(body, &summary); err != nil {
				addStatus(taskContextStatus("workflow", handle, nil, err))
				return
			}
			workflow := taskContextWorkflow{
				CurrentReviewRound:     summary.CurrentRound,
				CurrentVerdict:         summary.CurrentVerdict,
				ReviewRoundCount:       summary.ReviewRoundCount,
				UnresolvedFindingCount: summary.UnresolvedFindingCount,
				OpenFindings:           sortRawMessages(decodeRawArray(summary.OpenFindings), taskContextFindingLimit),
				LatestPackets:          make(map[string]*taskWorkflowPacketHeader, taskContextPacketLimit),
			}
			if messagesBackend, exists := backends[taskWorkflowMessagesBackend]; exists {
				workflow.LatestPackets, workflow.PacketWarnings = c.taskWorkflowLatestPackets(ctx, messagesBackend, arguments.ProjectID, arguments.TaskID, call)
			}
			sourceMu.Lock()
			response.Workflow = workflow
			response.Truncated.OpenFindings = len(decodeRawArray(summary.OpenFindings)) > taskContextFindingLimit
			sourceMu.Unlock()
			addStatus(taskContextSourceStatus{Source: "workflow", State: "ok", Handle: handle, Retryable: false})
		}()
	} else {
		addStatus(taskContextSourceStatus{Source: "workflow", State: "unavailable", Handle: "review", ErrorCode: "den_backend_config_error", Retryable: false})
	}

	if backend, exists := backends[taskWorkflowMessagesBackend]; exists {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handle := "/v1/projects/" + url.PathEscape(arguments.ProjectID) + "/messages?task_id=" + strconv.FormatInt(arguments.TaskID, 10) + "&limit=" + strconv.Itoa(taskContextMessageLimit) + "&verbose=true"
			body, downstreamFailure, downstreamErr := c.taskContextGET(ctx, backend, handle, call)
			if downstreamErr != nil || downstreamFailure != nil {
				addStatus(taskContextStatus("task_thread", handle, downstreamFailure, downstreamErr))
				return
			}
			var messages []taskContextMessage
			if err := json.Unmarshal(body, &messages); err != nil {
				addStatus(taskContextStatus("task_thread", handle, nil, err))
				return
			}
			sort.Slice(messages, func(i, j int) bool {
				if messages[i].CreatedAt == messages[j].CreatedAt {
					return messages[i].ID > messages[j].ID
				}
				return messages[i].CreatedAt > messages[j].CreatedAt
			})
			if len(messages) > taskContextMessageLimit {
				sourceMu.Lock()
				response.Truncated.RecentMessages = true
				sourceMu.Unlock()
				messages = messages[:taskContextMessageLimit]
			}
			for index := range messages {
				messages[index].Content, messages[index].ContentTruncated = truncateTaskContextText(messages[index].Content, taskContextMessageBytes)
			}
			sourceMu.Lock()
			response.RecentMessages = messages
			sourceMu.Unlock()
			addStatus(taskContextSourceStatus{Source: "task_thread", State: "ok", Handle: handle, Retryable: false})
		}()
	} else {
		addStatus(taskContextSourceStatus{Source: "task_thread", State: "unavailable", Handle: "messages", ErrorCode: "den_backend_config_error", Retryable: false})
	}

	if backend, exists := backends["guidance"]; exists {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handle := "/v1/projects/" + url.PathEscape(arguments.ProjectID) + "/agent-guidance"
			body, downstreamFailure, downstreamErr := c.taskContextGET(ctx, backend, handle, call)
			if downstreamErr != nil || downstreamFailure != nil {
				addStatus(taskContextStatus("guidance", handle, downstreamFailure, downstreamErr))
				return
			}
			var wire taskContextGuidanceWire
			if err := json.Unmarshal(body, &wire); err != nil {
				addStatus(taskContextStatus("guidance", handle, nil, err))
				return
			}
			guidance := taskContextGuidance{ProjectID: wire.ProjectID, ResolvedAt: wire.ResolvedAt, Incomplete: wire.Incomplete, Truncated: wire.Truncated, SkippedSources: wire.SkippedSources, Sources: make([]taskContextDocHandle, 0, len(wire.Sources))}
			for _, source := range wire.Sources {
				guidance.Sources = append(guidance.Sources, taskContextDocHandle{SourceScope: source.SourceScope, DocumentProjectID: source.DocumentProjectID, DocumentSlug: source.DocumentSlug, DocumentTitle: source.DocumentTitle, DocumentType: source.DocumentType, DocumentUpdatedAt: source.DocumentUpdatedAt, Visibility: source.Visibility, Tags: source.Tags, Importance: source.Importance, Audience: source.Audience, SortOrder: source.SortOrder, Notes: source.Notes})
			}
			sort.Slice(guidance.Sources, func(i, j int) bool {
				if guidance.Sources[i].SortOrder == guidance.Sources[j].SortOrder {
					return guidance.Sources[i].DocumentSlug < guidance.Sources[j].DocumentSlug
				}
				return guidance.Sources[i].SortOrder < guidance.Sources[j].SortOrder
			})
			sourceMu.Lock()
			if len(guidance.Sources) > taskContextItemLimit {
				guidance.Sources = guidance.Sources[:taskContextItemLimit]
				response.Truncated.GuidanceSources = true
			}
			response.Guidance = guidance
			sourceMu.Unlock()
			state := "ok"
			if wire.Incomplete || wire.Truncated {
				state = "partial"
			}
			addStatus(taskContextSourceStatus{Source: "guidance", State: state, Handle: handle, Retryable: false})
		}()
	} else {
		addStatus(taskContextSourceStatus{Source: "guidance", State: "unavailable", Handle: "guidance", ErrorCode: "den_backend_config_error", Retryable: false})
	}

	librarianQuery := taskContextLibrarianQuery(taskDetail.Task)
	if backend, exists := backends["librarian"]; exists {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handle := "/v1/projects/" + url.PathEscape(arguments.ProjectID) + "/librarian/query"
			body, downstreamFailure, downstreamErr := c.taskContextPOST(ctx, backend, handle, librarianQueryBody{Query: librarianQuery, TaskID: &arguments.TaskID, IncludeGlobal: boolPointer(true)}, call)
			if downstreamErr != nil || downstreamFailure != nil {
				addStatus(taskContextStatus("librarian", handle, downstreamFailure, downstreamErr))
				return
			}
			var wire taskContextLibrarianWire
			if err := json.Unmarshal(body, &wire); err != nil {
				addStatus(taskContextStatus("librarian", handle, nil, err))
				return
			}
			items := sortRawMessages(wire.RelevantItems, taskContextItemLimit)
			sourceMu.Lock()
			response.Librarian = taskContextLibrarian{Query: librarianQuery, RelevantItems: items, Recommendations: wire.Recommendations, Confidence: wire.Confidence}
			response.Truncated.LibrarianItems = len(wire.RelevantItems) > taskContextItemLimit
			sourceMu.Unlock()
			addStatus(taskContextSourceStatus{Source: "librarian", State: "ok", Handle: handle, Retryable: false})
		}()
	} else {
		addStatus(taskContextSourceStatus{Source: "librarian", State: "unavailable", Handle: "librarian", ErrorCode: "den_backend_config_error", Retryable: false})
	}

	wg.Wait()
	sourceMu.Lock()
	response.SearchHints = taskContextSearchHints(response.Task, response.Guidance.Sources, response.Librarian.RelevantItems)
	sort.Slice(response.SourceStatus, func(i, j int) bool { return response.SourceStatus[i].Source < response.SourceStatus[j].Source })
	sourceMu.Unlock()

	responseBody, err := json.Marshal(response)
	if err != nil {
		return Result{}, nil, fmt.Errorf("encoding task context response: %w", err)
	}
	result, err := buildRESTToolResult(responseBody)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func decodeTaskContextArguments(raw json.RawMessage) (taskContextArguments, error) {
	var arguments taskContextArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return taskContextArguments{}, fmt.Errorf("decoding task context arguments: %w", err)
	}
	arguments.ProjectID = strings.TrimSpace(arguments.ProjectID)
	if arguments.ProjectID == "" {
		return taskContextArguments{}, fmt.Errorf("task context route requires project_id")
	}
	if arguments.TaskID <= 0 {
		return taskContextArguments{}, fmt.Errorf("task context route requires task_id")
	}
	return arguments, nil
}

func (c *Client) taskContextGET(ctx context.Context, backend config.BackendConfig, path string, call ToolCall) ([]byte, *Failure, error) {
	request, err := newTaskContextRequest(ctx, http.MethodGet, backend, path, nil)
	if err != nil {
		return nil, nil, err
	}
	return c.taskContextDo(request, backend, call)
}

func (c *Client) taskContextPOST(ctx context.Context, backend config.BackendConfig, path string, value any, call ToolCall) ([]byte, *Failure, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding task context request: %w", err)
	}
	request, err := newTaskContextRequest(ctx, http.MethodPost, backend, path, body)
	if err != nil {
		return nil, nil, err
	}
	return c.taskContextDo(request, backend, call)
}

func newTaskContextRequest(ctx context.Context, method string, backend config.BackendConfig, path string, body []byte) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, backend.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building task context request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if backend.ServiceToken != "" {
		request.Header.Set("Authorization", "Bearer "+backend.ServiceToken)
	}
	return request, nil
}

func (c *Client) taskContextDo(request *http.Request, backend config.BackendConfig, call ToolCall) ([]byte, *Failure, error) {
	response, cancel, err := c.doRESTRequest(request, backend)
	if err != nil {
		return nil, backendFailure(backend.Name, call.Operation, call.ToolName, err, nil), nil
	}
	defer cancel()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading task context response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, statusFailure(backend.Name, call.Operation, call.ToolName, response.StatusCode, body), nil
	}
	return body, nil, nil
}

func taskContextStatus(source, handle string, failure *Failure, err error) taskContextSourceStatus {
	if failure != nil {
		state := "partial"
		if failure.Retryable {
			state = "unavailable"
		}
		return taskContextSourceStatus{Source: source, State: state, Handle: handle, ErrorCode: failure.Error, Retryable: failure.Retryable}
	}
	if err != nil {
		return taskContextSourceStatus{Source: source, State: "partial", Handle: handle, ErrorCode: "den_context_decode_error", Retryable: false}
	}
	return taskContextSourceStatus{Source: source, State: "ok", Handle: handle, Retryable: false}
}

func taskContextLibrarianQuery(task taskContextTask) string {
	parts := append([]string{strings.TrimSpace(task.Title)}, trimStrings(task.Tags)...)
	return strings.Join(parts, " ")
}

func taskContextSearchHints(task taskContextTask, guidance []taskContextDocHandle, items []json.RawMessage) []string {
	hints := append([]string{task.Title}, task.Tags...)
	for _, source := range guidance {
		if source.DocumentProjectID != "" && source.DocumentSlug != "" {
			hints = append(hints, source.DocumentProjectID+"/"+source.DocumentSlug)
		}
		if source.DocumentTitle != "" {
			hints = append(hints, source.DocumentTitle)
		}
	}
	for _, item := range items {
		var reference struct {
			Source   string `json:"source"`
			SourceID string `json:"source_id"`
			Title    string `json:"title"`
		}
		if json.Unmarshal(item, &reference) != nil {
			continue
		}
		if reference.Source != "" && reference.SourceID != "" {
			hints = append(hints, reference.Source+":"+reference.SourceID)
		}
		if reference.Title != "" {
			hints = append(hints, reference.Title)
		}
	}
	return sortedUniqueStrings(hints)
}

func sortedUniqueStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			unique[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func truncateTaskContextText(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	for limit > 0 && (value[limit]&0xc0) == 0x80 {
		limit--
	}
	return value[:limit], true
}

func decodeRawArray(raw json.RawMessage) []json.RawMessage {
	var values []json.RawMessage
	_ = json.Unmarshal(raw, &values)
	return values
}

func sortRawMessages(values []json.RawMessage, limit int) []json.RawMessage {
	result := append([]json.RawMessage(nil), values...)
	sort.Slice(result, func(i, j int) bool { return string(result[i]) < string(result[j]) })
	if len(result) > limit {
		result = result[:limit]
	}
	if result == nil {
		return []json.RawMessage{}
	}
	return result
}

func boolPointer(value bool) *bool { return &value }
