package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"den-services/mcp/internal/config"
)

const (
	reviewContextSchema        = "den_review.reviewer_context.v1"
	reviewContextSchemaVersion = 1
	reviewContextMaxBytes      = 8192
)

type reviewContextArguments struct {
	TaskID int64 `json:"task_id"`
}

type reviewContextRound struct {
	ID                     int64  `json:"id"`
	ProjectID              string `json:"project_id"`
	TaskID                 int64  `json:"task_id"`
	RoundNumber            int    `json:"round_number"`
	Branch                 string `json:"branch,omitempty"`
	BaseBranch             string `json:"base_branch,omitempty"`
	BaseCommit             string `json:"base_commit,omitempty"`
	HeadCommit             string `json:"head_commit,omitempty"`
	LastReviewedHeadCommit string `json:"last_reviewed_head_commit,omitempty"`
	DeltaBaseCommit        string `json:"delta_base_commit,omitempty"`
	Verdict                string `json:"verdict,omitempty"`
}

type reviewContextTask struct {
	ID               int64  `json:"id"`
	ProjectID        string `json:"project_id"`
	Title            string `json:"title,omitempty"`
	Status           string `json:"status"`
	RepositoryHandle string `json:"repository_handle"`
}

type reviewContextGate struct {
	ID                int64    `json:"id"`
	Status            string   `json:"status"`
	RequiredChecks    []string `json:"required_checks,omitempty"`
	TerminalReason    string   `json:"terminal_reason,omitempty"`
	EvidenceMessageID *int64   `json:"evidence_message_id,omitempty"`
}

type reviewContextTruncation struct {
	Findings bool `json:"findings,omitempty"`
	Packets  bool `json:"packets,omitempty"`
	Guidance bool `json:"guidance,omitempty"`
	Bounded  bool `json:"bounded,omitempty"`
}

type reviewContextResponse struct {
	SchemaVersion  int                                  `json:"schema_version"`
	Schema         string                               `json:"schema"`
	ProjectID      string                               `json:"project_id"`
	TaskID         int64                                `json:"task_id"`
	Task           reviewContextTask                    `json:"task"`
	CurrentRound   *reviewContextRound                  `json:"current_round,omitempty"`
	CurrentStatus  string                               `json:"current_status"`
	NextState      string                               `json:"next_state"`
	MaterialDigest string                               `json:"material_digest"`
	PriorFindings  []json.RawMessage                    `json:"prior_findings,omitempty"`
	Gate           *reviewContextGate                   `json:"gate,omitempty"`
	PacketHeaders  map[string]*taskWorkflowPacketHeader `json:"packet_headers,omitempty"`
	Guidance       []taskContextDocHandle               `json:"guidance_handles,omitempty"`
	SourceStatus   []taskContextSourceStatus            `json:"source_status"`
	Truncation     reviewContextTruncation              `json:"truncation,omitempty"`
}

type reviewContextErrorResponse struct {
	SchemaVersion int    `json:"schema_version"`
	Schema        string `json:"schema"`
	TaskID        int64  `json:"task_id"`
	ErrorCode     string `json:"error_code"`
	Reason        string `json:"reason"`
	Retryable     bool   `json:"retryable"`
}

