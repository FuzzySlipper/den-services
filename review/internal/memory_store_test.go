package review

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type memoryStore struct {
	nextRoundID                int64
	nextFindingID              int64
	nextPacketID               int64
	nextFinalizationID         int64
	nextGitHubCheckGateID      int64
	nextGitHubCheckGateEventID int64
	rounds                     map[int64]*ReviewRound
	findings                   map[int64]*ReviewFinding
	packets                    map[int64]*ReviewPacket
	finalizations              map[int64]*ReviewFinalization
	githubCheckGates           map[int64]*GitHubCheckGate
	githubCheckGateEvents      map[int64]*GitHubCheckGateTerminalEvent
	failCompleteOnce           bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		nextRoundID: 1, nextFindingID: 1, nextPacketID: 1, nextFinalizationID: 1,
		nextGitHubCheckGateID: 1, nextGitHubCheckGateEventID: 1,
		rounds: map[int64]*ReviewRound{}, findings: map[int64]*ReviewFinding{}, packets: map[int64]*ReviewPacket{},
		finalizations:         map[int64]*ReviewFinalization{},
		githubCheckGates:      map[int64]*GitHubCheckGate{},
		githubCheckGateEvents: map[int64]*GitHubCheckGateTerminalEvent{},
	}
}

func (s *memoryStore) Ping(context.Context) error { return nil }

func (s *memoryStore) CreateRound(_ context.Context, round *ReviewRound) (*ReviewRound, error) {
	var latest *ReviewRound
	for _, existing := range s.rounds {
		if existing.ProjectID == round.ProjectID && existing.TaskID == round.TaskID && (latest == nil || existing.RoundNumber > latest.RoundNumber) {
			latest = existing
		}
	}
	if latest != nil {
		if latest.HeadCommit == round.HeadCommit {
			return nil, conflict(fmt.Errorf("head commit was already reviewed: %s", round.HeadCommit), "same_head_review")
		}
		round.RoundNumber = latest.RoundNumber + 1
		if round.LastReviewedHeadCommit == "" {
			round.LastReviewedHeadCommit = latest.HeadCommit
		}
	} else {
		round.RoundNumber = 1
	}
	if round.DeltaBaseCommit == "" {
		round.DeltaBaseCommit = round.LastReviewedHeadCommit
	}
	round.ID = s.nextRoundID
	s.nextRoundID++
	copied := *round
	s.rounds[copied.ID] = &copied
	return &copied, nil
}

func (s *memoryStore) ListRounds(_ context.Context, projectID string, taskID int64) ([]*ReviewRound, error) {
	var rounds []*ReviewRound
	for _, round := range s.rounds {
		if round.ProjectID == projectID && round.TaskID == taskID {
			copied := *round
			rounds = append(rounds, &copied)
		}
	}
	return rounds, nil
}

func (s *memoryStore) GetRound(_ context.Context, id int64) (*ReviewRound, error) {
	round, ok := s.rounds[id]
	if !ok {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingRound, id), "round_not_found")
	}
	copied := *round
	return &copied, nil
}

func (s *memoryStore) SetVerdict(_ context.Context, id int64, verdict string, decidedBy string, notes string, decidedAt time.Time) (*ReviewRound, error) {
	round, ok := s.rounds[id]
	if !ok {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingRound, id), "round_not_found")
	}
	round.Verdict = verdict
	round.VerdictBy = decidedBy
	round.VerdictNotes = notes
	round.VerdictAt = &decidedAt
	round.UpdatedAt = decidedAt
	copied := *round
	return &copied, nil
}

