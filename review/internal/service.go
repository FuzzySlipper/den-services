package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultGitHubCheckPollInterval = 30 * time.Second
	defaultGitHubMissingCheckGrace = 2 * time.Minute
	defaultGitHubHTTPErrorBackoff  = 15 * time.Minute
	defaultGitHubEventWaitMax      = 55 * time.Second
	defaultGitHubEventWaitPoll     = 500 * time.Millisecond
	defaultGitHubToolWaitMax       = 50 * time.Second
)

type ProjectValidator interface {
	AssertWritable(ctx context.Context, projectID string) error
}

type TaskClient interface {
	GetTask(ctx context.Context, taskID int64) (TaskContext, error)
	GetTaskContext(ctx context.Context, projectID string, taskID int64) (TaskContext, error)
	SetTaskStatus(ctx context.Context, projectID string, taskID int64, agent string, status string) (TaskContext, error)
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
	GetReviewFindingsPacketForRound(ctx context.Context, roundID int64) (*ReviewPacket, error)
	GetFinalizationByRound(ctx context.Context, roundID int64) (*ReviewFinalization, error)
	BeginFinalization(ctx context.Context, finalization *ReviewFinalization, packet *ReviewPacket, decidedAt time.Time) (*ReviewFinalization, *ReviewPacket, *ReviewRound, error)
	MarkFinalizationPacketPosted(ctx context.Context, id int64, messageID int64, postedAt time.Time) (*ReviewFinalization, *ReviewPacket, error)
	MarkFinalizationTaskTransitioned(ctx context.Context, id int64, transitionedAt time.Time) (*ReviewFinalization, error)
	CompleteFinalization(ctx context.Context, id int64, completedAt time.Time) (*ReviewFinalization, error)
	RecordFinalizationError(ctx context.Context, id int64, step string, message string, attemptedAt time.Time) (*ReviewFinalization, error)
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
	ListGitHubCheckGateEvents(ctx context.Context, query ListGitHubCheckGateEventsQuery) ([]*GitHubCheckGateTerminalEvent, error)
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
	ReviewRoundID          int64                    `json:"review_round_id"`
	RoundNumber            int                      `json:"round_number"`
	TargetKind             string                   `json:"target_kind"`
	CampaignChildren       []CampaignReviewChild    `json:"campaign_children,omitempty"`
	CampaignRepositories   []CampaignRepositoryHead `json:"campaign_repositories,omitempty"`
	Branch                 string                   `json:"branch,omitempty"`
	RequestedBy            string                   `json:"requested_by"`
	RequestedAt            time.Time                `json:"requested_at"`
	HeadCommit             string                   `json:"head_commit,omitempty"`
	LastReviewedHeadCommit string                   `json:"last_reviewed_head_commit,omitempty"`
	CommitsSinceLastReview *int                     `json:"commits_since_last_review,omitempty"`
	Verdict                string                   `json:"verdict,omitempty"`
	VerdictBy              string                   `json:"verdict_by,omitempty"`
	VerdictAt              *time.Time               `json:"verdict_at,omitempty"`
	TotalFindings          int                      `json:"total_findings"`
	OpenFindings           int                      `json:"open_findings"`
	AddressedFindings      int                      `json:"addressed_findings"`
	ClaimedFixedFindings   int                      `json:"claimed_fixed_findings"`
	ResolvedFindings       int                      `json:"resolved_findings"`
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
	DefaultTimeout    time.Duration
	MaxTimeout        time.Duration
	PollInterval      time.Duration
	MissingCheckGrace time.Duration
	StatusURLBase     string
	EventWaitMax      time.Duration
	EventWaitPoll     time.Duration
}

