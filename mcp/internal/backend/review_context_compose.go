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
	reviewContextSchema          = "den_review.reviewer_context.v1"
	reviewContextSchemaVersion   = 1
	reviewContextMaxBytes        = 8192
	reviewContextDetailMaxBytes  = 64 * 1024
	reviewContextCampaignLimit   = 8
	reviewContextCampaignTextMax = 256
)

type reviewContextArguments struct {
	TaskID  int64 `json:"task_id"`
	Verbose bool  `json:"verbose"`
}

type reviewContextRound struct {
	ID                      int64                             `json:"id"`
	ProjectID               string                            `json:"project_id"`
	TaskID                  int64                             `json:"task_id"`
	RoundNumber             int                               `json:"round_number"`
	TargetKind              string                            `json:"target_kind,omitempty"`
	Branch                  string                            `json:"branch,omitempty"`
	BaseBranch              string                            `json:"base_branch,omitempty"`
	BaseCommit              string                            `json:"base_commit,omitempty"`
	HeadCommit              string                            `json:"head_commit,omitempty"`
	LastReviewedHeadCommit  string                            `json:"last_reviewed_head_commit,omitempty"`
	PreferredDiffBaseRef    string                            `json:"preferred_diff_base_ref,omitempty"`
	PreferredDiffBaseCommit string                            `json:"preferred_diff_base_commit,omitempty"`
	PreferredDiffHeadRef    string                            `json:"preferred_diff_head_ref,omitempty"`
	PreferredDiffHeadCommit string                            `json:"preferred_diff_head_commit,omitempty"`
	AlternateDiffBaseRef    string                            `json:"alternate_diff_base_ref,omitempty"`
	AlternateDiffBaseCommit string                            `json:"alternate_diff_base_commit,omitempty"`
	AlternateDiffHeadRef    string                            `json:"alternate_diff_head_ref,omitempty"`
	AlternateDiffHeadCommit string                            `json:"alternate_diff_head_commit,omitempty"`
	DeltaBaseCommit         string                            `json:"delta_base_commit,omitempty"`
	CampaignChildren        []reviewContextCampaignChild      `json:"campaign_children,omitempty"`
	CampaignRepositories    []reviewContextCampaignRepository `json:"campaign_repositories,omitempty"`
	CampaignDetailRef       string                            `json:"campaign_detail_ref,omitempty"`
	Verdict                 string                            `json:"verdict,omitempty"`
}

type reviewContextCampaignChild struct {
	ProjectID       string `json:"project_id"`
	TaskID          int64  `json:"task_id"`
	ReviewRoundID   int64  `json:"review_round_id"`
	HeadCommit      string `json:"head_commit,omitempty"`
	MembershipKind  string `json:"membership_kind,omitempty"`
	ApprovedVerdict string `json:"approved_verdict,omitempty"`
}

type reviewContextCampaignRepository struct {
	Repository string `json:"repository"`
	HeadSHA    string `json:"head_sha"`
}

type reviewContextTask struct {
	ID               int64  `json:"id"`
	ProjectID        string `json:"project_id"`
	Title            string `json:"title,omitempty"`
	Status           string `json:"status"`
	Repository       string `json:"repository,omitempty"`
	RootPath         string `json:"root_path,omitempty"`
	RepositoryHandle string `json:"repository_handle,omitempty"`
}

type reviewContextProject struct {
	RootPath     string          `json:"root_path,omitempty"`
	SettingsJSON json.RawMessage `json:"settings_json,omitempty"`
}

type reviewContextGate struct {
	ID                int64    `json:"id"`
	Repository        string   `json:"repository,omitempty"`
	Status            string   `json:"status"`
	RequiredChecks    []string `json:"required_checks,omitempty"`
	TerminalReason    string   `json:"terminal_reason,omitempty"`
	EvidenceMessageID *int64   `json:"evidence_message_id,omitempty"`
}

type reviewContextTruncation struct {
	Findings bool `json:"findings,omitempty"`
	Packets  bool `json:"packets,omitempty"`
	Guidance bool `json:"guidance,omitempty"`
	Campaign bool `json:"campaign,omitempty"`
	Bounded  bool `json:"bounded,omitempty"`
}

