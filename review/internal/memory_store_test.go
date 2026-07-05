package review

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type memoryStore struct {
	nextRoundID           int64
	nextFindingID         int64
	nextPacketID          int64
	nextGitHubCheckGateID int64
	rounds                map[int64]*ReviewRound
	findings              map[int64]*ReviewFinding
	packets               map[int64]*ReviewPacket
	githubCheckGates      map[int64]*GitHubCheckGate
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		nextRoundID: 1, nextFindingID: 1, nextPacketID: 1, nextGitHubCheckGateID: 1,
		rounds: map[int64]*ReviewRound{}, findings: map[int64]*ReviewFinding{}, packets: map[int64]*ReviewPacket{},
		githubCheckGates: map[int64]*GitHubCheckGate{},
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
			existing.EvidenceMessageStatus = GitHubCheckEvidenceStatusPending
			existing.EvidenceMessageError = ""
			existing.UpdatedAt = now
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
				existing.TimeoutAt = gate.TimeoutAt
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
	gate.CheckRuns = result.CheckRuns
	gate.LastCheckedAt = &checkedAt
	if terminalGitHubCheckGateStatus(status) {
		gate.CompletedAt = &checkedAt
		gate.EvidenceMessageStatus = GitHubCheckEvidenceStatusPending
		gate.EvidenceMessageError = ""
	} else {
		gate.NextPollAt = checkedAt.Add(time.Duration(gate.PollIntervalSeconds) * time.Second)
	}
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
	return s.CompleteGitHubCheckGate(ctx, id, GitHubCheckGateStatusTimedOut, GitHubCheckResult{
		Status:  GitHubCheckGateStatusTimedOut,
		Summary: "GitHub check gate timed out before all required checks passed.",
	}, checkedAt)
}

func replace(value string, old string, next string) string {
	return strings.Replace(value, old, next, 1)
}
