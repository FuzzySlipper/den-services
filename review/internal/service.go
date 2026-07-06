package review

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const defaultGitHubHTTPErrorBackoff = 15 * time.Minute

type ProjectValidator interface {
	AssertWritable(ctx context.Context, projectID string) error
}

type TaskClient interface {
	GetTask(ctx context.Context, taskID int64) (TaskContext, error)
	GetTaskContext(ctx context.Context, projectID string, taskID int64) (TaskContext, error)
	CreateFollowUpTask(ctx context.Context, projectID string, req CreateFollowUpTaskRequest) (CreatedTask, error)
}

type MessageClient interface {
	AppendTaskMessage(ctx context.Context, projectID string, req AppendMessageRequest) (AppendedMessage, error)
}

type GitHubCheckProvider interface {
	CheckCommit(ctx context.Context, repository string, commitSHA string, requiredChecks []string) (GitHubCheckResult, error)
}

type ReviewStore interface {
	Ping(ctx context.Context) error
	CreateRound(ctx context.Context, round *ReviewRound) (*ReviewRound, error)
	ListRounds(ctx context.Context, projectID string, taskID int64) ([]*ReviewRound, error)
	GetRound(ctx context.Context, id int64) (*ReviewRound, error)
	SetVerdict(ctx context.Context, id int64, verdict string, decidedBy string, notes string, decidedAt time.Time) (*ReviewRound, error)
	CreateFinding(ctx context.Context, finding *ReviewFinding) (*ReviewFinding, error)
	ListFindings(ctx context.Context, query ListFindingsQuery) ([]*ReviewFinding, error)
	GetFinding(ctx context.Context, id int64) (*ReviewFinding, error)
	RespondToFinding(ctx context.Context, id int64, response FindingResponseUpdate, updatedAt time.Time) (*ReviewFinding, error)
	SetFindingStatus(ctx context.Context, id int64, update FindingStatusUpdate, updatedAt time.Time) (*ReviewFinding, error)
	StorePacket(ctx context.Context, packet *ReviewPacket) (*ReviewPacket, error)
	GetPacket(ctx context.Context, id int64) (*ReviewPacket, error)
	GetPacketByIdempotency(ctx context.Context, projectID string, idempotencyKey string) (*ReviewPacket, error)
	WorkflowSummary(ctx context.Context, projectID string, taskID int64) (WorkflowSummary, error)
	RegisterGitHubCheckGate(ctx context.Context, gate *GitHubCheckGate, now time.Time) (*GitHubCheckGate, []*GitHubCheckGate, error)
	GetGitHubCheckGate(ctx context.Context, projectID string, taskID int64, commitSHA string) (*GitHubCheckGate, error)
	ListPendingGitHubCheckGates(ctx context.Context, now time.Time, limit int) ([]*GitHubCheckGate, error)
	ListGitHubCheckGatesPendingEvidence(ctx context.Context, limit int) ([]*GitHubCheckGate, error)
	CompleteGitHubCheckGate(ctx context.Context, id int64, status string, result GitHubCheckResult, checkedAt time.Time) (*GitHubCheckGate, bool, error)
	DelayGitHubCheckGate(ctx context.Context, id int64, result GitHubCheckResult, nextPollAt time.Time, checkedAt time.Time) (*GitHubCheckGate, bool, error)
	TimeoutGitHubCheckGate(ctx context.Context, id int64, checkedAt time.Time) (*GitHubCheckGate, bool, error)
	MarkGitHubCheckGateEvidencePosted(ctx context.Context, id int64, messageID int64, at time.Time) (*GitHubCheckGate, error)
	RecordGitHubCheckGateEvidenceError(ctx context.Context, id int64, messageError string, at time.Time) (*GitHubCheckGate, error)
}

type ListFindingsQuery struct {
	ProjectID     string
	TaskID        int64
	ReviewRoundID *int64
	Statuses      []string
	Resolved      *bool
}

type FindingResponseUpdate struct {
	RespondedBy    string
	ResponseNotes  string
	Status         string
	StatusNotes    string
	FollowUpTaskID *int64
	RunID          string
	SubagentRole   string
}

type FindingStatusUpdate struct {
	Status         string
	UpdatedBy      string
	Notes          string
	FollowUpTaskID *int64
	RunID          string
	SubagentRole   string
}

type WorkflowSummary struct {
	CurrentRound           *ReviewRound
	CurrentVerdict         string
	ReviewRoundCount       int
	UnresolvedFindingCount int
	ResolvedFindingCount   int
	AddressedFindingCount  int
	OpenFindings           []*ReviewFinding
	ResolvedFindings       []*ReviewFinding
	Timeline               []ReviewTimelineEntry
}

type ReviewTimelineEntry struct {
	ReviewRoundID          int64      `json:"review_round_id"`
	RoundNumber            int        `json:"round_number"`
	Branch                 string     `json:"branch"`
	RequestedBy            string     `json:"requested_by"`
	RequestedAt            time.Time  `json:"requested_at"`
	HeadCommit             string     `json:"head_commit"`
	LastReviewedHeadCommit string     `json:"last_reviewed_head_commit,omitempty"`
	CommitsSinceLastReview *int       `json:"commits_since_last_review,omitempty"`
	Verdict                string     `json:"verdict,omitempty"`
	VerdictBy              string     `json:"verdict_by,omitempty"`
	VerdictAt              *time.Time `json:"verdict_at,omitempty"`
	TotalFindings          int        `json:"total_findings"`
	OpenFindings           int        `json:"open_findings"`
	AddressedFindings      int        `json:"addressed_findings"`
	ClaimedFixedFindings   int        `json:"claimed_fixed_findings"`
	ResolvedFindings       int        `json:"resolved_findings"`
}

