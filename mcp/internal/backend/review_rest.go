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

type reviewToolArguments struct {
	ProjectID               string          `json:"project_id"`
	TaskID                  int64           `json:"task_id"`
	ReviewRoundID           int64           `json:"review_round_id"`
	ReviewFindingID         int64           `json:"review_finding_id"`
	RequestedBy             string          `json:"requested_by"`
	Branch                  string          `json:"branch"`
	BaseBranch              string          `json:"base_branch"`
	BaseCommit              string          `json:"base_commit"`
	HeadCommit              string          `json:"head_commit"`
	LastReviewedHeadCommit  string          `json:"last_reviewed_head_commit"`
	CommitsSinceLastReview  *int            `json:"commits_since_last_review"`
	TestsRun                json.RawMessage `json:"tests_run"`
	Notes                   string          `json:"notes"`
	PreferredDiffBaseRef    string          `json:"preferred_diff_base_ref"`
	PreferredDiffBaseCommit string          `json:"preferred_diff_base_commit"`
	PreferredDiffHeadRef    string          `json:"preferred_diff_head_ref"`
	PreferredDiffHeadCommit string          `json:"preferred_diff_head_commit"`
	AlternateDiffBaseRef    string          `json:"alternate_diff_base_ref"`
	AlternateDiffBaseCommit string          `json:"alternate_diff_base_commit"`
	AlternateDiffHeadRef    string          `json:"alternate_diff_head_ref"`
	AlternateDiffHeadCommit string          `json:"alternate_diff_head_commit"`
	DeltaBaseCommit         string          `json:"delta_base_commit"`
	InheritedCommitCount    *int            `json:"inherited_commit_count"`
	TaskLocalCommitCount    *int            `json:"task_local_commit_count"`
	ThreadID                *int64          `json:"thread_id"`
	RunID                   string          `json:"run_id"`
	SubagentRole            string          `json:"subagent_role"`
	Sender                  string          `json:"sender"`
	CreatedBy               string          `json:"created_by"`
	Category                string          `json:"category"`
	Summary                 string          `json:"summary"`
	FileReferences          json.RawMessage `json:"file_references"`
	TestCommands            json.RawMessage `json:"test_commands"`
	Verdict                 string          `json:"verdict"`
	DecidedBy               string          `json:"decided_by"`
	RespondedBy             string          `json:"responded_by"`
	ResponseNotes           string          `json:"response_notes"`
	Status                  string          `json:"status"`
	StatusNotes             string          `json:"status_notes"`
	UpdatedBy               string          `json:"updated_by"`
	FollowUpTaskID          *int64          `json:"follow_up_task_id"`
	FindingIDs              json.RawMessage `json:"finding_ids"`
	SplitBy                 string          `json:"split_by"`
	FollowUpTitle           string          `json:"follow_up_title"`
	FollowUpParentTaskID    *int64          `json:"follow_up_parent_task_id"`
	FollowUpPriority        *int            `json:"follow_up_priority"`
	FollowUpAssignedTo      string          `json:"follow_up_assigned_to"`
	FollowUpTags            json.RawMessage `json:"follow_up_tags"`
	OverrideBlocking        bool            `json:"override_blocking"`
	IdempotencyKey          string          `json:"idempotency_key"`
	Resolved                *bool           `json:"resolved"`
	Repository              string          `json:"repository"`
	CommitSHA               string          `json:"commit_sha"`
	Ref                     string          `json:"ref"`
	RequiredChecks          json.RawMessage `json:"required_checks"`
	TimeoutSeconds          *int            `json:"timeout_seconds"`
	PollIntervalSeconds     *int            `json:"poll_interval_seconds"`
	AgentProfile            string          `json:"agent_profile"`
	AgentInstanceID         string          `json:"agent_instance_id"`
	SessionKey              string          `json:"session_key"`
}