func (c *Client) callReviewContextCompose(ctx context.Context, backends map[string]config.BackendConfig, _ Route, call ToolCall) (Result, *Failure, error) {
	arguments, err := decodeReviewContextArguments(call.Arguments)
	if err != nil {
		return Result{}, nil, err
	}
	tasksBackend, reviewBackend, messagesBackend, err := taskWorkflowBackends(backends)
	if err != nil {
		return Result{}, nil, err
	}
	taskPath := "/v1/tasks/" + strconv.FormatInt(arguments.TaskID, 10)
	taskBody, failure, err := c.taskContextGET(ctx, tasksBackend, taskPath, call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}
	var taskDetail taskContextTaskDetail
	if err := json.Unmarshal(taskBody, &taskDetail); err != nil {
		return Result{}, nil, fmt.Errorf("parsing review context task: %w", err)
	}
	if taskDetail.Task.ID != arguments.TaskID || strings.TrimSpace(taskDetail.Task.ProjectID) == "" {
		return Result{}, nil, fmt.Errorf("review context task is missing canonical identity")
	}
	projectID := strings.TrimSpace(taskDetail.Task.ProjectID)
	reviewPath := "/v1/projects/" + url.PathEscape(projectID) + "/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/review/workflow-summary"
	reviewBody, failure, err := c.taskContextGET(ctx, reviewBackend, reviewPath, call)
	if err != nil || failure != nil {
		return Result{}, failure, err
	}
	var summary taskWorkflowReviewSummary
	if err := json.Unmarshal(reviewBody, &summary); err != nil {
		return Result{}, nil, fmt.Errorf("parsing review context workflow: %w", err)
	}
	response := reviewContextResponse{
		Schema: reviewContextSchema, SchemaVersion: reviewContextSchemaVersion,
		ProjectID: projectID, TaskID: arguments.TaskID,
		Task: reviewContextTask{ID: taskDetail.Task.ID, ProjectID: projectID, Title: taskDetail.Task.Title, Status: taskDetail.Task.Status,
			RepositoryHandle: reviewPath},
		CurrentStatus: taskDetail.Task.Status, NextState: "not_reviewable",
		PacketHeaders: make(map[string]*taskWorkflowPacketHeader), Guidance: []taskContextDocHandle{},
		SourceStatus: []taskContextSourceStatus{
			{Source: "task", State: "ok", Handle: taskPath, Retryable: false},
			{Source: "review", State: "ok", Handle: reviewPath, Retryable: false},
		},
	}
	if len(summary.CurrentRound) > 0 && string(summary.CurrentRound) != "null" {
		var round reviewContextRound
		if err := json.Unmarshal(summary.CurrentRound, &round); err != nil {
			return Result{}, nil, fmt.Errorf("parsing review context current round: %w", err)
		}
		response.CurrentRound = &round
		response.NextState = reviewContextNextState(taskDetail.Task.Status, round, summary)
		response.PriorFindings = sortRawMessages(decodeRawArray(summary.OpenFindings), 32)
		if round.HeadCommit != "" {
			gatePath := "/v1/projects/" + url.PathEscape(projectID) + "/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/review/github-check-gates/" + url.PathEscape(round.HeadCommit)
			gateBody, gateFailure, gateErr := c.taskContextGET(ctx, reviewBackend, gatePath, call)
			if gateErr == nil && gateFailure == nil {
				var gate reviewContextGate
				if err := json.Unmarshal(gateBody, &gate); err != nil {
					return Result{}, nil, fmt.Errorf("parsing review context gate: %w", err)
				}
				response.Gate = &gate
				response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "gate", State: "ok", Handle: gatePath, Retryable: false})
			} else if gateFailure != nil && gateFailure.StatusCode != nil && *gateFailure.StatusCode == 404 {
				response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "gate", State: "absent", Handle: gatePath, ErrorCode: "no_current_gate", Retryable: false})
			} else {
				response.SourceStatus = append(response.SourceStatus, taskContextStatus("gate", gatePath, gateFailure, gateErr))
			}
		}
	} else {
		return c.reviewContextTypedError(reviewContextErrorResponse{
			SchemaVersion: reviewContextSchemaVersion, Schema: reviewContextSchema, TaskID: arguments.TaskID,
			ErrorCode: "review_context_unavailable", Reason: "no_current_round", Retryable: false,
		})
	}
	for _, query := range []taskWorkflowPacketQuery{
		{Key: "review_request", PacketType: "review_request", Role: "reviewer"},
		{Key: "rereview_request", PacketType: "rereview_request", Role: "reviewer"},
		{Key: "review_findings", PacketType: "review_findings", Role: "reviewer"},
		{Key: "implementer_response", PacketType: "implementer_response", Role: "implementer"},
	} {
		header, warning := c.taskWorkflowLatestPacket(ctx, messagesBackend, projectID, arguments.TaskID, query, call)
		response.PacketHeaders[query.Key] = header
		if warning != nil {
			response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "messages", State: "partial", Handle: "packets/latest", ErrorCode: "packet_header_unavailable", Retryable: false})
		}
	}
	guidanceBackend, guidanceOK := backends["guidance"]
	guidancePath := "/v1/projects/" + url.PathEscape(projectID) + "/agent-guidance"
	if guidanceOK {
		body, guidanceFailure, guidanceErr := c.taskContextGET(ctx, guidanceBackend, guidancePath, call)
		if guidanceErr == nil && guidanceFailure == nil {
			var guidance guidancePacketResponse
			if err := json.Unmarshal(body, &guidance); err != nil {
				return Result{}, nil, fmt.Errorf("parsing review context guidance: %w", err)
			}
			for _, source := range guidance.Sources {
				response.Guidance = append(response.Guidance, taskContextDocHandle{SourceScope: source.SourceScope, DocumentProjectID: source.DocumentProjectID, DocumentSlug: source.DocumentSlug, DocumentTitle: source.DocumentTitle, DocumentType: source.DocumentType, DocumentUpdatedAt: source.DocumentUpdatedAt, Visibility: source.Visibility, Tags: source.Tags, Importance: source.Importance, Audience: source.Audience, SortOrder: source.SortOrder, Notes: source.Notes})
			}
			response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "guidance", State: "ok", Handle: guidancePath, Retryable: false})
		} else {
			response.SourceStatus = append(response.SourceStatus, taskContextStatus("guidance", guidancePath, guidanceFailure, guidanceErr))
		}
	} else {
		response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "guidance", State: "unavailable", Handle: "guidance", ErrorCode: "den_backend_config_error", Retryable: false})
	}
	if len(response.PriorFindings) > 0 && len(decodeRawArray(summary.OpenFindings)) > len(response.PriorFindings) {
		response.Truncation.Findings = true
	}
	if len(response.Guidance) > 16 {
		response.Guidance = response.Guidance[:16]
		response.Truncation.Guidance = true
	}
	response.MaterialDigest = reviewContextDigest(response)
	encoded, err := boundReviewContext(&response)
	if err != nil {
		return c.reviewContextTypedError(reviewContextErrorResponse{
			SchemaVersion: reviewContextSchemaVersion, Schema: reviewContextSchema, TaskID: arguments.TaskID,
			ErrorCode: "review_context_too_large", Reason: err.Error(), Retryable: false,
		})
	}
	result, err := buildRESTToolResult(encoded)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func (c *Client) reviewContextTypedError(response reviewContextErrorResponse) (Result, *Failure, error) {
	body, err := json.Marshal(response)
	if err != nil {
		return Result{}, nil, fmt.Errorf("encoding review context error: %w", err)
	}
	result, err := buildRESTToolResult(body)
	if err != nil {
		return Result{}, nil, err
	}
	return Result{Value: result}, nil, nil
}