func (s *memoryStore) CreateFinding(_ context.Context, finding *ReviewFinding) (*ReviewFinding, error) {
	nextNumber := 1
	for _, existing := range s.findings {
		if existing.ProjectID == finding.ProjectID && existing.TaskID == finding.TaskID && existing.FindingNumber >= nextNumber {
			nextNumber = existing.FindingNumber + 1
		}
	}
	finding.ID = s.nextFindingID
	s.nextFindingID++
	finding.FindingNumber = nextNumber
	finding.FindingKey = fmt.Sprintf("R%d-%d", finding.TaskID, nextNumber)
	copied := *finding
	s.findings[copied.ID] = &copied
	return &copied, nil
}

func (s *memoryStore) ListFindings(_ context.Context, query ListFindingsQuery) ([]*ReviewFinding, error) {
	var findings []*ReviewFinding
	for _, finding := range s.findings {
		if finding.ProjectID != query.ProjectID || finding.TaskID != query.TaskID {
			continue
		}
		if query.ReviewRoundID != nil && finding.ReviewRoundID != *query.ReviewRoundID {
			continue
		}
		if len(query.Statuses) > 0 && !contains(query.Statuses, finding.Status) {
			continue
		}
		if query.Resolved != nil && resolvedStatus(finding.Status) != *query.Resolved {
			continue
		}
		copied := *finding
		findings = append(findings, &copied)
	}
	return findings, nil
}

func (s *memoryStore) GetFinding(_ context.Context, id int64) (*ReviewFinding, error) {
	finding, ok := s.findings[id]
	if !ok {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingFinding, id), "finding_not_found")
	}
	copied := *finding
	return &copied, nil
}

func (s *memoryStore) RespondToFinding(_ context.Context, id int64, response FindingResponseUpdate, updatedAt time.Time) (*ReviewFinding, error) {
	finding, ok := s.findings[id]
	if !ok {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingFinding, id), "finding_not_found")
	}
	finding.ResponseBy = response.RespondedBy
	finding.ResponseNotes = response.ResponseNotes
	finding.ResponseAt = &updatedAt
	if response.Status != "" {
		finding.Status = response.Status
		finding.StatusUpdatedBy = response.RespondedBy
		finding.StatusNotes = response.StatusNotes
		finding.StatusUpdatedAt = &updatedAt
		finding.FollowUpTaskID = response.FollowUpTaskID
	}
	finding.UpdatedAt = updatedAt
	copied := *finding
	return &copied, nil
}

func (s *memoryStore) SetFindingStatus(_ context.Context, id int64, update FindingStatusUpdate, updatedAt time.Time) (*ReviewFinding, error) {
	finding, ok := s.findings[id]
	if !ok {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingFinding, id), "finding_not_found")
	}
	finding.Status = update.Status
	finding.StatusUpdatedBy = update.UpdatedBy
	finding.StatusNotes = update.Notes
	finding.StatusUpdatedAt = &updatedAt
	if update.Status == StatusSplitToFollowUp {
		finding.FollowUpTaskID = update.FollowUpTaskID
	} else {
		finding.FollowUpTaskID = nil
	}
	finding.UpdatedAt = updatedAt
	copied := *finding
	return &copied, nil
}

func (s *memoryStore) StorePacket(_ context.Context, packet *ReviewPacket) (*ReviewPacket, error) {
	for _, existing := range s.packets {
		if packet.IdempotencyKey != "" && existing.ProjectID == packet.ProjectID && existing.IdempotencyKey == packet.IdempotencyKey {
			existing.MessageID = packet.MessageID
			existing.FrontMatter = packet.FrontMatter
			existing.TypedEnvelope = packet.TypedEnvelope
			existing.MarkdownBody = packet.MarkdownBody
			existing.SourceMarkdown = packet.SourceMarkdown
			existing.ValidationStatus = packet.ValidationStatus
			existing.ValidationErrors = packet.ValidationErrors
			existing.AcceptedAt = packet.AcceptedAt
			copied := *existing
			return &copied, nil
		}
	}
	packet.ID = s.nextPacketID
	s.nextPacketID++
	copied := *packet
	s.packets[copied.ID] = &copied
	return &copied, nil
}

