package review

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type memoryStore struct {
	nextRoundID   int64
	nextFindingID int64
	nextPacketID  int64
	rounds        map[int64]*ReviewRound
	findings      map[int64]*ReviewFinding
	packets       map[int64]*ReviewPacket
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		nextRoundID: 1, nextFindingID: 1, nextPacketID: 1,
		rounds: map[int64]*ReviewRound{}, findings: map[int64]*ReviewFinding{}, packets: map[int64]*ReviewPacket{},
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

func replace(value string, old string, next string) string {
	return strings.Replace(value, old, next, 1)
}