type reviewRoundBody struct {
	RequestedBy             string   `json:"requested_by"`
	Branch                  string   `json:"branch"`
	BaseBranch              string   `json:"base_branch"`
	BaseCommit              string   `json:"base_commit"`
	HeadCommit              string   `json:"head_commit"`
	LastReviewedHeadCommit  string   `json:"last_reviewed_head_commit,omitempty"`
	CommitsSinceLastReview  *int     `json:"commits_since_last_review,omitempty"`
	TestsRun                []string `json:"tests_run,omitempty"`
	Notes                   string   `json:"notes,omitempty"`
	PreferredDiffBaseRef    string   `json:"preferred_diff_base_ref,omitempty"`
	PreferredDiffBaseCommit string   `json:"preferred_diff_base_commit,omitempty"`
	PreferredDiffHeadRef    string   `json:"preferred_diff_head_ref,omitempty"`
	PreferredDiffHeadCommit string   `json:"preferred_diff_head_commit,omitempty"`
	AlternateDiffBaseRef    string   `json:"alternate_diff_base_ref,omitempty"`
	AlternateDiffBaseCommit string   `json:"alternate_diff_base_commit,omitempty"`
	AlternateDiffHeadRef    string   `json:"alternate_diff_head_ref,omitempty"`
	AlternateDiffHeadCommit string   `json:"alternate_diff_head_commit,omitempty"`
	DeltaBaseCommit         string   `json:"delta_base_commit,omitempty"`
	InheritedCommitCount    *int     `json:"inherited_commit_count,omitempty"`
	TaskLocalCommitCount    *int     `json:"task_local_commit_count,omitempty"`
	ThreadID                *int64   `json:"thread_id,omitempty"`
	RunID                   string   `json:"run_id,omitempty"`
}

type postReviewFindingsBody struct {
	ReviewRoundID int64  `json:"review_round_id"`
	Sender        string `json:"sender"`
	ThreadID      *int64 `json:"thread_id,omitempty"`
	Notes         string `json:"notes,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	SubagentRole  string `json:"subagent_role,omitempty"`
}

type createReviewFindingBody struct {
	CreatedBy      string   `json:"created_by"`
	Category       string   `json:"category"`
	Summary        string   `json:"summary"`
	Notes          string   `json:"notes,omitempty"`
	FileReferences []string `json:"file_references,omitempty"`
	TestCommands   []string `json:"test_commands,omitempty"`
	RunID          string   `json:"run_id,omitempty"`
	SubagentRole   string   `json:"subagent_role,omitempty"`
}

type reviewVerdictBody struct {
	Verdict      string `json:"verdict"`
	DecidedBy    string `json:"decided_by"`
	Notes        string `json:"notes,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	SubagentRole string `json:"subagent_role,omitempty"`
}

type respondReviewFindingBody struct {
	RespondedBy    string `json:"responded_by"`
	ResponseNotes  string `json:"response_notes,omitempty"`
	Status         string `json:"status,omitempty"`
	StatusNotes    string `json:"status_notes,omitempty"`
	FollowUpTaskID *int64 `json:"follow_up_task_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	SubagentRole   string `json:"subagent_role,omitempty"`
}

type reviewFindingStatusBody struct {
	Status         string `json:"status"`
	UpdatedBy      string `json:"updated_by"`
	Notes          string `json:"notes,omitempty"`
	FollowUpTaskID *int64 `json:"follow_up_task_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	SubagentRole   string `json:"subagent_role,omitempty"`
}

type splitReviewFindingsBody struct {
	FindingIDs           []int64  `json:"finding_ids"`
	SplitBy              string   `json:"split_by"`
	FollowUpTitle        string   `json:"follow_up_title,omitempty"`
	FollowUpParentTaskID *int64   `json:"follow_up_parent_task_id,omitempty"`
	FollowUpPriority     int      `json:"follow_up_priority,omitempty"`
	FollowUpAssignedTo   string   `json:"follow_up_assigned_to,omitempty"`
	FollowUpTags         []string `json:"follow_up_tags,omitempty"`
	OverrideBlocking     bool     `json:"override_blocking,omitempty"`
	IdempotencyKey       string   `json:"idempotency_key,omitempty"`
}

type githubCheckGateBody struct {
	Repository          string   `json:"repository"`
	CommitSHA           string   `json:"commit_sha"`
	Ref                 string   `json:"ref"`
	RequiredChecks      []string `json:"required_checks"`
	TimeoutSeconds      *int     `json:"timeout_seconds,omitempty"`
	PollIntervalSeconds *int     `json:"poll_interval_seconds,omitempty"`
	RequestedBy         string   `json:"requested_by"`
	AgentProfile        string   `json:"agent_profile,omitempty"`
	AgentInstanceID     string   `json:"agent_instance_id,omitempty"`
	SessionKey          string   `json:"session_key,omitempty"`
}