type reviewContextResponse struct {
	SchemaVersion    int                                  `json:"schema_version"`
	Schema           string                               `json:"schema"`
	ProjectID        string                               `json:"project_id"`
	TaskID           int64                                `json:"task_id"`
	Task             reviewContextTask                    `json:"task"`
	CurrentRound     *reviewContextRound                  `json:"current_round,omitempty"`
	CurrentStatus    string                               `json:"current_status"`
	NextState        string                               `json:"next_state"`
	MaterialDigest   string                               `json:"material_digest"`
	PriorFindings    []json.RawMessage                    `json:"prior_findings,omitempty"`
	Gate             *reviewContextGate                   `json:"gate,omitempty"`
	PacketHeaders    map[string]*taskWorkflowPacketHeader `json:"packet_headers,omitempty"`
	Guidance         []taskContextDocHandle               `json:"guidance_handles,omitempty"`
	DetailRefs       *reviewContextDetailRefs             `json:"detail_refs,omitempty"`
	ExpandedFindings []json.RawMessage                    `json:"expanded_findings,omitempty"`
	ExpandedPackets  map[string]json.RawMessage           `json:"expanded_packets,omitempty"`
	ExpandedGuidance *guidancePacketResponse              `json:"expanded_guidance,omitempty"`
	SourceStatus     []taskContextSourceStatus            `json:"source_status"`
	Truncation       reviewContextTruncation              `json:"truncation,omitempty"`
}