type CreateFollowUpTaskRequest struct {
	Title          string
	Description    string
	ParentID       *int64
	Priority       int
	AssignedTo     string
	Tags           []string
	IdempotencyKey string
}

type AppendMessageRequest struct {
	TaskID   int64
	ThreadID *int64
	Sender   string
	Content  string
	Intent   string
	Metadata map[string]any
}

type Service struct {
	store         ReviewStore
	projects      ProjectValidator
	tasks         TaskClient
	messages      MessageClient
	githubChecks  GitHubCheckProvider
	githubOptions GitHubCheckOptions
	clock         func() time.Time
}

func NewService(store ReviewStore, projects ProjectValidator, tasks TaskClient, messages MessageClient, clock func() time.Time) *Service {
	return &Service{
		store: store, projects: projects, tasks: tasks, messages: messages,
		githubOptions: DefaultGitHubCheckOptions(), clock: clock,
	}
}

type GitHubCheckOptions struct {
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
	PollInterval   time.Duration
	StatusURLBase  string
}

func DefaultGitHubCheckOptions() GitHubCheckOptions {
	return GitHubCheckOptions{
		DefaultTimeout: 30 * time.Minute,
		MaxTimeout:     2 * time.Hour,
		PollInterval:   30 * time.Second,
	}
}

func (s *Service) ConfigureGitHubChecks(provider GitHubCheckProvider, options GitHubCheckOptions) {
	if options.DefaultTimeout <= 0 {
		options.DefaultTimeout = DefaultGitHubCheckOptions().DefaultTimeout
	}
	if options.MaxTimeout <= 0 {
		options.MaxTimeout = DefaultGitHubCheckOptions().MaxTimeout
	}
	if options.PollInterval <= 0 {
		options.PollInterval = DefaultGitHubCheckOptions().PollInterval
	}
	s.githubChecks = provider
	s.githubOptions = options
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) CreateRound(ctx context.Context, projectID string, taskID int64, req CreateReviewRoundRequest) (*ReviewRound, error) {
	task, err := s.validateTask(ctx, projectID, taskID, TaskStatusInProgress, TaskStatusReview)
	if err != nil {
		return nil, err
	}
	if err := s.projects.AssertWritable(ctx, task.ProjectID); err != nil {
		return nil, err
	}
	round, err := roundFromRequest(task.ProjectID, taskID, req, s.clock().UTC())
	if err != nil {
		return nil, err
	}
	return s.store.CreateRound(ctx, round)
}

func (s *Service) CreateRoundForTask(ctx context.Context, taskID int64, req CreateReviewRoundRequest) (*ReviewRound, error) {
	task, err := s.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return s.CreateRound(ctx, task.ProjectID, taskID, req)
}

func (s *Service) RequestReview(ctx context.Context, projectID string, taskID int64, req CreateReviewRoundRequest) (*ReviewPacket, error) {
	round, err := s.CreateRound(ctx, projectID, taskID, req)
	if err != nil {
		return nil, err
	}
	kind := PacketKindReviewRequest
	if round.RoundNumber > 1 {
		kind = PacketKindRereviewRequest
	}
	packet := packetForRound(round, kind, req.ThreadID, req.RunID)
	return s.acceptPacket(ctx, packet, req.ThreadID)
}

func (s *Service) ListRounds(ctx context.Context, projectID string, taskID int64) ([]*ReviewRound, error) {
	if _, err := s.validateTask(ctx, projectID, taskID); err != nil {
		return nil, err
	}
	return s.store.ListRounds(ctx, strings.TrimSpace(projectID), taskID)
}

func (s *Service) ListRoundsForTask(ctx context.Context, taskID int64) ([]*ReviewRound, error) {
	task, err := s.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return s.ListRounds(ctx, task.ProjectID, taskID)
}