func (c *Client) callReviewREST(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (Result, *Failure, error) {
	request, err := buildReviewRESTRequest(ctx, backend, route, call)
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
		return Result{}, nil, fmt.Errorf("reading review backend response: %w", err)
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

func buildReviewRESTRequest(ctx context.Context, backend config.BackendConfig, route Route, call ToolCall) (*http.Request, error) {
	arguments, err := decodeReviewToolArguments(call.Arguments)
	if err != nil {
		return nil, err
	}
	requestBody, err := reviewRESTRequestBody(route.Operation, arguments)
	if err != nil {
		return nil, err
	}
	requestURL, err := reviewRESTURL(backend.BaseURL, route, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, route.Method, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("building review backend request: %w", err)
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

func decodeReviewToolArguments(raw json.RawMessage) (reviewToolArguments, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var arguments reviewToolArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return reviewToolArguments{}, fmt.Errorf("decoding review tool arguments: %w", err)
	}
	return arguments, nil
}

func reviewRESTRequestBody(operation string, arguments reviewToolArguments) ([]byte, error) {
	switch operation {
	case "create_review_round", "request_review":
		testsRun, err := parseStringList(arguments.TestsRun)
		if err != nil {
			return nil, err
		}
		return json.Marshal(reviewRoundBody{
			RequestedBy: strings.TrimSpace(arguments.RequestedBy), Branch: strings.TrimSpace(arguments.Branch),
			BaseBranch: strings.TrimSpace(arguments.BaseBranch), BaseCommit: strings.TrimSpace(arguments.BaseCommit),
			HeadCommit: strings.TrimSpace(arguments.HeadCommit), LastReviewedHeadCommit: strings.TrimSpace(arguments.LastReviewedHeadCommit),
			CommitsSinceLastReview: arguments.CommitsSinceLastReview, TestsRun: testsRun, Notes: strings.TrimSpace(arguments.Notes),
			PreferredDiffBaseRef: strings.TrimSpace(arguments.PreferredDiffBaseRef), PreferredDiffBaseCommit: strings.TrimSpace(arguments.PreferredDiffBaseCommit),
			PreferredDiffHeadRef: strings.TrimSpace(arguments.PreferredDiffHeadRef), PreferredDiffHeadCommit: strings.TrimSpace(arguments.PreferredDiffHeadCommit),
			AlternateDiffBaseRef: strings.TrimSpace(arguments.AlternateDiffBaseRef), AlternateDiffBaseCommit: strings.TrimSpace(arguments.AlternateDiffBaseCommit),
			AlternateDiffHeadRef: strings.TrimSpace(arguments.AlternateDiffHeadRef), AlternateDiffHeadCommit: strings.TrimSpace(arguments.AlternateDiffHeadCommit),
			DeltaBaseCommit: strings.TrimSpace(arguments.DeltaBaseCommit), InheritedCommitCount: arguments.InheritedCommitCount,
			TaskLocalCommitCount: arguments.TaskLocalCommitCount, ThreadID: arguments.ThreadID, RunID: strings.TrimSpace(arguments.RunID),
		})
	case "post_review_findings":
		return json.Marshal(postReviewFindingsBody{
			ReviewRoundID: arguments.ReviewRoundID, Sender: strings.TrimSpace(arguments.Sender), ThreadID: arguments.ThreadID,
			Notes: strings.TrimSpace(arguments.Notes), RunID: strings.TrimSpace(arguments.RunID), SubagentRole: strings.TrimSpace(arguments.SubagentRole),
		})
	case "create_review_finding":
		fileReferences, err := parseStringList(arguments.FileReferences)
		if err != nil {
			return nil, err
		}
		testCommands, err := parseStringList(arguments.TestCommands)
		if err != nil {
			return nil, err
		}
		return json.Marshal(createReviewFindingBody{
			CreatedBy: strings.TrimSpace(arguments.CreatedBy), Category: strings.TrimSpace(arguments.Category), Summary: strings.TrimSpace(arguments.Summary),
			Notes: strings.TrimSpace(arguments.Notes), FileReferences: fileReferences, TestCommands: testCommands,
			RunID: strings.TrimSpace(arguments.RunID), SubagentRole: strings.TrimSpace(arguments.SubagentRole),
		})
	case "set_review_verdict":
		return json.Marshal(reviewVerdictBody{
			Verdict: strings.TrimSpace(arguments.Verdict), DecidedBy: strings.TrimSpace(arguments.DecidedBy),
			Notes: strings.TrimSpace(arguments.Notes), RunID: strings.TrimSpace(arguments.RunID), SubagentRole: strings.TrimSpace(arguments.SubagentRole),
		})
	case "respond_to_review_finding":
		return json.Marshal(respondReviewFindingBody{
			RespondedBy: strings.TrimSpace(arguments.RespondedBy), ResponseNotes: strings.TrimSpace(arguments.ResponseNotes),
			Status: strings.TrimSpace(arguments.Status), StatusNotes: strings.TrimSpace(arguments.StatusNotes), FollowUpTaskID: arguments.FollowUpTaskID,
			RunID: strings.TrimSpace(arguments.RunID), SubagentRole: strings.TrimSpace(arguments.SubagentRole),
		})
	case "set_review_finding_status":
		return json.Marshal(reviewFindingStatusBody{
			Status: strings.TrimSpace(arguments.Status), UpdatedBy: strings.TrimSpace(arguments.UpdatedBy), Notes: strings.TrimSpace(arguments.Notes),
			FollowUpTaskID: arguments.FollowUpTaskID, RunID: strings.TrimSpace(arguments.RunID), SubagentRole: strings.TrimSpace(arguments.SubagentRole),
		})
	case "split_review_findings_to_follow_up":
		findingIDs, err := parseInt64List(arguments.FindingIDs)
		if err != nil {
			return nil, err
		}
		followUpTags, err := parseStringList(arguments.FollowUpTags)
		if err != nil {
			return nil, err
		}
		priority := 0
		if arguments.FollowUpPriority != nil {
			priority = *arguments.FollowUpPriority
		}
		return json.Marshal(splitReviewFindingsBody{
			FindingIDs: findingIDs, SplitBy: strings.TrimSpace(arguments.SplitBy), FollowUpTitle: strings.TrimSpace(arguments.FollowUpTitle),
			FollowUpParentTaskID: arguments.FollowUpParentTaskID, FollowUpPriority: priority, FollowUpAssignedTo: strings.TrimSpace(arguments.FollowUpAssignedTo),
			FollowUpTags: followUpTags, OverrideBlocking: arguments.OverrideBlocking, IdempotencyKey: strings.TrimSpace(arguments.IdempotencyKey),
		})
	case "await_github_checks":
		requiredChecks, err := parseStringList(arguments.RequiredChecks)
		if err != nil {
			return nil, err
		}
		return json.Marshal(githubCheckGateBody{
			Repository: strings.TrimSpace(arguments.Repository), CommitSHA: strings.TrimSpace(arguments.CommitSHA),
			Ref: strings.TrimSpace(arguments.Ref), RequiredChecks: requiredChecks, TimeoutSeconds: arguments.TimeoutSeconds,
			PollIntervalSeconds: arguments.PollIntervalSeconds, RequestedBy: strings.TrimSpace(arguments.RequestedBy),
			AgentProfile: strings.TrimSpace(arguments.AgentProfile), AgentInstanceID: strings.TrimSpace(arguments.AgentInstanceID),
			SessionKey: strings.TrimSpace(arguments.SessionKey),
		})
	case "list_review_rounds", "list_review_findings":
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: review operation %s", ErrUnsupportedAdapter, operation)
	}
}

func reviewRESTURL(baseURL string, route Route, arguments reviewToolArguments) (string, error) {
	routePath, err := expandReviewPath(route.Path, arguments)
	if err != nil {
		return "", err
	}
	parsedURL, err := url.Parse(baseURL + routePath)
	if err != nil {
		return "", fmt.Errorf("parsing review backend URL: %w", err)
	}
	query := parsedURL.Query()
	if route.Operation == "list_review_findings" {
		if arguments.ReviewRoundID != 0 {
			query.Set("review_round_id", strconv.FormatInt(arguments.ReviewRoundID, 10))
		}
		setStringValueQuery(query, "status", arguments.Status)
		setBoolQuery(query, "resolved", arguments.Resolved)
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func expandReviewPath(path string, arguments reviewToolArguments) (string, error) {
	result := path
	if strings.Contains(result, "{project_id}") {
		if strings.TrimSpace(arguments.ProjectID) == "" {
			return "", fmt.Errorf("review route requires project_id")
		}
		result = strings.ReplaceAll(result, "{project_id}", url.PathEscape(strings.TrimSpace(arguments.ProjectID)))
	}
	if strings.Contains(result, "{task_id}") {
		if arguments.TaskID == 0 {
			return "", fmt.Errorf("review route requires task_id")
		}
		result = strings.ReplaceAll(result, "{task_id}", strconv.FormatInt(arguments.TaskID, 10))
	}
	if strings.Contains(result, "{review_round_id}") {
		if arguments.ReviewRoundID == 0 {
			return "", fmt.Errorf("review route requires review_round_id")
		}
		result = strings.ReplaceAll(result, "{review_round_id}", strconv.FormatInt(arguments.ReviewRoundID, 10))
	}
	if strings.Contains(result, "{finding_id}") {
		if arguments.ReviewFindingID == 0 {
			return "", fmt.Errorf("review route requires review_finding_id")
		}
		result = strings.ReplaceAll(result, "{finding_id}", strconv.FormatInt(arguments.ReviewFindingID, 10))
	}
	return result, nil
}
