package review

import "time"

type CreateReviewRoundRequest struct {
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

type CreateReviewFindingRequest struct {
	CreatedBy      string   `json:"created_by"`
	Category       string   `json:"category"`
	Summary        string   `json:"summary"`
	Notes          string   `json:"notes,omitempty"`
	FileReferences []string `json:"file_references,omitempty"`
	TestCommands   []string `json:"test_commands,omitempty"`
	RunID          string   `json:"run_id,omitempty"`
	SubagentRole   string   `json:"subagent_role,omitempty"`
}

type SetReviewVerdictRequest struct {
	Verdict      string `json:"verdict"`
	DecidedBy    string `json:"decided_by"`
	Notes        string `json:"notes,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	SubagentRole string `json:"subagent_role,omitempty"`
}

type RespondToFindingRequest struct {
	RespondedBy    string `json:"responded_by"`
	ResponseNotes  string `json:"response_notes,omitempty"`
	Status         string `json:"status,omitempty"`
	StatusNotes    string `json:"status_notes,omitempty"`
	FollowUpTaskID *int64 `json:"follow_up_task_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	SubagentRole   string `json:"subagent_role,omitempty"`
}

type SetFindingStatusRequest struct {
	Status         string `json:"status"`
	UpdatedBy      string `json:"updated_by"`
	Notes          string `json:"notes,omitempty"`
	FollowUpTaskID *int64 `json:"follow_up_task_id,omitempty"`
	RunID          string `json:"run_id,omitempty"`
	SubagentRole   string `json:"subagent_role,omitempty"`
}

type SplitFindingsRequest struct {
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

type PostReviewFindingsRequest struct {
	ReviewRoundID int64  `json:"review_round_id"`
	Sender        string `json:"sender"`
	ThreadID      *int64 `json:"thread_id,omitempty"`
	Notes         string `json:"notes,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	SubagentRole  string `json:"subagent_role,omitempty"`
}

type PostPacketMarkdownRequest struct {
	Markdown       string `json:"markdown"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type RegisterGitHubCheckGateRequest struct {
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

type ReviewRoundResponse struct {
	ID                      int64      `json:"id"`
	ProjectID               string     `json:"project_id"`
	TaskID                  int64      `json:"task_id"`
	RoundNumber             int        `json:"round_number"`
	RequestedBy             string     `json:"requested_by"`
	Branch                  string     `json:"branch"`
	BaseBranch              string     `json:"base_branch"`
	BaseCommit              string     `json:"base_commit"`
	HeadCommit              string     `json:"head_commit"`
	LastReviewedHeadCommit  string     `json:"last_reviewed_head_commit,omitempty"`
	CommitsSinceLastReview  *int       `json:"commits_since_last_review,omitempty"`
	TestsRun                []string   `json:"tests_run,omitempty"`
	Notes                   string     `json:"notes,omitempty"`
	PreferredDiffBaseRef    string     `json:"preferred_diff_base_ref,omitempty"`
	PreferredDiffBaseCommit string     `json:"preferred_diff_base_commit,omitempty"`
	PreferredDiffHeadRef    string     `json:"preferred_diff_head_ref,omitempty"`
	PreferredDiffHeadCommit string     `json:"preferred_diff_head_commit,omitempty"`
	AlternateDiffBaseRef    string     `json:"alternate_diff_base_ref,omitempty"`
	AlternateDiffBaseCommit string     `json:"alternate_diff_base_commit,omitempty"`
	AlternateDiffHeadRef    string     `json:"alternate_diff_head_ref,omitempty"`
	AlternateDiffHeadCommit string     `json:"alternate_diff_head_commit,omitempty"`
	DeltaBaseCommit         string     `json:"delta_base_commit,omitempty"`
	InheritedCommitCount    *int       `json:"inherited_commit_count,omitempty"`
	TaskLocalCommitCount    *int       `json:"task_local_commit_count,omitempty"`
	Verdict                 string     `json:"verdict,omitempty"`
	VerdictBy               string     `json:"verdict_by,omitempty"`
	VerdictNotes            string     `json:"verdict_notes,omitempty"`
	RequestedAt             time.Time  `json:"requested_at"`
	VerdictAt               *time.Time `json:"verdict_at,omitempty"`
}

type ReviewFindingResponse struct {
	ID              int64      `json:"id"`
	ProjectID       string     `json:"project_id"`
	FindingKey      string     `json:"finding_key"`
	TaskID          int64      `json:"task_id"`
	ReviewRoundID   int64      `json:"review_round_id"`
	RoundNumber     int        `json:"round_number"`
	FindingNumber   int        `json:"finding_number"`
	CreatedBy       string     `json:"created_by"`
	Category        string     `json:"category"`
	Summary         string     `json:"summary"`
	Notes           string     `json:"notes,omitempty"`
	FileReferences  []string   `json:"file_references,omitempty"`
	TestCommands    []string   `json:"test_commands,omitempty"`
	Status          string     `json:"status"`
	StatusUpdatedBy string     `json:"status_updated_by,omitempty"`
	StatusNotes     string     `json:"status_notes,omitempty"`
	StatusUpdatedAt *time.Time `json:"status_updated_at,omitempty"`
	ResponseBy      string     `json:"response_by,omitempty"`
	ResponseNotes   string     `json:"response_notes,omitempty"`
	ResponseAt      *time.Time `json:"response_at,omitempty"`
	FollowUpTaskID  *int64     `json:"follow_up_task_id,omitempty"`
	RunID           string     `json:"run_id,omitempty"`
	SubagentRole    string     `json:"subagent_role,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type ReviewPacketResponse struct {
	ID               int64             `json:"id,omitempty"`
	ProjectID        string            `json:"project_id"`
	TaskID           int64             `json:"task_id"`
	ReviewRoundID    *int64            `json:"review_round_id,omitempty"`
	PacketKind       string            `json:"packet_kind"`
	Sender           string            `json:"sender"`
	MessageID        *int64            `json:"message_id,omitempty"`
	FrontMatter      map[string]any    `json:"front_matter"`
	TypedEnvelope    map[string]any    `json:"typed_envelope"`
	MarkdownBody     string            `json:"markdown_body"`
	ValidationStatus string            `json:"validation_status"`
	ValidationErrors []ValidationIssue `json:"validation_errors,omitempty"`
	CreatedAt        time.Time         `json:"created_at,omitempty"`
	AcceptedAt       *time.Time        `json:"accepted_at,omitempty"`
}

type GitHubCheckGateResponse struct {
	ID                         int64            `json:"id"`
	ProjectID                  string           `json:"project_id"`
	TaskID                     int64            `json:"task_id"`
	Repository                 string           `json:"repository"`
	CommitSHA                  string           `json:"commit_sha"`
	Ref                        string           `json:"ref"`
	RequiredChecks             []string         `json:"required_checks"`
	Status                     string           `json:"status"`
	RequestedBy                string           `json:"requested_by"`
	AgentProfile               string           `json:"agent_profile,omitempty"`
	AgentInstanceID            string           `json:"agent_instance_id,omitempty"`
	SessionKey                 string           `json:"session_key,omitempty"`
	TimeoutAt                  time.Time        `json:"timeout_at"`
	PollIntervalSeconds        int              `json:"poll_interval_seconds"`
	NextPollAt                 time.Time        `json:"next_poll_at"`
	LastCheckedAt              *time.Time       `json:"last_checked_at,omitempty"`
	CompletedAt                *time.Time       `json:"completed_at,omitempty"`
	StatusURL                  string           `json:"status_url,omitempty"`
	Summary                    string           `json:"summary,omitempty"`
	CheckRuns                  []GitHubCheckRun `json:"check_runs,omitempty"`
	ObservedCheckRuns          []GitHubCheckRun `json:"observed_check_runs,omitempty"`
	MissingRequiredChecks      []string         `json:"missing_required_checks,omitempty"`
	TerminalReason             string           `json:"terminal_reason,omitempty"`
	FailureSummary             string           `json:"failure_summary,omitempty"`
	EvidenceMessageStatus      string           `json:"evidence_message_status,omitempty"`
	EvidenceMessageID          *int64           `json:"evidence_message_id,omitempty"`
	EvidenceMessageError       string           `json:"evidence_message_error,omitempty"`
	EvidenceMessageAttemptedAt *time.Time       `json:"evidence_message_attempted_at,omitempty"`
	CreatedAt                  time.Time        `json:"created_at"`
	UpdatedAt                  time.Time        `json:"updated_at"`
}

type WorkflowSummaryResponse struct {
	CurrentRound           *ReviewRoundResponse    `json:"current_round,omitempty"`
	CurrentVerdict         string                  `json:"current_verdict,omitempty"`
	ReviewRoundCount       int                     `json:"review_round_count"`
	UnresolvedFindingCount int                     `json:"unresolved_finding_count"`
	ResolvedFindingCount   int                     `json:"resolved_finding_count"`
	AddressedFindingCount  int                     `json:"addressed_finding_count"`
	OpenFindings           []ReviewFindingResponse `json:"open_findings"`
	ResolvedFindings       []ReviewFindingResponse `json:"resolved_findings"`
	Timeline               []ReviewTimelineEntry   `json:"timeline"`
}

type SplitFindingsResponse struct {
	FollowUpTaskID  int64                   `json:"follow_up_task_id"`
	SplitFindings   []ReviewFindingResponse `json:"split_findings"`
	SkippedFindings []ReviewFindingResponse `json:"skipped_findings"`
}

type SimpleMessageResponse struct {
	Message string `json:"message"`
}

func toRoundResponse(round *ReviewRound) ReviewRoundResponse {
	return ReviewRoundResponse{
		ID: round.ID, ProjectID: round.ProjectID, TaskID: round.TaskID, RoundNumber: round.RoundNumber,
		RequestedBy: round.RequestedBy, Branch: round.Branch, BaseBranch: round.BaseBranch,
		BaseCommit: round.BaseCommit, HeadCommit: round.HeadCommit, LastReviewedHeadCommit: round.LastReviewedHeadCommit,
		CommitsSinceLastReview: round.CommitsSinceLastReview, TestsRun: round.TestsRun, Notes: round.Notes,
		PreferredDiffBaseRef: round.PreferredDiffBaseRef, PreferredDiffBaseCommit: round.PreferredDiffBaseCommit,
		PreferredDiffHeadRef: round.PreferredDiffHeadRef, PreferredDiffHeadCommit: round.PreferredDiffHeadCommit,
		AlternateDiffBaseRef: round.AlternateDiffBaseRef, AlternateDiffBaseCommit: round.AlternateDiffBaseCommit,
		AlternateDiffHeadRef: round.AlternateDiffHeadRef, AlternateDiffHeadCommit: round.AlternateDiffHeadCommit,
		DeltaBaseCommit: round.DeltaBaseCommit, InheritedCommitCount: round.InheritedCommitCount,
		TaskLocalCommitCount: round.TaskLocalCommitCount, Verdict: round.Verdict, VerdictBy: round.VerdictBy,
		VerdictNotes: round.VerdictNotes, RequestedAt: round.RequestedAt, VerdictAt: round.VerdictAt,
	}
}

func toFindingResponse(finding *ReviewFinding) ReviewFindingResponse {
	return ReviewFindingResponse(*finding)
}

func toGitHubCheckGateResponse(gate *GitHubCheckGate) GitHubCheckGateResponse {
	return GitHubCheckGateResponse{
		ID: gate.ID, ProjectID: gate.ProjectID, TaskID: gate.TaskID, Repository: gate.Repository,
		CommitSHA: gate.CommitSHA, Ref: gate.Ref, RequiredChecks: gate.RequiredChecks, Status: gate.Status,
		RequestedBy: gate.RequestedBy, AgentProfile: gate.AgentProfile, AgentInstanceID: gate.AgentInstanceID,
		SessionKey: gate.SessionKey, TimeoutAt: gate.TimeoutAt, PollIntervalSeconds: gate.PollIntervalSeconds,
		NextPollAt: gate.NextPollAt, LastCheckedAt: gate.LastCheckedAt, CompletedAt: gate.CompletedAt,
		StatusURL: gate.StatusURL, Summary: gate.Summary, CheckRuns: gate.CheckRuns,
		ObservedCheckRuns: gate.ObservedCheckRuns, MissingRequiredChecks: gate.MissingRequiredChecks,
		TerminalReason: gate.TerminalReason,
		FailureSummary: gate.FailureSummary, EvidenceMessageStatus: gate.EvidenceMessageStatus,
		EvidenceMessageID: gate.EvidenceMessageID, EvidenceMessageError: gate.EvidenceMessageError,
		EvidenceMessageAttemptedAt: gate.EvidenceMessageAttemptedAt, CreatedAt: gate.CreatedAt, UpdatedAt: gate.UpdatedAt,
	}
}