type reviewContextDetailRefs struct {
	Findings string `json:"findings,omitempty"`
	Packets  string `json:"packets,omitempty"`
	Guidance string `json:"guidance,omitempty"`
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
		Task:          reviewContextTask{ID: taskDetail.Task.ID, ProjectID: projectID, Title: taskDetail.Task.Title, Status: taskDetail.Task.Status},
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
		if !arguments.Verbose && boundReviewContextCampaign(&round) {
			round.CampaignDetailRef = "/v1/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/review/campaign-details"
			response.Truncation.Campaign = true
		}
		response.CurrentRound = &round
		allFindings := decodeRawArray(summary.OpenFindings)
		response.PriorFindings = summarizeReviewContextFindings(sortRawMessages(allFindings, 32),
			"/v1/tasks/"+strconv.FormatInt(arguments.TaskID, 10)+"/review/findings")
		response.Truncation.Findings = len(allFindings) > len(response.PriorFindings)
		if round.HeadCommit != "" {
			gatePath := "/v1/projects/" + url.PathEscape(projectID) + "/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/review/github-check-gates/" + url.PathEscape(round.HeadCommit)
			gateBody, gateFailure, gateErr := c.taskContextGET(ctx, reviewBackend, gatePath, call)
			if gateErr == nil && gateFailure == nil {
				var gate reviewContextGate
				if err := json.Unmarshal(gateBody, &gate); err != nil {
					return Result{}, nil, fmt.Errorf("parsing review context gate: %w", err)
				}
				response.Gate = &gate
				response.Task.Repository = strings.TrimSpace(gate.Repository)
				response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "gate", State: "ok", Handle: gatePath, Retryable: false})
			} else if gateFailure != nil && gateFailure.StatusCode != nil && *gateFailure.StatusCode == 404 {
				response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "gate", State: "absent", Handle: gatePath, ErrorCode: "no_current_gate", Retryable: false})
			} else {
				response.SourceStatus = append(response.SourceStatus, taskContextStatus("gate", gatePath, gateFailure, gateErr))
			}
		}
		response.NextState = reviewContextNextState(taskDetail.Task.Status, round, summary, response.Gate)
	} else {
		return c.reviewContextTypedError(reviewContextErrorResponse{
			SchemaVersion: reviewContextSchemaVersion, Schema: reviewContextSchema, TaskID: arguments.TaskID,
			ErrorCode: "review_context_unavailable", Reason: "no_current_round", Retryable: false,
		})
	}
	if projectsBackend, ok := backends["projects"]; ok {
		projectPath := "/v1/projects/" + url.PathEscape(projectID)
		projectBody, projectFailure, projectErr := c.taskContextGET(ctx, projectsBackend, projectPath, call)
		if projectErr == nil && projectFailure == nil {
			var project reviewContextProject
			if err := json.Unmarshal(projectBody, &project); err != nil {
				return Result{}, nil, fmt.Errorf("parsing review context project: %w", err)
			}
			response.Task.RootPath = strings.TrimSpace(project.RootPath)
			var settings struct {
				Repository string `json:"repository"`
			}
			if len(project.SettingsJSON) > 0 {
				_ = json.Unmarshal(project.SettingsJSON, &settings)
			}
			if response.Task.Repository == "" {
				response.Task.Repository = strings.TrimSpace(settings.Repository)
			}
			response.SourceStatus = append(response.SourceStatus, taskContextSourceStatus{Source: "project", State: "ok", Handle: projectPath, Retryable: false})
		} else {
			response.SourceStatus = append(response.SourceStatus, taskContextStatus("project", projectPath, projectFailure, projectErr))
		}
	}
	if response.Task.RootPath != "" {
		response.Task.RepositoryHandle = response.Task.RootPath
	} else {
		response.Task.RepositoryHandle = response.Task.Repository
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
			if arguments.Verbose {
				response.ExpandedGuidance = &guidance
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
	if arguments.Verbose {
		findingsPath := "/v1/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/review/findings"
		findingsBody, findingsFailure, findingsErr := c.taskContextGET(ctx, reviewBackend, findingsPath, call)
		if findingsErr == nil && findingsFailure == nil {
			var findings []json.RawMessage
			if err := json.Unmarshal(findingsBody, &findings); err != nil {
				return Result{}, nil, fmt.Errorf("parsing expanded review findings: %w", err)
			}
			response.ExpandedFindings = findings
		} else if findingsFailure == nil || findingsFailure.StatusCode == nil || *findingsFailure.StatusCode != 404 {
			response.SourceStatus = append(response.SourceStatus, taskContextStatus("review_findings", findingsPath, findingsFailure, findingsErr))
		}
		response.ExpandedPackets = make(map[string]json.RawMessage)
		for _, query := range []taskWorkflowPacketQuery{
			{Key: "review_request", PacketType: "review_request", Role: "reviewer"},
			{Key: "rereview_request", PacketType: "rereview_request", Role: "reviewer"},
			{Key: "review_findings", PacketType: "review_findings", Role: "reviewer"},
			{Key: "implementer_response", PacketType: "implementer_response", Role: "implementer"},
		} {
			path := "/v1/projects/" + url.PathEscape(projectID) + "/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/packets/latest?packet_type=" + url.QueryEscape(query.PacketType) + "&role=" + url.QueryEscape(query.Role)
			body, packetFailure, packetErr := c.taskWorkflowOptionalGET(ctx, messagesBackend, path, call)
			if packetErr == nil && packetFailure == nil {
				response.ExpandedPackets[query.Key] = body
			} else if packetFailure == nil || packetFailure.StatusCode == nil || *packetFailure.StatusCode != 404 {
				response.SourceStatus = append(response.SourceStatus, taskContextStatus("expanded_packets", path, packetFailure, packetErr))
			}
		}
		response.DetailRefs = nil
	} else {
		response.DetailRefs = &reviewContextDetailRefs{
			Findings: "/v1/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/review/findings",
			Packets:  "/v1/projects/" + url.PathEscape(projectID) + "/tasks/" + strconv.FormatInt(arguments.TaskID, 10) + "/packets/latest",
			Guidance: guidancePath,
		}
	}
	if len(response.Guidance) > 16 {
		response.Guidance = response.Guidance[:16]
		response.Truncation.Guidance = true
	}
	byteLimit := reviewContextMaxBytes
	if arguments.Verbose {
		byteLimit = reviewContextDetailMaxBytes
	}
	encoded, err := boundReviewContext(&response, byteLimit)
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

func reviewContextNextState(taskStatus string, round reviewContextRound, _ taskWorkflowReviewSummary, gate *reviewContextGate) string {
	if taskStatus != "review" && taskStatus != "in_progress" {
		return "not_reviewable"
	}
	if round.Verdict != "" {
		return "round_superseded"
	}
	if gate == nil {
		return "gate_pending"
	}
	switch gate.Status {
	case "passed":
		return "source_review_ready"
	case "failed", "timed_out":
		return "gate_failed"
	case "superseded":
		return "round_superseded"
	default:
		return "gate_pending"
	}
}

func reviewContextDigest(response reviewContextResponse) string {
	response.MaterialDigest = ""
	response.SourceStatus = nil
	response.DetailRefs = nil
	if response.CurrentRound != nil {
		round := *response.CurrentRound
		round.CampaignDetailRef = ""
		response.CurrentRound = &round
	}
	data, _ := json.Marshal(response)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func boundReviewContextCampaign(round *reviewContextRound) bool {
	truncated := false
	if len(round.CampaignChildren) > reviewContextCampaignLimit {
		round.CampaignChildren = round.CampaignChildren[:reviewContextCampaignLimit]
		truncated = true
	}
	if len(round.CampaignRepositories) > reviewContextCampaignLimit {
		round.CampaignRepositories = round.CampaignRepositories[:reviewContextCampaignLimit]
		truncated = true
	}
	for index := range round.CampaignChildren {
		child := &round.CampaignChildren[index]
		values := []*string{&child.ProjectID, &child.HeadCommit, &child.MembershipKind, &child.ApprovedVerdict}
		for _, value := range values {
			clipped := truncateReviewContextText(*value, reviewContextCampaignTextMax)
			if clipped != *value {
				*value = clipped
				truncated = true
			}
		}
	}
	for index := range round.CampaignRepositories {
		repository := &round.CampaignRepositories[index]
		values := []*string{&repository.Repository, &repository.HeadSHA}
		for _, value := range values {
			clipped := truncateReviewContextText(*value, reviewContextCampaignTextMax)
			if clipped != *value {
				*value = clipped
				truncated = true
			}
		}
	}
	return truncated
}

func truncateReviewContextText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func boundReviewContext(response *reviewContextResponse, limits ...int) ([]byte, error) {
	byteLimit := reviewContextMaxBytes
	if len(limits) > 0 && limits[0] > 0 {
		byteLimit = limits[0]
	}
	response.MaterialDigest = reviewContextDigest(*response)
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encoding review context: %w", err)
	}
	if len(encoded) <= byteLimit {
		return encoded, nil
	}
	if response.ExpandedFindings != nil || response.ExpandedPackets != nil || response.ExpandedGuidance != nil {
		return nil, fmt.Errorf("expanded review context exceeds %d-byte detail budget: %d", byteLimit, len(encoded))
	}
	response.Truncation.Bounded = true
	response.Truncation.Guidance = len(response.Guidance) > 0
	response.Guidance = nil
	for key, header := range response.PacketHeaders {
		if header == nil {
			continue
		}
		compact := *header
		compact.Metadata = nil
		response.PacketHeaders[key] = &compact
	}
	response.Truncation.Packets = len(response.PacketHeaders) > 0
	encoded, err = json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encoding bounded review context: %w", err)
	}
	if len(encoded) > byteLimit {
		for index := range response.PriorFindings {
			response.PriorFindings[index] = compactReviewFinding(response.PriorFindings[index], 96, 96)
		}
		encoded, err = json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("encoding compacted review context: %w", err)
		}
	}
	if len(encoded) > byteLimit {
		return nil, fmt.Errorf("review context exceeds %d-byte budget after deterministic compaction: %d", byteLimit, len(encoded))
	}
	response.MaterialDigest = reviewContextDigest(*response)
	encoded, err = json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encoding compacted review context with digest: %w", err)
	}
	if len(encoded) > byteLimit {
		return nil, fmt.Errorf("review context exceeds %d-byte budget after material digest: %d", byteLimit, len(encoded))
	}
	return encoded, nil
}