func DefaultGitHubCheckOptions() GitHubCheckOptions {
	return GitHubCheckOptions{
		DefaultTimeout:    2 * time.Hour,
		MaxTimeout:        12 * time.Hour,
		PollInterval:      defaultGitHubCheckPollInterval,
		MissingCheckGrace: defaultGitHubMissingCheckGrace,
		EventWaitMax:      defaultGitHubEventWaitMax,
		EventWaitPoll:     defaultGitHubEventWaitPoll,
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
	if options.PollInterval < defaultGitHubCheckPollInterval {
		options.PollInterval = defaultGitHubCheckPollInterval
	}
	if options.MissingCheckGrace <= 0 {
		options.MissingCheckGrace = DefaultGitHubCheckOptions().MissingCheckGrace
	}
	if options.EventWaitMax <= 0 {
		options.EventWaitMax = DefaultGitHubCheckOptions().EventWaitMax
	}
	if options.EventWaitPoll <= 0 {
		options.EventWaitPoll = DefaultGitHubCheckOptions().EventWaitPoll
	}
	s.githubChecks = provider
	s.githubOptions = options
}

func (s *Service) WaitGitHubCheckGateEvents(ctx context.Context, query ListGitHubCheckGateEventsQuery, wait time.Duration) (GitHubCheckGateEventPage, error) {
	query.ProjectID = strings.TrimSpace(query.ProjectID)
	if query.ProjectID == "" {
		return GitHubCheckGateEventPage{}, badRequest(ErrMissingProjectID)
	}
	if query.TaskID < 0 || query.AfterID < 0 || query.Limit < 0 || wait < 0 {
		return GitHubCheckGateEventPage{}, badRequest(errors.New("task_id, after_id, limit, and wait must not be negative"))
	}
	if query.Limit == 0 {
		query.Limit = 50
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	if wait > s.githubOptions.EventWaitMax {
		wait = s.githubOptions.EventWaitMax
	}
	var timeout <-chan time.Time
	var timeoutTimer *time.Timer
	if wait > 0 {
		timeoutTimer = time.NewTimer(wait)
		defer timeoutTimer.Stop()
		timeout = timeoutTimer.C
	}
	for {
		events, err := s.store.ListGitHubCheckGateEvents(ctx, query)
		if err != nil {
			return GitHubCheckGateEventPage{}, err
		}
		if len(events) > 0 {
			return GitHubCheckGateEventPage{Events: events, NextCursor: events[len(events)-1].ID}, nil
		}
		if wait == 0 {
			return GitHubCheckGateEventPage{Events: []*GitHubCheckGateTerminalEvent{}, NextCursor: query.AfterID}, nil
		}
		timer := time.NewTimer(s.githubOptions.EventWaitPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return GitHubCheckGateEventPage{}, ctx.Err()
		case <-timeout:
			timer.Stop()
			return GitHubCheckGateEventPage{Events: []*GitHubCheckGateTerminalEvent{}, NextCursor: query.AfterID, TimedOut: true}, nil
		case <-timer.C:
		}
	}
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
	task, err := s.validateTask(ctx, projectID, taskID, TaskStatusInProgress, TaskStatusReview)
	if err != nil {
		return nil, err
	}
	if err := s.projects.AssertWritable(ctx, task.ProjectID); err != nil {
		return nil, err
	}
	requestedRound, err := roundFromRequest(task.ProjectID, taskID, req, s.clock().UTC())
	if err != nil {
		return nil, err
	}
	round, err := s.reusableOrNewReviewRound(ctx, requestedRound)
	if err != nil {
		return nil, err
	}
	kind := PacketKindReviewRequest
	if round.RoundNumber > 1 {
		kind = PacketKindRereviewRequest
	}
	packet := packetForRound(round, kind, req.ThreadID, req.RunID)
	packet, err = s.acceptPacket(ctx, packet, req.ThreadID)
	if err != nil {
		return nil, err
	}
	if task.Status == TaskStatusReview {
		packet.TaskTransition = TaskTransitionAlreadySatisfied
		packet.ResultingTaskStatus = TaskStatusReview
		return packet, nil
	}
	updated, err := s.tasks.SetTaskStatus(ctx, task.ProjectID, task.ID, req.RequestedBy, TaskStatusReview)
	if err != nil {
		return nil, NewServiceError(
			fmt.Errorf("transitioning task to review: %w", err),
			"task_transition_retryable",
			http.StatusServiceUnavailable,
		)
	}
	if updated.Status != TaskStatusReview {
		return nil, fmt.Errorf("task status transition returned %q, want %q", updated.Status, TaskStatusReview)
	}
	packet.TaskTransition = TaskTransitionApplied
	packet.ResultingTaskStatus = updated.Status
	return packet, nil
}

func (s *Service) reusableOrNewReviewRound(ctx context.Context, requested *ReviewRound) (*ReviewRound, error) {
	if existing, err := s.reusableReviewRound(ctx, requested); err != nil || existing != nil {
		return existing, err
	}
	round, err := s.store.CreateRound(ctx, requested)
	if err == nil {
		return round, nil
	}
	// Another identical request may have committed between the read and create.
	// Re-read before returning the create conflict so concurrent callers converge
	// on the canonical undecided round.
	if existing, reuseErr := s.reusableReviewRound(ctx, requested); reuseErr != nil || existing != nil {
		return existing, reuseErr
	}
	return nil, err
}

func (s *Service) reusableReviewRound(ctx context.Context, requested *ReviewRound) (*ReviewRound, error) {
	rounds, err := s.store.ListRounds(ctx, requested.ProjectID, requested.TaskID)
	if err != nil {
		return nil, err
	}
	var latest *ReviewRound
	for _, candidate := range rounds {
		if latest == nil || candidate.RoundNumber > latest.RoundNumber {
			latest = candidate
		}
	}
	if latest != nil && latest.Verdict == "" && sameReviewRequest(latest, requested) {
		return latest, nil
	}
	return nil, nil
}

func (s *Service) RequestCampaignReview(ctx context.Context, projectID string, taskID int64, req CreateCampaignReviewRequest) (*ReviewPacket, error) {
	parent, err := s.validateTask(ctx, projectID, taskID, TaskStatusInProgress, TaskStatusReview)
	if err != nil {
		return nil, err
	}
	if err := s.projects.AssertWritable(ctx, parent.ProjectID); err != nil {
		return nil, err
	}
	round, err := s.campaignRoundFromRequest(ctx, parent, req)
	if err != nil {
		return nil, err
	}
	existingRounds, err := s.store.ListRounds(ctx, parent.ProjectID, parent.ID)
	if err != nil {
		return nil, err
	}
	var latest *ReviewRound
	for _, candidate := range existingRounds {
		if latest == nil || candidate.RoundNumber > latest.RoundNumber {
			latest = candidate
		}
	}
	if latest != nil && latest.Verdict == "" && sameCampaignRequest(latest, round) {
		kind := PacketKindReviewRequest
		if latest.RoundNumber > 1 {
			kind = PacketKindRereviewRequest
		}
		return s.acceptPacket(ctx, packetForRound(latest, kind, req.ThreadID, req.RunID), req.ThreadID)
	}
	round, err = s.store.CreateRound(ctx, round)
	if err != nil {
		return nil, err
	}
	kind := PacketKindReviewRequest
	if round.RoundNumber > 1 {
		kind = PacketKindRereviewRequest
	}
	return s.acceptPacket(ctx, packetForRound(round, kind, req.ThreadID, req.RunID), req.ThreadID)
}

func (s *Service) campaignRoundFromRequest(ctx context.Context, parent TaskContext, req CreateCampaignReviewRequest) (*ReviewRound, error) {
	requestedBy := strings.TrimSpace(req.RequestedBy)
	if requestedBy == "" {
		return nil, validationError(ErrMissingActor, "missing_actor", "requested_by", "campaign_review_request.requested_by")
	}
	if len(req.Children) == 0 {
		return nil, validationError(ErrMissingCampaignChild, "missing_campaign_children", "children", "campaign_review_request.children")
	}
	if len(req.Repositories) == 0 {
		return nil, validationError(ErrMissingCampaignHead, "missing_campaign_repositories", "repositories", "campaign_review_request.repositories")
	}

	repositories := make([]CampaignRepositoryHead, 0, len(req.Repositories))
	heads := make(map[string]struct{}, len(req.Repositories))
	seenRepositories := make(map[string]struct{}, len(req.Repositories))
	for _, repository := range req.Repositories {
		name := strings.TrimSpace(repository.Repository)
		head := strings.TrimSpace(repository.HeadSHA)
		if !validGitHubRepository(name) || !validGitHubSHA(head) {
			return nil, validationError(ErrMissingCampaignHead, "invalid_campaign_repository_head", "repositories", "campaign_review_request.repositories")
		}
		if _, exists := seenRepositories[name]; exists {
			return nil, validationError(ErrDuplicateCampaignRepo, "duplicate_campaign_repository", "repositories", "campaign_review_request.repositories")
		}
		seenRepositories[name] = struct{}{}
		heads[head] = struct{}{}
		repositories = append(repositories, CampaignRepositoryHead{Repository: name, HeadSHA: head})
	}

	children := make([]CampaignReviewChild, 0, len(req.Children))
	seenTasks := make(map[string]struct{}, len(req.Children))
	seenRounds := make(map[int64]struct{}, len(req.Children))
	for _, requestedChild := range req.Children {
		childProjectID := strings.TrimSpace(requestedChild.ProjectID)
		if childProjectID == "" || requestedChild.TaskID == 0 || requestedChild.ReviewRoundID == 0 {
			return nil, validationError(ErrMissingCampaignChild, "invalid_campaign_child", "children", "campaign_review_request.children")
		}
		taskKey := fmt.Sprintf("%s:%d", childProjectID, requestedChild.TaskID)
		if _, exists := seenTasks[taskKey]; exists {
			return nil, validationError(ErrDuplicateCampaignChild, "duplicate_campaign_child", "children", "campaign_review_request.children")
		}
		if _, exists := seenRounds[requestedChild.ReviewRoundID]; exists {
			return nil, validationError(ErrDuplicateCampaignRound, "duplicate_campaign_review_round", "children", "campaign_review_request.children")
		}
		seenTasks[taskKey] = struct{}{}
		seenRounds[requestedChild.ReviewRoundID] = struct{}{}

		childTask, err := s.tasks.GetTaskContext(ctx, childProjectID, requestedChild.TaskID)
		if err != nil {
			return nil, err
		}
		membershipKind := campaignMembership(parent, childTask)
		if membershipKind == "" {
			return nil, validationError(ErrUnrelatedCampaignChild, "unrelated_campaign_child", "children", "campaign_review_request.children")
		}
		childRound, err := s.store.GetRound(ctx, requestedChild.ReviewRoundID)
		if err != nil {
			if errors.Is(err, ErrMissingRound) {
				return nil, validationError(ErrMissingCampaignRound, "missing_campaign_review_round", "children", "campaign_review_request.children")
			}
			return nil, err
		}
		if childRound.ProjectID != childProjectID || childRound.TaskID != requestedChild.TaskID {
			return nil, validationError(ErrMissingCampaignChild, "campaign_round_task_mismatch", "children", "campaign_review_request.children")
		}
		rounds, err := s.store.ListRounds(ctx, childProjectID, requestedChild.TaskID)
		if err != nil {
			return nil, err
		}
		var latest *ReviewRound
		for _, candidate := range rounds {
			if latest == nil || candidate.RoundNumber > latest.RoundNumber {
				latest = candidate
			}
		}
		if latest == nil || latest.ID != childRound.ID {
			return nil, validationError(ErrStaleCampaignRound, "stale_campaign_review_round", "children", "campaign_review_request.children")
		}
		if childRound.Verdict != VerdictLooksGood {
			return nil, validationError(ErrUnapprovedCampaignRound, "unapproved_campaign_review_round", "children", "campaign_review_request.children")
		}
		findings, err := s.store.ListFindings(ctx, ListFindingsQuery{ProjectID: childProjectID, TaskID: requestedChild.TaskID})
		if err != nil {
			return nil, err
		}
		for _, finding := range findings {
			if !resolvedStatus(finding.Status) && (finding.Category == CategoryBlockingBug || finding.Category == CategoryAcceptanceGap) {
				return nil, validationError(ErrBlockedCampaignChild, "blocked_campaign_child", "children", "campaign_review_request.children")
			}
		}
		if _, exists := heads[childRound.HeadCommit]; !exists {
			return nil, validationError(ErrCampaignHeadMismatch, "campaign_head_mismatch", "repositories", "campaign_review_request.repositories")
		}
		children = append(children, CampaignReviewChild{
			ProjectID: childProjectID, TaskID: requestedChild.TaskID, ReviewRoundID: childRound.ID,
			HeadCommit: childRound.HeadCommit, MembershipKind: membershipKind, ApprovedVerdict: childRound.Verdict,
		})
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].ProjectID != children[j].ProjectID {
			return children[i].ProjectID < children[j].ProjectID
		}
		if children[i].TaskID != children[j].TaskID {
			return children[i].TaskID < children[j].TaskID
		}
		return children[i].ReviewRoundID < children[j].ReviewRoundID
	})
	sort.Slice(repositories, func(i, j int) bool {
		return repositories[i].Repository < repositories[j].Repository
	})

	now := s.clock().UTC()
	return &ReviewRound{
		ProjectID: parent.ProjectID, TaskID: parent.ID, RequestedBy: requestedBy,
		TargetKind: ReviewTargetCampaignReconciliation, CampaignChildren: children, CampaignRepositories: repositories,
		TestsRun: trimSlice(req.TestsRun), Notes: strings.TrimSpace(req.Notes),
		RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func sameCampaignRequest(existing *ReviewRound, requested *ReviewRound) bool {
	if existing.TargetKind != ReviewTargetCampaignReconciliation ||
		existing.RequestedBy != requested.RequestedBy ||
		existing.Notes != requested.Notes ||
		!equalStrings(existing.TestsRun, requested.TestsRun) ||
		len(existing.CampaignChildren) != len(requested.CampaignChildren) ||
		len(existing.CampaignRepositories) != len(requested.CampaignRepositories) {
		return false
	}
	for i := range existing.CampaignChildren {
		if existing.CampaignChildren[i] != requested.CampaignChildren[i] {
			return false
		}
	}
	for i := range existing.CampaignRepositories {
		if existing.CampaignRepositories[i] != requested.CampaignRepositories[i] {
			return false
		}
	}
	return true
}

func sameReviewRequest(existing *ReviewRound, requested *ReviewRound) bool {
	return existing.TargetKind == ReviewTargetCodeDiff &&
		existing.RequestedBy == requested.RequestedBy &&
		existing.Branch == requested.Branch &&
		existing.BaseBranch == requested.BaseBranch &&
		existing.BaseCommit == requested.BaseCommit &&
		existing.HeadCommit == requested.HeadCommit &&
		(requested.LastReviewedHeadCommit == "" || existing.LastReviewedHeadCommit == requested.LastReviewedHeadCommit) &&
		equalOptionalInts(existing.CommitsSinceLastReview, requested.CommitsSinceLastReview) &&
		equalStrings(existing.TestsRun, requested.TestsRun) &&
		existing.Notes == requested.Notes &&
		existing.PreferredDiffBaseRef == requested.PreferredDiffBaseRef &&
		existing.PreferredDiffBaseCommit == requested.PreferredDiffBaseCommit &&
		existing.PreferredDiffHeadRef == requested.PreferredDiffHeadRef &&
		existing.PreferredDiffHeadCommit == requested.PreferredDiffHeadCommit &&
		existing.AlternateDiffBaseRef == requested.AlternateDiffBaseRef &&
		existing.AlternateDiffBaseCommit == requested.AlternateDiffBaseCommit &&
		existing.AlternateDiffHeadRef == requested.AlternateDiffHeadRef &&
		existing.AlternateDiffHeadCommit == requested.AlternateDiffHeadCommit &&
		(requested.DeltaBaseCommit == "" || existing.DeltaBaseCommit == requested.DeltaBaseCommit) &&
		equalOptionalInts(existing.InheritedCommitCount, requested.InheritedCommitCount) &&
		equalOptionalInts(existing.TaskLocalCommitCount, requested.TaskLocalCommitCount)
}

func equalOptionalInts(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func campaignMembership(parent TaskContext, child TaskContext) string {
	if child.ProjectID == parent.ProjectID && child.ParentID != nil && *child.ParentID == parent.ID {
		return CampaignMembershipDirectSubtask
	}
	tag := fmt.Sprintf("campaign:%s:%d", parent.ProjectID, parent.ID)
	if contains(parent.Tags, tag) && contains(child.Tags, tag) {
		return CampaignMembershipTag
	}
	return ""
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
	if !validCompatibilityVerdict(verdict) {
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

func (s *Service) FinalizeReview(ctx context.Context, req FinalizeReviewRequest) (*ReviewFinalizationReceipt, error) {
	req.ReviewRoundID = max(req.ReviewRoundID, 0)
	req.Verdict = strings.TrimSpace(req.Verdict)
	req.DecidedBy = strings.TrimSpace(req.DecidedBy)
	req.Notes = strings.TrimSpace(req.Notes)
	req.RunID = strings.TrimSpace(req.RunID)
	req.SubagentRole = strings.TrimSpace(req.SubagentRole)
	if req.ReviewRoundID == 0 {
		return nil, validationError(ErrMissingRound, "missing_review_round_id", "review_round_id", "review_findings.review_round_id")
	}
	if !validFinalizationVerdict(req.Verdict) {
		return nil, validationError(fmt.Errorf("%w: %s", ErrInvalidVerdict, req.Verdict), "invalid_verdict", "verdict", "review_findings.verdict")
	}
	if req.DecidedBy == "" {
		return nil, validationError(ErrMissingActor, "missing_actor", "decided_by", "review_findings.decided_by")
	}

	existing, err := s.store.GetFinalizationByRound(ctx, req.ReviewRoundID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.Verdict != req.Verdict || existing.DecidedBy != req.DecidedBy {
			return nil, conflict(ErrFinalizationConflict, "review_finalization_conflict")
		}
		packet, err := s.store.GetPacket(ctx, existing.PacketID)
		if err != nil {
			return nil, err
		}
		round, err := s.store.GetRound(ctx, existing.ReviewRoundID)
		if err != nil {
			return nil, err
		}
		return s.resumeFinalization(ctx, &ReviewFinalizationReceipt{
			Finalization: existing, Round: round, Packet: packet, TaskStatus: existing.TargetTaskStatus,
		})
	}

	round, err := s.store.GetRound(ctx, req.ReviewRoundID)
	if err != nil {
		return nil, err
	}
	task, err := s.validateTask(ctx, round.ProjectID, round.TaskID, TaskStatusReview)
	if err != nil {
		return nil, err
	}
	roundFindings, err := s.store.ListFindings(ctx, ListFindingsQuery{
		ProjectID: round.ProjectID, TaskID: round.TaskID, ReviewRoundID: &round.ID,
	})
	if err != nil {
		return nil, err
	}
	allFindings, err := s.store.ListFindings(ctx, ListFindingsQuery{ProjectID: round.ProjectID, TaskID: round.TaskID})
	if err != nil {
		return nil, err
	}
	if err := validateFinalizationFindings(req.Verdict, roundFindings, allFindings); err != nil {
		return nil, err
	}

	now := s.clock().UTC()
	decidedRound := *round
	decidedRound.Verdict = req.Verdict
	decidedRound.VerdictBy = req.DecidedBy
	decidedRound.VerdictNotes = req.Notes
	decidedRound.VerdictAt = &now
	decidedRound.UpdatedAt = now
	packet := reviewFindingsPacket(&decidedRound, roundFindings, unresolvedFindingSummaries(allFindings), PostReviewFindingsRequest{
		ReviewRoundID: round.ID, Sender: req.DecidedBy, ThreadID: req.ThreadID, Notes: req.Notes,
		RunID: req.RunID, SubagentRole: req.SubagentRole,
	})
	packet.IdempotencyKey = finalizationPacketKey(round.ID, req.Verdict, req.DecidedBy)
	finalization := &ReviewFinalization{
		ProjectID: round.ProjectID, TaskID: round.TaskID, ReviewRoundID: round.ID,
		Verdict: req.Verdict, DecidedBy: req.DecidedBy, Notes: req.Notes, ThreadID: req.ThreadID,
		RunID: req.RunID, SubagentRole: req.SubagentRole, TargetTaskStatus: taskStatusForVerdict(req.Verdict),
		IdempotencyKey:       finalizationKey(round.ID, req.Verdict, req.DecidedBy),
		PacketIdempotencyKey: packet.IdempotencyKey, State: FinalizationStatePending,
		CreatedAt: now, UpdatedAt: now,
	}
	stored, storedPacket, updatedRound, err := s.store.BeginFinalization(ctx, finalization, packet, now)
	if err != nil {
		return nil, err
	}
	if stored.Verdict != req.Verdict || stored.DecidedBy != req.DecidedBy {
		return nil, conflict(ErrFinalizationConflict, "review_finalization_conflict")
	}
	return s.resumeFinalization(ctx, &ReviewFinalizationReceipt{
		Finalization: stored, Round: updatedRound, Packet: storedPacket, TaskStatus: task.Status,
	})
}

func (s *Service) resumeFinalization(ctx context.Context, receipt *ReviewFinalizationReceipt) (*ReviewFinalizationReceipt, error) {
	finalization := receipt.Finalization
	if finalization.State == FinalizationStateComplete {
		receipt.TaskStatus = finalization.TargetTaskStatus
		return receipt, nil
	}
	if finalization.PacketPostedAt == nil {
		if receipt.Packet.MessageID != nil {
			updated, packet, err := s.store.MarkFinalizationPacketPosted(ctx, finalization.ID, *receipt.Packet.MessageID, s.clock().UTC())
			if err != nil {
				return receipt, err
			}
			receipt.Finalization, receipt.Packet = updated, packet
			finalization = updated
		} else {
			message, err := s.appendPacketMessage(ctx, receipt.Packet, finalization.ThreadID)
			if err != nil {
				return s.finalizationFailure(ctx, receipt, FinalizationStepPacketDelivery, err)
			}
			updated, packet, err := s.store.MarkFinalizationPacketPosted(ctx, finalization.ID, message.ID, s.clock().UTC())
			if err != nil {
				return receipt, err
			}
			receipt.Finalization, receipt.Packet = updated, packet
			finalization = updated
		}
	}
	if finalization.TaskTransitionedAt == nil {
		task, err := s.tasks.GetTaskContext(ctx, finalization.ProjectID, finalization.TaskID)
		if err != nil {
			return s.finalizationFailure(ctx, receipt, FinalizationStepTaskTransition, err)
		}
		if task.Status != finalization.TargetTaskStatus {
			if task.Status != TaskStatusReview {
				return s.finalizationFailure(ctx, receipt, FinalizationStepTaskTransition,
					fmt.Errorf("task status changed to %s before finalization could apply %s", task.Status, finalization.TargetTaskStatus))
			}
			task, err = s.tasks.SetTaskStatus(ctx, finalization.ProjectID, finalization.TaskID, finalization.DecidedBy, finalization.TargetTaskStatus)
			if err != nil {
				return s.finalizationFailure(ctx, receipt, FinalizationStepTaskTransition, err)
			}
			if task.Status != finalization.TargetTaskStatus {
				return s.finalizationFailure(ctx, receipt, FinalizationStepTaskTransition,
					fmt.Errorf("tasks returned status %s after requesting %s", task.Status, finalization.TargetTaskStatus))
			}
		}
		updated, err := s.store.MarkFinalizationTaskTransitioned(ctx, finalization.ID, s.clock().UTC())
		if err != nil {
			return receipt, err
		}
		receipt.Finalization = updated
		receipt.TaskStatus = task.Status
		finalization = updated
	}
	completed, err := s.store.CompleteFinalization(ctx, finalization.ID, s.clock().UTC())
	if err != nil {
		return s.finalizationFailure(ctx, receipt, FinalizationStepCompletion, err)
	}
	receipt.Finalization = completed
	receipt.TaskStatus = completed.TargetTaskStatus
	return receipt, nil
}

func (s *Service) finalizationFailure(
	ctx context.Context,
	receipt *ReviewFinalizationReceipt,
	step string,
	cause error,
) (*ReviewFinalizationReceipt, error) {
	updated, recordErr := s.store.RecordFinalizationError(ctx, receipt.Finalization.ID, step, cause.Error(), s.clock().UTC())
	if recordErr == nil {
		receipt.Finalization = updated
	}
	status := http.StatusBadGateway
	code := "review_finalization_" + step + "_failed"
	if step == FinalizationStepCompletion {
		status = http.StatusInternalServerError
	}
	if recordErr != nil {
		cause = errors.Join(cause, fmt.Errorf("recording finalization error: %w", recordErr))
	}
	return receipt, NewServiceError(cause, code, status)
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
	task, err := s.validateTask(ctx, projectID, taskID, TaskStatusReview, TaskStatusInProgress, TaskStatusDone)
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
	existing, err := s.store.GetReviewFindingsPacketForRound(ctx, round.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.ValidationStatus == PacketStatusAccepted && existing.MessageID != nil {
			return existing, nil
		}
		return s.deliverReservedPacket(ctx, existing, req.ThreadID)
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
	packet.IdempotencyKey = fmt.Sprintf("review-findings:%d", round.ID)
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

func (s *Service) DiscoverGitHubChecks(ctx context.Context, req DiscoverGitHubChecksRequest) (*GitHubCheckDiscovery, error) {
	repository := strings.TrimSpace(req.Repository)
	if !validGitHubRepository(repository) {
		return nil, validationError(fmt.Errorf("repository must be owner/name"), "invalid_repository", "repository", "github_check_discovery.repository")
	}
	commitSHA := strings.ToLower(strings.TrimSpace(req.CommitSHA))
	if !validGitHubSHA(commitSHA) {
		return nil, validationError(fmt.Errorf("commit_sha must be a full 40-character hex SHA"), "invalid_commit_sha", "commit_sha", "github_check_discovery.commit_sha")
	}
	if s.githubChecks == nil {
		return nil, NewServiceError(ErrGitHubChecksUnset, "github_checks_unconfigured", http.StatusInternalServerError)
	}
	requiredChecks := trimSlice(req.RequiredChecks)
	result, err := s.githubChecks.CheckCommit(ctx, repository, commitSHA, requiredChecks)
	if err != nil {
		return nil, err
	}
	configurationStatus := GitHubCheckDiscoveryNotValidated
	summary := "Observed GitHub check runs for the exact commit."
	if len(result.ObservedCheckRuns) == 0 {
		summary = "No GitHub check runs are currently visible for the exact commit."
	}
	if len(requiredChecks) > 0 {
		configurationStatus = GitHubCheckDiscoveryValid
		summary = "Every requested GitHub check name exactly matches an observed check run."
		if len(result.MissingRequiredChecks) > 0 {
			configurationStatus = GitHubCheckDiscoveryMissing
			summary = "One or more requested GitHub check names do not match the check runs currently observed for the exact commit."
		}
	}
	return &GitHubCheckDiscovery{
		Repository: repository, CommitSHA: commitSHA, RequiredChecks: requiredChecks,
		ConfigurationStatus: configurationStatus, Summary: summary,
		ObservedCheckRuns: result.ObservedCheckRuns, MissingRequiredChecks: result.MissingRequiredChecks,
		AllObservedChecksTerminal: result.AllObservedChecksTerminal,
	}, nil
}

func (s *Service) RegisterGitHubCheckGate(ctx context.Context, projectID string, taskID int64, req RegisterGitHubCheckGateRequest) (*GitHubCheckGate, error) {
	task, err := s.validateTask(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	gate, err := s.githubGateFromRequest(task.ProjectID, taskID, req)
	if err != nil {
		return nil, err
	}
	if task.Status != TaskStatusReview {
		task, err = s.tasks.SetTaskStatus(ctx, task.ProjectID, taskID, req.RequestedBy, TaskStatusReview)
		if err != nil {
			return nil, err
		}
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
	return s.evaluateGitHubCheckGate(ctx, stored, true)
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

func (s *Service) WaitForGitHubCheckGate(ctx context.Context, projectID string, taskID int64, commitSHA string, afterID int64, wait time.Duration) (GitHubCheckGateWaitReceipt, error) {
	if afterID < 0 || wait < 0 {
		return GitHubCheckGateWaitReceipt{}, badRequest(errors.New("after_id and wait must not be negative"))
	}
	if wait > defaultGitHubToolWaitMax {
		wait = defaultGitHubToolWaitMax
	}
	gate, err := s.GetGitHubCheckGate(ctx, projectID, taskID, commitSHA)
	if err != nil {
		return GitHubCheckGateWaitReceipt{}, err
	}
	if terminalGitHubCheckGateStatus(gate.Status) {
		return GitHubCheckGateWaitReceipt{Gate: gate, NextCursor: afterID, Terminal: true}, nil
	}
	deadline := time.Now().Add(wait)
	cursor := afterID
	for {
		remaining := time.Until(deadline)
		if wait == 0 || remaining <= 0 {
			return GitHubCheckGateWaitReceipt{Gate: gate, NextCursor: cursor, TimedOut: wait > 0}, nil
		}
		page, waitErr := s.WaitGitHubCheckGateEvents(ctx, ListGitHubCheckGateEventsQuery{
			ProjectID: gate.ProjectID, TaskID: gate.TaskID, AfterID: cursor, Limit: 50,
		}, remaining)
		if waitErr != nil {
			return GitHubCheckGateWaitReceipt{}, waitErr
		}
		cursor = page.NextCursor
		for _, event := range page.Events {
			if event.GateID == gate.ID {
				terminalGate, getErr := s.store.GetGitHubCheckGate(ctx, gate.ProjectID, gate.TaskID, gate.CommitSHA)
				if getErr != nil {
					return GitHubCheckGateWaitReceipt{}, getErr
				}
				return GitHubCheckGateWaitReceipt{Gate: terminalGate, Event: event, NextCursor: cursor, Terminal: true}, nil
			}
		}
		if page.TimedOut {
			return GitHubCheckGateWaitReceipt{Gate: gate, NextCursor: cursor, TimedOut: true}, nil
		}
	}
}

func (s *Service) PollGitHubCheckGates(ctx context.Context, limit int) error {
	if s.githubChecks == nil {
		return NewServiceError(ErrGitHubChecksUnset, "github_checks_unconfigured", 500)
	}
	if limit <= 0 {
		limit = 10
	}
	started := time.Now()
	processed := 0
	var pollErrors []error
	for {
		now := s.clock().UTC()
		gates, err := s.store.ListPendingGitHubCheckGates(ctx, now, limit)
		if err != nil {
			return errors.Join(append(pollErrors, err)...)
		}
		if len(gates) == 0 {
			break
		}
		for _, gate := range gates {
			processed++
			if _, err := s.evaluateGitHubCheckGate(ctx, gate, false); err != nil {
				pollErrors = append(pollErrors, err)
				slog.Warn("github check gate evaluation failed", "gate_id", gate.ID, "commit_sha", gate.CommitSHA, "error", err)
			}
		}
		if len(gates) < limit {
			break
		}
	}
	slog.Info("github check gate scan completed", "processed_gates", processed, "batch_size", limit,
		"duration_ms", time.Since(started).Milliseconds(), "errors", len(pollErrors))
	if err := s.RetryGitHubCheckGateEvidence(ctx, limit); err != nil {
		pollErrors = append(pollErrors, err)
	}
	return errors.Join(pollErrors...)
}

func (s *Service) evaluateGitHubCheckGate(ctx context.Context, gate *GitHubCheckGate, deliverEvidence bool) (*GitHubCheckGate, error) {
	now := s.clock().UTC()
	if !now.Before(gate.TimeoutAt) {
		updated, changed, err := s.store.TimeoutGitHubCheckGate(ctx, gate.ID, now)
		if err != nil {
			return nil, err
		}
		if changed && deliverEvidence {
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
	requestStarted := time.Now()
	result, err := s.githubChecks.CheckCommit(ctx, gate.Repository, gate.CommitSHA, gate.RequiredChecks)
	requestDuration := time.Since(requestStarted)
	if err != nil {
		var githubErr *GitHubHTTPError
		if errors.As(err, &githubErr) && delayableGitHubHTTPStatus(githubErr.StatusCode) {
			slog.Warn("github check gate throttled", "gate_id", gate.ID, "status_code", githubErr.StatusCode,
				"request_id", githubErr.RequestID, "api_duration_ms", requestDuration.Milliseconds())
			return s.delayGitHubCheckGateAfterGitHubHTTPError(ctx, gate, githubErr, now)
		}
		slog.Warn("github check gate transport failed", "gate_id", gate.ID, "api_duration_ms", requestDuration.Milliseconds(), "error", err)
		return s.delayGitHubCheckGateAfterError(ctx, gate, err, now)
	}
	queueTime, runTime := githubCheckQueueAndRunTime(result.ObservedCheckRuns)
	slog.Info("github check gate API result", "gate_id", gate.ID, "status", result.Status,
		"api_duration_ms", requestDuration.Milliseconds(), "observed_checks", len(result.ObservedCheckRuns),
		"queue_time_ms", queueTime.Milliseconds(), "run_time_ms", runTime.Milliseconds(),
		"detection_lag_ms", githubCheckDetectionLag(now, result.ObservedCheckRuns).Milliseconds())
	if result.Status == GitHubCheckGateStatusPending && missingRequiredChecksAreInvalid(gate, result, now, s.githubOptions.MissingCheckGrace) {
		result.Status = GitHubCheckGateStatusFailed
		result.TerminalReason = GitHubCheckTerminalReasonRequiredChecksMissing
		result.Summary = "Required GitHub check names did not match the check runs observed for this commit."
		result.FailureSummary = "Missing required checks: " + strings.Join(result.MissingRequiredChecks, ", ") + ". Observed check runs: " + strings.Join(githubCheckRunNames(result.ObservedCheckRuns), ", ") + "."
	}
	if result.Status == GitHubCheckGateStatusPending {
		pending := GitHubCheckResult{
			Status: GitHubCheckGateStatusPending, Summary: result.Summary, CheckRuns: result.CheckRuns,
			ObservedCheckRuns: result.ObservedCheckRuns, MissingRequiredChecks: result.MissingRequiredChecks,
		}
		updated, _, err := s.store.CompleteGitHubCheckGate(ctx, gate.ID, GitHubCheckGateStatusPending, pending, now)
		return updated, err
	}
	updated, changed, err := s.store.CompleteGitHubCheckGate(ctx, gate.ID, result.Status, result, now)
	if err != nil {
		return nil, err
	}
	if changed && deliverEvidence {
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

func (s *Service) delayGitHubCheckGateAfterError(ctx context.Context, gate *GitHubCheckGate, checkErr error, checkedAt time.Time) (*GitHubCheckGate, error) {
	nextPollAt := checkedAt.Add(time.Duration(gate.PollIntervalSeconds) * time.Second)
	if nextPollAt.After(gate.TimeoutAt) {
		nextPollAt = gate.TimeoutAt
	}
	result := GitHubCheckResult{
		Status:  GitHubCheckGateStatusPending,
		Summary: "GitHub check polling will retry after a request error: " + checkErr.Error(),
	}
	updated, _, err := s.store.DelayGitHubCheckGate(ctx, gate.ID, result, nextPollAt, checkedAt)
	if err != nil {
		return nil, fmt.Errorf("checking github commit %s: %w; recording retry: %v", gate.CommitSHA, checkErr, err)
	}
	return updated, nil
}

func githubCheckDetectionLag(now time.Time, runs []GitHubCheckRun) time.Duration {
	var latest time.Time
	for _, run := range runs {
		if run.CompletedAt != nil && run.CompletedAt.After(latest) {
			latest = *run.CompletedAt
		}
	}
	if latest.IsZero() || latest.After(now) {
		return 0
	}
	return now.Sub(latest)
}

func githubCheckQueueAndRunTime(runs []GitHubCheckRun) (time.Duration, time.Duration) {
	var queueTime, runTime time.Duration
	for _, run := range runs {
		if run.CreatedAt != nil && run.StartedAt != nil && !run.StartedAt.Before(*run.CreatedAt) {
			queueTime = max(queueTime, run.StartedAt.Sub(*run.CreatedAt))
		}
		if run.StartedAt != nil && run.CompletedAt != nil && !run.CompletedAt.Before(*run.StartedAt) {
			runTime = max(runTime, run.CompletedAt.Sub(*run.StartedAt))
		}
	}
	return queueTime, runTime
}

func missingRequiredChecksAreInvalid(gate *GitHubCheckGate, result GitHubCheckResult, now time.Time, grace time.Duration) bool {
	return len(result.MissingRequiredChecks) > 0 && len(result.ObservedCheckRuns) > 0 && result.AllObservedChecksTerminal &&
		!now.Before(gate.CreatedAt.Add(grace))
}

func githubCheckRunNames(runs []GitHubCheckRun) []string {
	names := make([]string, 0, len(runs))
	for _, run := range runs {
		names = append(names, run.Name)
	}
	return names
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

func (s *Service) RetryGitHubCheckGateEvidence(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 10
	}
	gates, err := s.store.ListGitHubCheckGatesPendingEvidence(ctx, limit)
	if err != nil {
		return err
	}
	var evidenceErrors []error
	for _, gate := range gates {
		started := time.Now()
		if err := s.deliverGitHubCheckGateEvidence(ctx, gate); err != nil {
			evidenceErrors = append(evidenceErrors, err)
			slog.Warn("github check gate evidence delivery failed", "gate_id", gate.ID, "error", err)
			continue
		}
		completedAt := gate.UpdatedAt
		if gate.CompletedAt != nil {
			completedAt = *gate.CompletedAt
		}
		slog.Info("github check gate evidence delivered", "gate_id", gate.ID,
			"attempt_duration_ms", time.Since(started).Milliseconds(), "evidence_lag_ms", s.clock().UTC().Sub(completedAt).Milliseconds())
	}
	return errors.Join(evidenceErrors...)
}

func (s *Service) deliverGitHubCheckGateEvidence(ctx context.Context, gate *GitHubCheckGate) error {
	if gate.EvidenceMessageStatus == GitHubCheckEvidenceStatusPosted {
		return nil
	}
	content, intent := renderGitHubCheckGateEvidence(gate)
	message, err := s.messages.AppendTaskMessage(ctx, gate.ProjectID, AppendMessageRequest{
		TaskID:  gate.TaskID,
		Sender:  "den-review",
		Content: content,
		Intent:  intent,
		Metadata: map[string]any{
			"type":                    "github_check_gate",
			"gate_id":                 gate.ID,
			"status":                  gate.Status,
			"repository":              gate.Repository,
			"commit_sha":              gate.CommitSHA,
			"ref":                     gate.Ref,
			"required_checks":         gate.RequiredChecks,
			"observed_check_runs":     gate.ObservedCheckRuns,
			"missing_required_checks": gate.MissingRequiredChecks,
			"terminal_reason":         gate.TerminalReason,
			"agent_profile":           gate.AgentProfile,
			"agent_instance_id":       gate.AgentInstanceID,
			"session_key":             gate.SessionKey,
			"requested_by":            gate.RequestedBy,
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
	if pollInterval < defaultGitHubCheckPollInterval {
		pollInterval = defaultGitHubCheckPollInterval
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
			if existing.ValidationStatus == PacketStatusAccepted && existing.MessageID != nil {
				return existing, nil
			}
			return s.deliverReservedPacket(ctx, existing, threadID)
		}
	}
	packet.ValidationStatus = PacketStatusPendingMessageAppend
	packet.AcceptedAt = nil
	packet.MessageID = nil
	reserved, err := s.store.StorePacket(ctx, packet)
	if err != nil {
		return nil, err
	}
	return s.deliverReservedPacket(ctx, reserved, threadID)
}

func (s *Service) appendPacketMessage(ctx context.Context, packet *ReviewPacket, threadID *int64) (AppendedMessage, error) {
	metadata := cloneStringMap(packet.TypedEnvelope)
	metadata["review_packet_id"] = packet.ID
	return s.messages.AppendTaskMessage(ctx, packet.ProjectID, AppendMessageRequest{
		TaskID: packet.TaskID, ThreadID: threadID, Sender: packet.Sender, Content: packet.SourceMarkdown,
		Intent:   intentForPacket(packet.PacketKind, stringValue(packet.TypedEnvelope["verdict"])),
		Metadata: metadata,
	})
}

func (s *Service) deliverReservedPacket(ctx context.Context, packet *ReviewPacket, threadID *int64) (*ReviewPacket, error) {
	message, err := s.appendPacketMessage(ctx, packet, threadID)
	if err != nil {
		return packet, err
	}
	packet.MessageID = &message.ID
	packet.ValidationStatus = PacketStatusAccepted
	now := s.clock().UTC()
	packet.AcceptedAt = &now
	return s.store.StorePacket(ctx, packet)
}

func validateFinalizationFindings(verdict string, roundFindings []*ReviewFinding, allFindings []*ReviewFinding) error {
	switch verdict {
	case VerdictLooksGood:
		for _, finding := range allFindings {
			if !resolvedStatus(finding.Status) {
				return conflict(ErrUnresolvedFindings, "unresolved_review_findings")
			}
		}
	case VerdictChangesRequested:
		for _, finding := range roundFindings {
			if !resolvedStatus(finding.Status) {
				return nil
			}
		}
		return conflict(ErrActionableFinding, "actionable_review_finding_required")
	}
	return nil
}

func validFinalizationVerdict(verdict string) bool {
	return verdict == VerdictLooksGood || verdict == VerdictChangesRequested
}

func validCompatibilityVerdict(verdict string) bool {
	return verdict == VerdictFollowUpNeeded || verdict == VerdictBlockedByDependency
}

func taskStatusForVerdict(verdict string) string {
	if verdict == VerdictLooksGood {
		return TaskStatusDone
	}
	return TaskStatusInProgress
}

func finalizationPacketKey(roundID int64, verdict string, decidedBy string) string {
	return "review-finalization-packet:" + finalizationKey(roundID, verdict, decidedBy)
}

func finalizationKey(roundID int64, verdict string, decidedBy string) string {
	return fmt.Sprintf("%d:%s:%s", roundID, verdict, decidedBy)
}

func cloneStringMap(input map[string]any) map[string]any {
	cloned := make(map[string]any, len(input)+1)
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
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
		if requiresReviewedHead(packet.PacketKind) && round.TargetKind != ReviewTargetCampaignReconciliation &&
			stringValue(packet.TypedEnvelope["reviewed_head_commit"]) != round.HeadCommit {
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
		TargetKind: ReviewTargetCodeDiff,
		Branch:     strings.TrimSpace(req.Branch), BaseBranch: strings.TrimSpace(req.BaseBranch),
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