func (s *Service) CreateFinding(ctx context.Context, roundID int64, req CreateReviewFindingRequest) (*ReviewFinding, error) {
	round, err := s.store.GetRound(ctx, roundID)
	if err != nil {
		return nil, err
	}
	if _, err := s.validateTask(ctx, round.ProjectID, round.TaskID, TaskStatusReview, TaskStatusInProgress); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.CreatedBy) == "" {
		return nil, validationError(ErrMissingActor, "missing_actor", "created_by", "review_findings.created_by")
	}
	category := strings.TrimSpace(req.Category)
	if !validCategory(category) {
		return nil, validationError(fmt.Errorf("%w: %s", ErrInvalidCategory, category), "invalid_category", "category", "review_findings.category")
	}
	if strings.TrimSpace(req.Summary) == "" {
		return nil, validationError(fmt.Errorf("summary is required"), "missing_summary", "summary", "review_findings.summary")
	}
	now := s.clock().UTC()
	return s.store.CreateFinding(ctx, &ReviewFinding{
		ProjectID: round.ProjectID, TaskID: round.TaskID, ReviewRoundID: round.ID, RoundNumber: round.RoundNumber,
		CreatedBy: strings.TrimSpace(req.CreatedBy), Category: category, Summary: strings.TrimSpace(req.Summary),
		Notes: strings.TrimSpace(req.Notes), FileReferences: trimSlice(req.FileReferences), TestCommands: trimSlice(req.TestCommands),
		Status: StatusOpen, RunID: strings.TrimSpace(req.RunID), SubagentRole: strings.TrimSpace(req.SubagentRole),
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Service) ListFindings(ctx context.Context, projectID string, taskID int64, query ListFindingsQuery) ([]*ReviewFinding, error) {
	if _, err := s.validateTask(ctx, projectID, taskID); err != nil {
		return nil, err
	}
	for _, status := range query.Statuses {
		if !validFindingStatus(status) {
			return nil, validationError(fmt.Errorf("%w: %s", ErrInvalidStatus, status), "invalid_status", "status", "review_findings.status")
		}
	}
	query.ProjectID = strings.TrimSpace(projectID)
	query.TaskID = taskID
	return s.store.ListFindings(ctx, query)
}

func (s *Service) ListFindingsForTask(ctx context.Context, taskID int64, query ListFindingsQuery) ([]*ReviewFinding, error) {
	task, err := s.resolveTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return s.ListFindings(ctx, task.ProjectID, taskID, query)
}

func (s *Service) SetVerdict(ctx context.Context, roundID int64, req SetReviewVerdictRequest) (*ReviewRound, error) {
	round, err := s.store.GetRound(ctx, roundID)
	if err != nil {
		return nil, err
	}
	if _, err := s.validateTask(ctx, round.ProjectID, round.TaskID, TaskStatusReview); err != nil {
		return nil, err
	}
	verdict := strings.TrimSpace(req.Verdict)
	if !validVerdict(verdict) {
		return nil, validationError(fmt.Errorf("%w: %s", ErrInvalidVerdict, verdict), "invalid_verdict", "verdict", "review_findings.verdict")
	}
	actor := strings.TrimSpace(req.DecidedBy)
	if actor == "" {
		return nil, validationError(ErrMissingActor, "missing_actor", "decided_by", "review_findings.decided_by")
	}
	updated, err := s.store.SetVerdict(ctx, roundID, verdict, actor, strings.TrimSpace(req.Notes), s.clock().UTC())
	if err != nil {
		return nil, err
	}
	_, err = s.messages.AppendTaskMessage(ctx, updated.ProjectID, AppendMessageRequest{
		TaskID: updated.TaskID, Sender: actor, Content: renderVerdictPacket(updated),
		Intent: intentForVerdict(verdict), Metadata: metadataForRound(updated, packetKindForVerdict(verdict), verdictType(verdict), verdict),
	})
	return updated, err
}

func (s *Service) RespondToFinding(ctx context.Context, findingID int64, req RespondToFindingRequest) (*ReviewFinding, error) {
	finding, err := s.store.GetFinding(ctx, findingID)
	if err != nil {
		return nil, err
	}
	if _, err := s.validateTask(ctx, finding.ProjectID, finding.TaskID, TaskStatusInProgress, TaskStatusReview); err != nil {
		return nil, err
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && !validFindingStatus(status) {
		return nil, validationError(fmt.Errorf("%w: %s", ErrInvalidStatus, status), "invalid_status", "status", "implementer_response.responses")
	}
	if err := validateFollowUpStatus(status, req.FollowUpTaskID); err != nil {
		return nil, err
	}
	return s.store.RespondToFinding(ctx, findingID, FindingResponseUpdate{
		RespondedBy: strings.TrimSpace(req.RespondedBy), ResponseNotes: strings.TrimSpace(req.ResponseNotes),
		Status: status, StatusNotes: strings.TrimSpace(req.StatusNotes), FollowUpTaskID: req.FollowUpTaskID,
		RunID: strings.TrimSpace(req.RunID), SubagentRole: strings.TrimSpace(req.SubagentRole),
	}, s.clock().UTC())
}

func (s *Service) SetFindingStatus(ctx context.Context, findingID int64, req SetFindingStatusRequest) (*ReviewFinding, error) {
	finding, err := s.store.GetFinding(ctx, findingID)
	if err != nil {
		return nil, err
	}
	if _, err := s.validateTask(ctx, finding.ProjectID, finding.TaskID, TaskStatusInProgress, TaskStatusReview); err != nil {
		return nil, err
	}
	status := strings.TrimSpace(req.Status)
	if !validFindingStatus(status) {
		return nil, validationError(fmt.Errorf("%w: %s", ErrInvalidStatus, status), "invalid_status", "status", "review_findings.status")
	}
	if err := validateFollowUpStatus(status, req.FollowUpTaskID); err != nil {
		return nil, err
	}
	return s.store.SetFindingStatus(ctx, findingID, FindingStatusUpdate{
		Status: status, UpdatedBy: strings.TrimSpace(req.UpdatedBy), Notes: strings.TrimSpace(req.Notes),
		FollowUpTaskID: req.FollowUpTaskID, RunID: strings.TrimSpace(req.RunID), SubagentRole: strings.TrimSpace(req.SubagentRole),
	}, s.clock().UTC())
}

func (s *Service) SplitFindingsToFollowUp(ctx context.Context, projectID string, taskID int64, req SplitFindingsRequest) (SplitFindingsResponse, error) {
	task, err := s.validateTask(ctx, projectID, taskID, TaskStatusReview, TaskStatusInProgress)
	if err != nil {
		return SplitFindingsResponse{}, err
	}
	findings, err := s.store.ListFindings(ctx, ListFindingsQuery{ProjectID: task.ProjectID, TaskID: taskID})
	if err != nil {
		return SplitFindingsResponse{}, err
	}
	selected := map[int64]*ReviewFinding{}
	for _, finding := range findings {
		selected[finding.ID] = finding
	}
	var split []*ReviewFinding
	var skipped []*ReviewFinding
	for _, id := range req.FindingIDs {
		finding, ok := selected[id]
		if !ok {
			return SplitFindingsResponse{}, notFound(fmt.Errorf("%w: %d", ErrMissingFinding, id), "finding_not_found")
		}
		if finding.Category == CategoryBlockingBug && !req.OverrideBlocking {
			skipped = append(skipped, finding)
			continue
		}
		split = append(split, finding)
	}
	if len(split) == 0 {
		return SplitFindingsResponse{SkippedFindings: toFindingResponses(skipped)}, nil
	}
	followUp, err := s.tasks.CreateFollowUpTask(ctx, task.ProjectID, CreateFollowUpTaskRequest{
		Title:       firstNonEmpty(req.FollowUpTitle, "Follow up review findings for task "+fmt.Sprint(taskID)),
		Description: renderFollowUpDescription(task, split, strings.TrimSpace(req.SplitBy)),
		ParentID:    req.FollowUpParentTaskID, Priority: req.FollowUpPriority, AssignedTo: strings.TrimSpace(req.FollowUpAssignedTo),
		Tags: trimSlice(req.FollowUpTags), IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
	})
	if err != nil {
		return SplitFindingsResponse{}, err
	}
	updated := make([]*ReviewFinding, 0, len(split))
	for _, finding := range split {
		statused, err := s.store.SetFindingStatus(ctx, finding.ID, FindingStatusUpdate{
			Status: StatusSplitToFollowUp, UpdatedBy: strings.TrimSpace(req.SplitBy), FollowUpTaskID: &followUp.ID,
			Notes: "Split to follow-up task.",
		}, s.clock().UTC())
		if err != nil {
			return SplitFindingsResponse{}, err
		}
		updated = append(updated, statused)
	}
	return SplitFindingsResponse{FollowUpTaskID: followUp.ID, SplitFindings: toFindingResponses(updated), SkippedFindings: toFindingResponses(skipped)}, nil
}

func (s *Service) PostReviewFindings(ctx context.Context, projectID string, taskID int64, req PostReviewFindingsRequest) (*ReviewPacket, error) {
	task, err := s.validateTask(ctx, projectID, taskID, TaskStatusReview)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Sender) == "" {
		return nil, validationError(ErrMissingActor, "missing_sender", "sender", "review_findings.sender")
	}
	round, err := s.store.GetRound(ctx, req.ReviewRoundID)
	if err != nil {
		return nil, err
	}
	if round.ProjectID != task.ProjectID || round.TaskID != taskID {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingRound, req.ReviewRoundID), "round_not_found")
	}
	findings, err := s.store.ListFindings(ctx, ListFindingsQuery{ProjectID: task.ProjectID, TaskID: taskID, ReviewRoundID: &round.ID})
	if err != nil {
		return nil, err
	}
	allFindings, err := s.store.ListFindings(ctx, ListFindingsQuery{ProjectID: task.ProjectID, TaskID: taskID})
	if err != nil {
		return nil, err
	}
	packet := reviewFindingsPacket(round, findings, unresolvedFindingSummaries(allFindings), req)
	packet.IdempotencyKey = fmt.Sprintf("review-findings:%d:%s:%s", round.ID, strings.TrimSpace(req.Sender), strings.TrimSpace(req.RunID))
	return s.acceptPacket(ctx, packet, req.ThreadID)
}