func summarizeReviewContextFindings(findings []json.RawMessage, detailRef string) []json.RawMessage {
	result := make([]json.RawMessage, 0, len(findings))
	for _, finding := range findings {
		result = append(result, compactReviewFindingWithRef(finding, 256, 256, detailRef))
	}
	return result
}

func compactReviewFinding(raw json.RawMessage, summaryLimit, notesLimit int) json.RawMessage {
	return compactReviewFindingWithRef(raw, summaryLimit, notesLimit, "")
}

func compactReviewFindingWithRef(raw json.RawMessage, summaryLimit, notesLimit int, detailRef string) json.RawMessage {
	var source map[string]json.RawMessage
	if json.Unmarshal(raw, &source) != nil {
		return json.RawMessage(`{"status":"unavailable"}`)
	}
	compact := make(map[string]json.RawMessage)
	for _, key := range []string{"id", "finding_key", "category", "status"} {
		if value, ok := source[key]; ok {
			compact[key] = value
		}
	}
	for key, limit := range map[string]int{"summary": summaryLimit, "status_notes": notesLimit, "response_notes": notesLimit} {
		value, ok := source[key]
		if !ok {
			continue
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			continue
		}
		if len([]rune(text)) > limit {
			text = string([]rune(text)[:limit])
		}
		encoded, _ := json.Marshal(text)
		compact[key] = encoded
	}
	if detailRef != "" {
		encoded, _ := json.Marshal(detailRef)
		compact["detail_ref"] = encoded
	}
	encoded, err := json.Marshal(compact)
	if err != nil {
		return json.RawMessage(`{"status":"unavailable"}`)
	}
	return encoded
}