func (s *memoryStore) GetPacketByIdempotency(_ context.Context, projectID string, idempotencyKey string) (*ReviewPacket, error) {
	for _, packet := range s.packets {
		if packet.ProjectID == projectID && packet.IdempotencyKey == idempotencyKey {
			copied := *packet
			return &copied, nil
		}
	}
	return nil, nil
}

func (s *memoryStore) GetPacket(_ context.Context, id int64) (*ReviewPacket, error) {
	packet, ok := s.packets[id]
	if !ok {
		return nil, notFound(fmt.Errorf("review packet not found: %d", id), "packet_not_found")
	}
	copied := *packet
	return &copied, nil
}

func (s *memoryStore) GetReviewFindingsPacketForRound(_ context.Context, roundID int64) (*ReviewPacket, error) {
	var selected *ReviewPacket
	for _, packet := range s.packets {
		if packet.ReviewRoundID == nil || *packet.ReviewRoundID != roundID || packet.PacketKind != PacketKindReviewFindings {
			continue
		}
		if selected == nil || packet.MessageID != nil && selected.MessageID == nil || packet.ID > selected.ID {
			copied := *packet
			selected = &copied
		}
	}
	return selected, nil
}

func (s *memoryStore) GetFinalizationByRound(_ context.Context, roundID int64) (*ReviewFinalization, error) {
	for _, finalization := range s.finalizations {
		if finalization.ReviewRoundID == roundID {
			copied := *finalization
			return &copied, nil
		}
	}
	return nil, nil
}