func (s *Service) ValidatePacketMarkdown(ctx context.Context, projectID string, taskID int64, markdown string) (*ReviewPacket, error) {
	packet, err := ParseReviewPacketMarkdown(markdown)
	if err != nil {
		return nil, err
	}
	if err := s.validatePacketContext(ctx, packet, projectID, taskID); err != nil {
		return nil, err
	}
	packet.ValidationStatus = PacketStatusValid
	return packet, nil
}

func (s *Service) PostPacketMarkdown(ctx context.Context, projectID string, taskID int64, req PostPacketMarkdownRequest) (*ReviewPacket, error) {
	packet, err := s.ValidatePacketMarkdown(ctx, projectID, taskID, req.Markdown)
	if err != nil {
		return nil, err
	}
	packet.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	return s.acceptPacket(ctx, packet, nil)
}

func (s *Service) WorkflowSummary(ctx context.Context, projectID string, taskID int64) (WorkflowSummary, error) {
	if _, err := s.validateTask(ctx, projectID, taskID); err != nil {
		return WorkflowSummary{}, err
	}
	return s.store.WorkflowSummary(ctx, strings.TrimSpace(projectID), taskID)
}

func (s *Service) WorkflowSummaryForTask(ctx context.Context, taskID int64) (WorkflowSummary, error) {
	task, err := s.resolveTask(ctx, taskID)
	if err != nil {
		return WorkflowSummary{}, err
	}
	return s.WorkflowSummary(ctx, task.ProjectID, taskID)
}