func decodeReviewContextArguments(raw json.RawMessage) (reviewContextArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments reviewContextArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return reviewContextArguments{}, fmt.Errorf("decoding review context arguments: %w", err)
	}
	if arguments.TaskID <= 0 {
		return reviewContextArguments{}, fmt.Errorf("review context requires task_id")
	}
	return arguments, nil
}

func reviewContextNextState(taskStatus string, round reviewContextRound, summary taskWorkflowReviewSummary) string {
	if taskStatus != "review" && taskStatus != "in_progress" {
		return "not_reviewable"
	}
	if round.Verdict != "" {
		return "round_superseded"
	}
	if len(summary.OpenFindings) > 0 {
		return "source_review_ready"
	}
	return "gate_pending"
}

func reviewContextDigest(response reviewContextResponse) string {
	response.MaterialDigest = ""
	response.SourceStatus = nil
	data, _ := json.Marshal(response)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func boundReviewContext(response *reviewContextResponse) ([]byte, error) {
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encoding review context: %w", err)
	}
	if len(encoded) <= reviewContextMaxBytes {
		return encoded, nil
	}
	response.Truncation = reviewContextTruncation{Findings: len(response.PriorFindings) > 0, Packets: len(response.PacketHeaders) > 0, Guidance: len(response.Guidance) > 0, Bounded: true}
	response.PriorFindings = nil
	response.PacketHeaders = nil
	response.Guidance = nil
	encoded, err = json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encoding bounded review context: %w", err)
	}
	if len(encoded) > reviewContextMaxBytes {
		return nil, fmt.Errorf("review context exceeds %d-byte budget after deterministic compaction: %d", reviewContextMaxBytes, len(encoded))
	}
	return encoded, nil
}