func (s *memoryStore) BeginFinalization(
	ctx context.Context,
	finalization *ReviewFinalization,
	packet *ReviewPacket,
	decidedAt time.Time,
) (*ReviewFinalization, *ReviewPacket, *ReviewRound, error) {
	existing, err := s.GetFinalizationByRound(ctx, finalization.ReviewRoundID)
	if err != nil {
		return nil, nil, nil, err
	}
	if existing != nil {
		storedPacket, packetErr := s.GetPacket(ctx, existing.PacketID)
		round, roundErr := s.GetRound(ctx, existing.ReviewRoundID)
		return existing, storedPacket, round, errors.Join(packetErr, roundErr)
	}
	round, err := s.GetRound(ctx, finalization.ReviewRoundID)
	if err != nil {
		return nil, nil, nil, err
	}
	if round.Verdict != "" && (round.Verdict != finalization.Verdict ||
		round.VerdictBy != "" && round.VerdictBy != finalization.DecidedBy) {
		return nil, nil, nil, conflict(ErrFinalizationConflict, "review_finalization_conflict")
	}
	storedPacket, err := s.GetReviewFindingsPacketForRound(ctx, finalization.ReviewRoundID)
	if err != nil {
		return nil, nil, nil, err
	}
	if storedPacket == nil {
		packet.ValidationStatus = PacketStatusPendingMessageAppend
		storedPacket, err = s.StorePacket(ctx, packet)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	round, err = s.SetVerdict(ctx, round.ID, finalization.Verdict, finalization.DecidedBy, finalization.Notes, decidedAt)
	if err != nil {
		return nil, nil, nil, err
	}
	finalization.ID = s.nextFinalizationID
	s.nextFinalizationID++
	finalization.PacketID = storedPacket.ID
	finalization.PacketIdempotencyKey = storedPacket.IdempotencyKey
	if storedPacket.MessageID != nil {
		finalization.MessageID = storedPacket.MessageID
		finalization.PacketPostedAt = storedPacket.AcceptedAt
		if finalization.PacketPostedAt == nil {
			finalization.PacketPostedAt = &decidedAt
		}
		finalization.State = FinalizationStatePacketPosted
	}
	copied := *finalization
	s.finalizations[copied.ID] = &copied
	return &copied, storedPacket, round, nil
}

func (s *memoryStore) MarkFinalizationPacketPosted(
	_ context.Context,
	id int64,
	messageID int64,
	postedAt time.Time,
) (*ReviewFinalization, *ReviewPacket, error) {
	finalization, ok := s.finalizations[id]
	if !ok {
		return nil, nil, notFound(fmt.Errorf("review finalization not found: %d", id), "review_finalization_not_found")
	}
	packet := s.packets[finalization.PacketID]
	packet.MessageID = &messageID
	packet.ValidationStatus = PacketStatusAccepted
	if packet.AcceptedAt == nil {
		packet.AcceptedAt = &postedAt
	}
	if finalization.MessageID == nil {
		finalization.MessageID = &messageID
	}
	if finalization.PacketPostedAt == nil {
		finalization.PacketPostedAt = &postedAt
	}
	if finalization.State != FinalizationStateComplete {
		finalization.State = FinalizationStatePacketPosted
	}
	finalization.LastError = ""
	finalization.LastErrorStep = ""
	finalization.MessageAttempts++
	finalization.UpdatedAt = postedAt
	finalizationCopy := *finalization
	packetCopy := *packet
	return &finalizationCopy, &packetCopy, nil
}

func (s *memoryStore) MarkFinalizationTaskTransitioned(_ context.Context, id int64, transitionedAt time.Time) (*ReviewFinalization, error) {
	finalization, ok := s.finalizations[id]
	if !ok {
		return nil, notFound(fmt.Errorf("review finalization not found: %d", id), "review_finalization_not_found")
	}
	if finalization.TaskTransitionedAt == nil {
		finalization.TaskTransitionedAt = &transitionedAt
	}
	if finalization.State != FinalizationStateComplete {
		finalization.State = FinalizationStateTaskTransitioned
	}
	finalization.LastError = ""
	finalization.LastErrorStep = ""
	finalization.TaskTransitionAttempts++
	finalization.UpdatedAt = transitionedAt
	copied := *finalization
	return &copied, nil
}

func (s *memoryStore) CompleteFinalization(_ context.Context, id int64, completedAt time.Time) (*ReviewFinalization, error) {
	if s.failCompleteOnce {
		s.failCompleteOnce = false
		return nil, errors.New("completion checkpoint failed")
	}
	finalization, ok := s.finalizations[id]
	if !ok {
		return nil, notFound(fmt.Errorf("review finalization not found: %d", id), "review_finalization_not_found")
	}
	if finalization.PacketPostedAt == nil || finalization.TaskTransitionedAt == nil {
		return nil, conflict(errors.New("review finalization is missing a delivery checkpoint"), "review_finalization_incomplete")
	}
	finalization.State = FinalizationStateComplete
	if finalization.CompletedAt == nil {
		finalization.CompletedAt = &completedAt
	}
	finalization.LastError = ""
	finalization.LastErrorStep = ""
	finalization.UpdatedAt = completedAt
	copied := *finalization
	return &copied, nil
}

func (s *memoryStore) RecordFinalizationError(
	_ context.Context,
	id int64,
	step string,
	message string,
	attemptedAt time.Time,
) (*ReviewFinalization, error) {
	finalization, ok := s.finalizations[id]
	if !ok {
		return nil, notFound(fmt.Errorf("review finalization not found: %d", id), "review_finalization_not_found")
	}
	if finalization.State != FinalizationStateComplete {
		finalization.State = FinalizationStateRetryableError
	}
	finalization.LastErrorStep = step
	finalization.LastError = message
	if step == FinalizationStepPacketDelivery {
		finalization.MessageAttempts++
	}
	if step == FinalizationStepTaskTransition {
		finalization.TaskTransitionAttempts++
	}
	finalization.UpdatedAt = attemptedAt
	copied := *finalization
	return &copied, nil
}

func (s *memoryStore) WorkflowSummary(ctx context.Context, projectID string, taskID int64) (WorkflowSummary, error) {
	rounds, err := s.ListRounds(ctx, projectID, taskID)
	if err != nil {
		return WorkflowSummary{}, err
	}
	findings, err := s.ListFindings(ctx, ListFindingsQuery{ProjectID: projectID, TaskID: taskID})
	if err != nil {
		return WorkflowSummary{}, err
	}
	return buildWorkflowSummary(rounds, findings), nil
}

func (s *memoryStore) RegisterGitHubCheckGate(_ context.Context, gate *GitHubCheckGate, now time.Time) (*GitHubCheckGate, []*GitHubCheckGate, error) {
	var superseded []*GitHubCheckGate
	for _, existing := range s.githubCheckGates {
		if existing.ProjectID == gate.ProjectID && existing.TaskID == gate.TaskID && existing.CommitSHA != gate.CommitSHA && existing.Status == GitHubCheckGateStatusPending {
			existing.Status = GitHubCheckGateStatusSuperseded
			existing.CompletedAt = &now
			existing.Summary = "Superseded by newer commit " + gate.CommitSHA
			existing.TerminalReason = GitHubCheckTerminalReasonSuperseded
			existing.EvidenceMessageStatus = GitHubCheckEvidenceStatusPending
			existing.EvidenceMessageError = ""
			existing.UpdatedAt = now
			s.recordGitHubCheckGateEvent(existing, now)
			copied := *existing
			superseded = append(superseded, &copied)
		}
	}
	for _, existing := range s.githubCheckGates {
		if existing.ProjectID == gate.ProjectID && existing.TaskID == gate.TaskID && existing.CommitSHA == gate.CommitSHA {
			if existing.Status == GitHubCheckGateStatusPending {
				existing.Repository = gate.Repository
				existing.Ref = gate.Ref
				existing.RequiredChecks = gate.RequiredChecks
				existing.PollIntervalSeconds = gate.PollIntervalSeconds
				existing.NextPollAt = gate.NextPollAt
			}
			existing.RequestedBy = gate.RequestedBy
			existing.AgentProfile = gate.AgentProfile
			existing.AgentInstanceID = gate.AgentInstanceID
			existing.SessionKey = gate.SessionKey
			existing.StatusURL = gate.StatusURL
			existing.UpdatedAt = now
			copied := *existing
			return &copied, superseded, nil
		}
	}
	gate.ID = s.nextGitHubCheckGateID
	s.nextGitHubCheckGateID++
	gate.EvidenceMessageStatus = GitHubCheckEvidenceStatusNotRequired
	copied := *gate
	s.githubCheckGates[copied.ID] = &copied
	return &copied, superseded, nil
}

func (s *memoryStore) GetGitHubCheckGate(_ context.Context, projectID string, taskID int64, commitSHA string) (*GitHubCheckGate, error) {
	for _, gate := range s.githubCheckGates {
		if gate.ProjectID == projectID && gate.TaskID == taskID && gate.CommitSHA == commitSHA {
			copied := *gate
			return &copied, nil
		}
	}
	return nil, notFound(fmt.Errorf("github check gate not found for task %d commit %s", taskID, commitSHA), "github_check_gate_not_found")
}

func (s *memoryStore) ListGitHubCheckGateEvents(_ context.Context, query ListGitHubCheckGateEventsQuery) ([]*GitHubCheckGateTerminalEvent, error) {
	ids := make([]int, 0, len(s.githubCheckGateEvents))
	for id := range s.githubCheckGateEvents {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	events := make([]*GitHubCheckGateTerminalEvent, 0)
	for _, rawID := range ids {
		event := s.githubCheckGateEvents[int64(rawID)]
		if event.ProjectID != query.ProjectID || event.ID <= query.AfterID || (query.TaskID != 0 && event.TaskID != query.TaskID) {
			continue
		}
		copied := *event
		events = append(events, &copied)
		if len(events) == query.Limit {
			break
		}
	}
	return events, nil
}

func (s *memoryStore) ListPendingGitHubCheckGates(_ context.Context, now time.Time, limit int) ([]*GitHubCheckGate, error) {
	var gates []*GitHubCheckGate
	for _, gate := range s.githubCheckGates {
		if gate.Status == GitHubCheckGateStatusPending && !gate.NextPollAt.After(now) {
			copied := *gate
			gates = append(gates, &copied)
			if len(gates) == limit {
				break
			}
		}
	}
	return gates, nil
}

func (s *memoryStore) ListGitHubCheckGatesPendingEvidence(_ context.Context, limit int) ([]*GitHubCheckGate, error) {
	var gates []*GitHubCheckGate
	for _, gate := range s.githubCheckGates {
		if terminalGitHubCheckGateStatus(gate.Status) &&
			(gate.EvidenceMessageStatus == GitHubCheckEvidenceStatusPending || gate.EvidenceMessageStatus == GitHubCheckEvidenceStatusError) {
			copied := *gate
			gates = append(gates, &copied)
			if len(gates) == limit {
				break
			}
		}
	}
	return gates, nil
}

func (s *memoryStore) CompleteGitHubCheckGate(_ context.Context, id int64, status string, result GitHubCheckResult, checkedAt time.Time) (*GitHubCheckGate, bool, error) {
	gate, ok := s.githubCheckGates[id]
	if !ok {
		return nil, false, notFound(fmt.Errorf("github check gate not found: %d", id), "github_check_gate_not_found")
	}
	if gate.Status != GitHubCheckGateStatusPending {
		copied := *gate
		return &copied, false, nil
	}
	gate.Status = status
	gate.Summary = result.Summary
	gate.FailureSummary = result.FailureSummary
	gate.CheckRuns = normalizedGitHubCheckRuns(result.CheckRuns)
	gate.ObservedCheckRuns = result.ObservedCheckRuns
	gate.MissingRequiredChecks = result.MissingRequiredChecks
	gate.TerminalReason = result.TerminalReason
	gate.LastCheckedAt = &checkedAt
	if terminalGitHubCheckGateStatus(status) {
		gate.CompletedAt = &checkedAt
		gate.EvidenceMessageStatus = GitHubCheckEvidenceStatusPending
		gate.EvidenceMessageError = ""
		s.recordGitHubCheckGateEvent(gate, checkedAt)
	} else {
		gate.NextPollAt = checkedAt.Add(time.Duration(gate.PollIntervalSeconds) * time.Second)
	}
	gate.UpdatedAt = checkedAt
	copied := *gate
	return &copied, true, nil
}

func (s *memoryStore) recordGitHubCheckGateEvent(gate *GitHubCheckGate, createdAt time.Time) {
	for _, event := range s.githubCheckGateEvents {
		if event.GateID == gate.ID {
			return
		}
	}
	completedAt := createdAt
	if gate.CompletedAt != nil {
		completedAt = *gate.CompletedAt
	}
	event := &GitHubCheckGateTerminalEvent{
		ID: s.nextGitHubCheckGateEventID, Schema: GitHubCheckGateTerminalEventSchema,
		SchemaVersion: GitHubCheckGateTerminalEventSchemaVersion, GateID: gate.ID,
		ProjectID: gate.ProjectID, TaskID: gate.TaskID, Repository: gate.Repository, CommitSHA: gate.CommitSHA,
		Ref: gate.Ref, Status: gate.Status, TerminalReason: gate.TerminalReason, RequiredChecks: gate.RequiredChecks,
		CheckRuns: normalizedGitHubCheckRuns(gate.CheckRuns), ObservedCheckRuns: gate.ObservedCheckRuns, MissingRequiredChecks: gate.MissingRequiredChecks,
		Summary: gate.Summary, FailureSummary: gate.FailureSummary, RequestedBy: gate.RequestedBy,
		AgentProfile: gate.AgentProfile, AgentInstanceID: gate.AgentInstanceID, SessionKey: gate.SessionKey,
		GateCreatedAt: gate.CreatedAt, CompletedAt: completedAt, CreatedAt: createdAt,
	}
	s.nextGitHubCheckGateEventID++
	s.githubCheckGateEvents[event.ID] = event
}

func normalizedGitHubCheckRuns(runs []GitHubCheckRun) []GitHubCheckRun {
	if runs == nil {
		return []GitHubCheckRun{}
	}
	return runs
}

func (s *memoryStore) DelayGitHubCheckGate(_ context.Context, id int64, result GitHubCheckResult, nextPollAt time.Time, checkedAt time.Time) (*GitHubCheckGate, bool, error) {
	gate, ok := s.githubCheckGates[id]
	if !ok {
		return nil, false, notFound(fmt.Errorf("github check gate not found: %d", id), "github_check_gate_not_found")
	}
	if gate.Status != GitHubCheckGateStatusPending {
		copied := *gate
		return &copied, false, nil
	}
	gate.Summary = result.Summary
	gate.FailureSummary = result.FailureSummary
	gate.CheckRuns = normalizedGitHubCheckRuns(result.CheckRuns)
	gate.ObservedCheckRuns = result.ObservedCheckRuns
	gate.MissingRequiredChecks = result.MissingRequiredChecks
	gate.TerminalReason = result.TerminalReason
	gate.LastCheckedAt = &checkedAt
	gate.NextPollAt = nextPollAt
	gate.UpdatedAt = checkedAt
	copied := *gate
	return &copied, true, nil
}

func (s *memoryStore) MarkGitHubCheckGateEvidencePosted(_ context.Context, id int64, messageID int64, at time.Time) (*GitHubCheckGate, error) {
	gate, ok := s.githubCheckGates[id]
	if !ok {
		return nil, notFound(fmt.Errorf("github check gate not found: %d", id), "github_check_gate_not_found")
	}
	gate.EvidenceMessageStatus = GitHubCheckEvidenceStatusPosted
	gate.EvidenceMessageID = &messageID
	gate.EvidenceMessageError = ""
	gate.EvidenceMessageAttemptedAt = &at
	gate.UpdatedAt = at
	copied := *gate
	return &copied, nil
}

func (s *memoryStore) RecordGitHubCheckGateEvidenceError(_ context.Context, id int64, messageError string, at time.Time) (*GitHubCheckGate, error) {
	gate, ok := s.githubCheckGates[id]
	if !ok {
		return nil, notFound(fmt.Errorf("github check gate not found: %d", id), "github_check_gate_not_found")
	}
	gate.EvidenceMessageStatus = GitHubCheckEvidenceStatusError
	gate.EvidenceMessageError = messageError
	gate.EvidenceMessageAttemptedAt = &at
	gate.UpdatedAt = at
	copied := *gate
	return &copied, nil
}

func (s *memoryStore) TimeoutGitHubCheckGate(ctx context.Context, id int64, checkedAt time.Time) (*GitHubCheckGate, bool, error) {
	gate, ok := s.githubCheckGates[id]
	if !ok {
		return nil, false, notFound(fmt.Errorf("github check gate not found: %d", id), "github_check_gate_not_found")
	}
	return s.CompleteGitHubCheckGate(ctx, id, GitHubCheckGateStatusTimedOut, GitHubCheckResult{
		Status: GitHubCheckGateStatusTimedOut, Summary: "GitHub check gate timed out before all required checks passed.",
		TerminalReason: GitHubCheckTerminalReasonTimedOut, CheckRuns: gate.CheckRuns,
		ObservedCheckRuns: gate.ObservedCheckRuns, MissingRequiredChecks: gate.MissingRequiredChecks,
	}, checkedAt)
}

func replace(value string, old string, next string) string {
	return strings.Replace(value, old, next, 1)
}