func (s *Service) RegisterGitHubCheckGate(ctx context.Context, projectID string, taskID int64, req RegisterGitHubCheckGateRequest) (*GitHubCheckGate, error) {
	task, err := s.validateTask(ctx, projectID, taskID, TaskStatusInProgress, TaskStatusReview)
	if err != nil {
		return nil, err
	}
	gate, err := s.githubGateFromRequest(task.ProjectID, taskID, req)
	if err != nil {
		return nil, err
	}
	stored, superseded, err := s.store.RegisterGitHubCheckGate(ctx, gate, s.clock().UTC())
	if err != nil {
		return nil, err
	}
	for _, gate := range superseded {
		if err := s.deliverGitHubCheckGateEvidence(ctx, gate); err != nil {
			return stored, err
		}
	}
	if terminalGitHubCheckGateStatus(stored.Status) || s.githubChecks == nil {
		return stored, nil
	}
	return s.evaluateGitHubCheckGate(ctx, stored)
}

func (s *Service) GetGitHubCheckGate(ctx context.Context, projectID string, taskID int64, commitSHA string) (*GitHubCheckGate, error) {
	task, err := s.validateTask(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	commitSHA = strings.ToLower(strings.TrimSpace(commitSHA))
	if !validGitHubSHA(commitSHA) {
		return nil, validationError(fmt.Errorf("commit_sha must be a full 40-character hex SHA"), "invalid_commit_sha", "commit_sha", "github_check_gate.commit_sha")
	}
	return s.store.GetGitHubCheckGate(ctx, task.ProjectID, taskID, commitSHA)
}

func (s *Service) PollGitHubCheckGates(ctx context.Context, limit int) error {
	if err := s.retryGitHubCheckGateEvidence(ctx, limit); err != nil {
		return err
	}
	if s.githubChecks == nil {
		return NewServiceError(ErrGitHubChecksUnset, "github_checks_unconfigured", 500)
	}
	if limit <= 0 {
		limit = 10
	}
	now := s.clock().UTC()
	gates, err := s.store.ListPendingGitHubCheckGates(ctx, now, limit)
	if err != nil {
		return err
	}
	for _, gate := range gates {
		if _, err := s.evaluateGitHubCheckGate(ctx, gate); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) evaluateGitHubCheckGate(ctx context.Context, gate *GitHubCheckGate) (*GitHubCheckGate, error) {
	now := s.clock().UTC()
	if !now.Before(gate.TimeoutAt) {
		updated, changed, err := s.store.TimeoutGitHubCheckGate(ctx, gate.ID, now)
		if err != nil {
			return nil, err
		}
		if changed {
			if err := s.deliverGitHubCheckGateEvidence(ctx, updated); err != nil {
				return updated, err
			}
			updated, err = s.store.GetGitHubCheckGate(ctx, updated.ProjectID, updated.TaskID, updated.CommitSHA)
			if err != nil {
				return nil, err
			}
		}
		return updated, nil
	}
	result, err := s.githubChecks.CheckCommit(ctx, gate.Repository, gate.CommitSHA, gate.RequiredChecks)
	if err != nil {
		var githubErr *GitHubHTTPError
		if errors.As(err, &githubErr) && delayableGitHubHTTPStatus(githubErr.StatusCode) {
			return s.delayGitHubCheckGateAfterGitHubHTTPError(ctx, gate, githubErr, now)
		}
		return nil, fmt.Errorf("checking github commit %s: %w", gate.CommitSHA, err)
	}
	if result.Status == GitHubCheckGateStatusPending {
		pending := GitHubCheckResult{Status: GitHubCheckGateStatusPending, Summary: result.Summary, CheckRuns: result.CheckRuns}
		updated, _, err := s.store.CompleteGitHubCheckGate(ctx, gate.ID, GitHubCheckGateStatusPending, pending, now)
		return updated, err
	}
	updated, changed, err := s.store.CompleteGitHubCheckGate(ctx, gate.ID, result.Status, result, now)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := s.deliverGitHubCheckGateEvidence(ctx, updated); err != nil {
			return updated, err
		}
		updated, err = s.store.GetGitHubCheckGate(ctx, updated.ProjectID, updated.TaskID, updated.CommitSHA)
		if err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func (s *Service) delayGitHubCheckGateAfterGitHubHTTPError(ctx context.Context, gate *GitHubCheckGate, githubErr *GitHubHTTPError, checkedAt time.Time) (*GitHubCheckGate, error) {
	nextPollAt := nextGitHubHTTPErrorPollAt(checkedAt, gate.TimeoutAt, githubErr)
	summary := githubHTTPErrorSummary(githubErr, nextPollAt)
	result := GitHubCheckResult{Status: GitHubCheckGateStatusPending, Summary: summary}
	updated, _, err := s.store.DelayGitHubCheckGate(ctx, gate.ID, result, nextPollAt, checkedAt)
	return updated, err
}

func delayableGitHubHTTPStatus(statusCode int) bool {
	return statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests
}

func nextGitHubHTTPErrorPollAt(now time.Time, timeoutAt time.Time, githubErr *GitHubHTTPError) time.Time {
	next := now.Add(defaultGitHubHTTPErrorBackoff)
	if githubErr.RetryAfterSet {
		next = now.Add(githubErr.RetryAfter)
	} else if githubErr.RateLimitResetSet && (!githubErr.RateLimitRemainingSet || githubErr.RateLimitRemaining == 0) && githubErr.RateLimitReset.After(now) {
		next = githubErr.RateLimitReset.Add(time.Minute)
	}
	if !timeoutAt.IsZero() && timeoutAt.After(now) && next.After(timeoutAt) {
		return timeoutAt
	}
	if !next.After(now) {
		return now.Add(defaultGitHubHTTPErrorBackoff)
	}
	return next
}

func githubHTTPErrorSummary(githubErr *GitHubHTTPError, nextPollAt time.Time) string {
	status := strings.TrimSpace(githubErr.Status)
	if status == "" {
		status = fmt.Sprintf("HTTP %d", githubErr.StatusCode)
	}
	summary := "GitHub check polling delayed after GitHub returned " + status + "."
	if message := strings.TrimSpace(githubErr.Message); message != "" {
		summary += " " + message
	}
	if githubErr.RateLimitResetSet {
		summary += " GitHub rate limit reset is " + githubErr.RateLimitReset.Format(time.RFC3339) + "."
	}
	summary += " Next poll is " + nextPollAt.Format(time.RFC3339) + "."
	return summary
}

func (s *Service) retryGitHubCheckGateEvidence(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 10
	}
	gates, err := s.store.ListGitHubCheckGatesPendingEvidence(ctx, limit)
	if err != nil {
		return err
	}
	for _, gate := range gates {
		if err := s.deliverGitHubCheckGateEvidence(ctx, gate); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deliverGitHubCheckGateEvidence(ctx context.Context, gate *GitHubCheckGate) error {
	if gate.EvidenceMessageStatus == GitHubCheckEvidenceStatusPosted {
		return nil
	}
	content, intent := renderGitHubCheckGateEvidence(gate)
	message, err := s.messages.AppendTaskMessage(ctx, gate.ProjectID, AppendMessageRequest{
		TaskID:  gate.TaskID,
		Sender:  firstNonEmpty(gate.RequestedBy, "den-review"),
		Content: content,
		Intent:  intent,
		Metadata: map[string]any{
			"type":              "github_check_gate",
			"gate_id":           gate.ID,
			"status":            gate.Status,
			"repository":        gate.Repository,
			"commit_sha":        gate.CommitSHA,
			"ref":               gate.Ref,
			"required_checks":   gate.RequiredChecks,
			"agent_profile":     gate.AgentProfile,
			"agent_instance_id": gate.AgentInstanceID,
			"session_key":       gate.SessionKey,
		},
	})
	now := s.clock().UTC()
	if err != nil {
		_, recordErr := s.store.RecordGitHubCheckGateEvidenceError(ctx, gate.ID, err.Error(), now)
		if recordErr != nil {
			return fmt.Errorf("appending github check evidence: %w; recording evidence error: %v", err, recordErr)
		}
		return fmt.Errorf("appending github check evidence: %w", err)
	}
	_, err = s.store.MarkGitHubCheckGateEvidencePosted(ctx, gate.ID, message.ID, now)
	return err
}

func (s *Service) githubGateFromRequest(projectID string, taskID int64, req RegisterGitHubCheckGateRequest) (*GitHubCheckGate, error) {
	now := s.clock().UTC()
	repository := strings.TrimSpace(req.Repository)
	if !validGitHubRepository(repository) {
		return nil, validationError(fmt.Errorf("repository must be owner/name"), "invalid_repository", "repository", "github_check_gate.repository")
	}
	commitSHA := strings.ToLower(strings.TrimSpace(req.CommitSHA))
	if !validGitHubSHA(commitSHA) {
		return nil, validationError(fmt.Errorf("commit_sha must be a full 40-character hex SHA"), "invalid_commit_sha", "commit_sha", "github_check_gate.commit_sha")
	}
	requiredChecks := trimSlice(req.RequiredChecks)
	if len(requiredChecks) == 0 {
		return nil, validationError(fmt.Errorf("required_checks is required"), "missing_required_checks", "required_checks", "github_check_gate.required_checks")
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		return nil, validationError(fmt.Errorf("ref is required"), "missing_ref", "ref", "github_check_gate.ref")
	}
	requestedBy := strings.TrimSpace(req.RequestedBy)
	if requestedBy == "" {
		return nil, validationError(ErrMissingActor, "missing_requested_by", "requested_by", "github_check_gate.requested_by")
	}
	timeout := s.githubOptions.DefaultTimeout
	if req.TimeoutSeconds != nil {
		timeout = time.Duration(*req.TimeoutSeconds) * time.Second
	}
	if timeout <= 0 || timeout > s.githubOptions.MaxTimeout {
		return nil, validationError(fmt.Errorf("timeout_seconds must be positive and no greater than %d", int(s.githubOptions.MaxTimeout.Seconds())), "invalid_timeout", "timeout_seconds", "github_check_gate.timeout_seconds")
	}
	pollInterval := s.githubOptions.PollInterval
	if req.PollIntervalSeconds != nil {
		pollInterval = time.Duration(*req.PollIntervalSeconds) * time.Second
	}
	if pollInterval <= 0 {
		return nil, validationError(fmt.Errorf("poll_interval_seconds must be positive"), "invalid_poll_interval", "poll_interval_seconds", "github_check_gate.poll_interval_seconds")
	}
	return &GitHubCheckGate{
		ProjectID: projectID, TaskID: taskID, Repository: repository, CommitSHA: commitSHA,
		Ref: ref, RequiredChecks: requiredChecks, Status: GitHubCheckGateStatusPending,
		RequestedBy: requestedBy, AgentProfile: strings.TrimSpace(req.AgentProfile),
		AgentInstanceID: strings.TrimSpace(req.AgentInstanceID), SessionKey: strings.TrimSpace(req.SessionKey),
		TimeoutAt: now.Add(timeout), PollIntervalSeconds: int(pollInterval.Seconds()), NextPollAt: now,
		StatusURL: s.statusURL(projectID, taskID, commitSHA), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Service) statusURL(projectID string, taskID int64, commitSHA string) string {
	if strings.TrimSpace(s.githubOptions.StatusURLBase) == "" {
		return ""
	}
	return strings.TrimRight(s.githubOptions.StatusURLBase, "/") + "/v1/projects/" + projectID + "/tasks/" + fmt.Sprint(taskID) + "/review/github-check-gates/" + commitSHA
}

func (s *Service) acceptPacket(ctx context.Context, packet *ReviewPacket, threadID *int64) (*ReviewPacket, error) {
	if packet.CreatedAt.IsZero() {
		packet.CreatedAt = s.clock().UTC()
	}
	if packet.IdempotencyKey == "" {
		packet.IdempotencyKey = defaultPacketIdempotencyKey(packet)
	}
	if packet.IdempotencyKey != "" {
		existing, err := s.store.GetPacketByIdempotency(ctx, packet.ProjectID, packet.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}
	packet.ValidationStatus = PacketStatusPendingMessageAppend
	packet.AcceptedAt = nil
	packet.MessageID = nil
	reserved, err := s.store.StorePacket(ctx, packet)
	if err != nil {
		return nil, err
	}
	message, err := s.messages.AppendTaskMessage(ctx, packet.ProjectID, AppendMessageRequest{
		TaskID: packet.TaskID, ThreadID: threadID, Sender: packet.Sender, Content: packet.SourceMarkdown,
		Intent:   intentForPacket(packet.PacketKind, stringValue(packet.TypedEnvelope["verdict"])),
		Metadata: packet.TypedEnvelope,
	})
	if err != nil {
		return reserved, err
	}
	packet.ID = reserved.ID
	packet.MessageID = &message.ID
	packet.ValidationStatus = PacketStatusAccepted
	now := s.clock().UTC()
	packet.AcceptedAt = &now
	return s.store.StorePacket(ctx, packet)
}

func (s *Service) validatePacketContext(ctx context.Context, packet *ReviewPacket, projectID string, taskID int64) error {
	if packet.ProjectID != strings.TrimSpace(projectID) {
		return validationError(fmt.Errorf("project mismatch"), "project_mismatch", "project_id", "common.project_id")
	}
	if packet.TaskID != taskID {
		return validationError(fmt.Errorf("task mismatch"), "task_id_mismatch", "task_id", "common.task_id")
	}
	task, err := s.validateTask(ctx, packet.ProjectID, packet.TaskID, allowedStatusesForPacket(packet.PacketKind)...)
	if err != nil {
		return err
	}
	if task.Status == TaskStatusDone || task.Status == TaskStatusCancelled {
		return validationError(ErrInvalidTaskState, "task_not_reviewable", "task_id", "common.task_id")
	}
	if packet.ReviewRoundID != nil {
		round, err := s.store.GetRound(ctx, *packet.ReviewRoundID)
		if err != nil {
			return err
		}
		if round.ProjectID != packet.ProjectID || round.TaskID != packet.TaskID {
			return validationError(fmt.Errorf("review round mismatch"), "review_round_mismatch", "review_round_id", packet.PacketKind+".review_round_id")
		}
		if requiresReviewedHead(packet.PacketKind) && stringValue(packet.TypedEnvelope["reviewed_head_commit"]) != round.HeadCommit {
			return validationError(fmt.Errorf("reviewed head does not match round"), "stale_reviewed_head", "reviewed_head_commit", packet.PacketKind+".reviewed_head_commit")
		}
	}
	return nil
}

func (s *Service) validateTask(ctx context.Context, projectID string, taskID int64, allowed ...string) (TaskContext, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return TaskContext{}, validationError(ErrMissingProjectID, "missing_project_id", "project_id", "common.project_id")
	}
	if taskID == 0 {
		return TaskContext{}, validationError(ErrMissingTaskID, "missing_task_id", "task_id", "common.task_id")
	}
	task, err := s.tasks.GetTaskContext(ctx, projectID, taskID)
	if err != nil {
		return TaskContext{}, err
	}
	if task.ProjectID != projectID {
		return TaskContext{}, validationError(fmt.Errorf("task %d is not in project %s", taskID, projectID), "project_mismatch", "task_id", "common.task_id")
	}
	if len(allowed) > 0 && !contains(allowed, task.Status) {
		return TaskContext{}, validationError(fmt.Errorf("%w: %s", ErrInvalidTaskState, task.Status), "task_not_reviewable", "task_id", "common.task_id")
	}
	return task, nil
}

func (s *Service) resolveTask(ctx context.Context, taskID int64) (TaskContext, error) {
	if taskID == 0 {
		return TaskContext{}, validationError(ErrMissingTaskID, "missing_task_id", "task_id", "common.task_id")
	}
	return s.tasks.GetTask(ctx, taskID)
}

func roundFromRequest(projectID string, taskID int64, req CreateReviewRoundRequest, now time.Time) (*ReviewRound, error) {
	if strings.TrimSpace(req.RequestedBy) == "" {
		return nil, validationError(ErrMissingActor, "missing_actor", "requested_by", "review_request.requested_by")
	}
	for field, value := range map[string]string{"branch": req.Branch, "base_branch": req.BaseBranch, "base_commit": req.BaseCommit, "head_commit": req.HeadCommit} {
		if strings.TrimSpace(value) == "" {
			return nil, validationError(fmt.Errorf("%s is required", field), "missing_"+field, field, "review_request."+field)
		}
	}
	if negative(req.CommitsSinceLastReview) || negative(req.InheritedCommitCount) || negative(req.TaskLocalCommitCount) {
		return nil, validationError(fmt.Errorf("commit counts must be non-negative"), "invalid_commit_count", "commits_since_last_review", "review_request.commits_since_last_review")
	}
	round := &ReviewRound{
		ProjectID: projectID, TaskID: taskID, RequestedBy: strings.TrimSpace(req.RequestedBy),
		Branch: strings.TrimSpace(req.Branch), BaseBranch: strings.TrimSpace(req.BaseBranch),
		BaseCommit: strings.TrimSpace(req.BaseCommit), HeadCommit: strings.TrimSpace(req.HeadCommit),
		LastReviewedHeadCommit: strings.TrimSpace(req.LastReviewedHeadCommit), CommitsSinceLastReview: req.CommitsSinceLastReview,
		TestsRun: trimSlice(req.TestsRun), Notes: strings.TrimSpace(req.Notes),
		PreferredDiffBaseRef:    firstNonEmpty(req.PreferredDiffBaseRef, req.BaseBranch),
		PreferredDiffBaseCommit: firstNonEmpty(req.PreferredDiffBaseCommit, req.BaseCommit),
		PreferredDiffHeadRef:    firstNonEmpty(req.PreferredDiffHeadRef, req.Branch),
		PreferredDiffHeadCommit: firstNonEmpty(req.PreferredDiffHeadCommit, req.HeadCommit),
		AlternateDiffBaseRef:    strings.TrimSpace(req.AlternateDiffBaseRef),
		AlternateDiffBaseCommit: strings.TrimSpace(req.AlternateDiffBaseCommit),
		AlternateDiffHeadRef:    strings.TrimSpace(req.AlternateDiffHeadRef),
		AlternateDiffHeadCommit: strings.TrimSpace(req.AlternateDiffHeadCommit),
		DeltaBaseCommit:         strings.TrimSpace(req.DeltaBaseCommit), InheritedCommitCount: req.InheritedCommitCount,
		TaskLocalCommitCount: req.TaskLocalCommitCount, RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if round.AlternateDiffBaseRef != "" || round.AlternateDiffBaseCommit != "" || round.AlternateDiffHeadRef != "" || round.AlternateDiffHeadCommit != "" {
		if round.AlternateDiffBaseRef == "" || round.AlternateDiffBaseCommit == "" {
			return nil, validationError(fmt.Errorf("alternate diff base ref and commit are required"), "invalid_alternate_diff", "alternate_diff", "review_request.alternate_diff")
		}
		round.AlternateDiffHeadRef = firstNonEmpty(round.AlternateDiffHeadRef, round.Branch)
		round.AlternateDiffHeadCommit = firstNonEmpty(round.AlternateDiffHeadCommit, round.HeadCommit)
	}
	return round, nil
}

func validateFollowUpStatus(status string, followUpTaskID *int64) error {
	if followUpTaskID != nil && status != StatusSplitToFollowUp {
		return validationError(ErrFollowUpStatusMismatch, "follow_up_status_mismatch", "follow_up_task_id", "review_findings.follow_up_task_id")
	}
	return nil
}

func allowedStatusesForPacket(kind string) []string {
	switch kind {
	case PacketKindReviewRequest, PacketKindRereviewRequest, PacketKindResponse:
		return []string{TaskStatusInProgress, TaskStatusReview}
	case PacketKindReviewFindings, PacketKindCompletion:
		return []string{TaskStatusReview}
	default:
		return nil
	}
}

func requiresReviewedHead(kind string) bool {
	return kind == PacketKindReviewFindings || kind == PacketKindResponse || kind == PacketKindCompletion
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func negative(value *int) bool {
	return value != nil && *value < 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var githubSHARegex = regexp.MustCompile(`^[0-9a-f]{40}$`) //nolint:gochecknoglobals

func validGitHubSHA(value string) bool {
	return githubSHARegex.MatchString(value)
}

func validGitHubRepository(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != ""
}
