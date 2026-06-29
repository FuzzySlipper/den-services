package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging review store: %w", err)
	}
	return nil
}

func (s *Store) CreateRound(ctx context.Context, round *ReviewRound) (*ReviewRound, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning create review round: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var previousHead string
	var roundNumber int
	err = tx.QueryRow(ctx, latestRoundSQL, round.ProjectID, round.TaskID).Scan(&roundNumber, &previousHead)
	if errors.Is(err, pgx.ErrNoRows) {
		roundNumber = 1
	} else if err != nil {
		return nil, fmt.Errorf("loading latest review round: %w", err)
	} else {
		roundNumber++
	}
	if round.LastReviewedHeadCommit == "" {
		round.LastReviewedHeadCommit = previousHead
	}
	if round.DeltaBaseCommit == "" {
		round.DeltaBaseCommit = round.LastReviewedHeadCommit
	}
	if previousHead != "" && previousHead == round.HeadCommit {
		return nil, conflict(fmt.Errorf("head commit was already reviewed: %s", round.HeadCommit), "same_head_review")
	}
	round.RoundNumber = roundNumber
	created, err := scanRound(tx.QueryRow(ctx, createRoundSQL, round.ProjectID, round.TaskID, round.RoundNumber, round.RequestedBy,
		round.Branch, round.BaseBranch, round.BaseCommit, round.HeadCommit, emptyToNil(round.LastReviewedHeadCommit), round.CommitsSinceLastReview,
		jsonOrNil(round.TestsRun), emptyToNil(round.Notes), emptyToNil(round.PreferredDiffBaseRef), emptyToNil(round.PreferredDiffBaseCommit),
		emptyToNil(round.PreferredDiffHeadRef), emptyToNil(round.PreferredDiffHeadCommit), emptyToNil(round.AlternateDiffBaseRef),
		emptyToNil(round.AlternateDiffBaseCommit), emptyToNil(round.AlternateDiffHeadRef), emptyToNil(round.AlternateDiffHeadCommit),
		emptyToNil(round.DeltaBaseCommit), round.InheritedCommitCount, round.TaskLocalCommitCount, round.RequestedAt, round.CreatedAt, round.UpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("creating review round: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing review round: %w", err)
	}
	return created, nil
}

func (s *Store) ListRounds(ctx context.Context, projectID string, taskID int64) ([]*ReviewRound, error) {
	rows, err := s.pool.Query(ctx, listRoundsSQL, projectID, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing review rounds: %w", err)
	}
	defer rows.Close()
	return scanRounds(rows)
}

func (s *Store) GetRound(ctx context.Context, id int64) (*ReviewRound, error) {
	round, err := scanRound(s.pool.QueryRow(ctx, getRoundSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingRound, id), "round_not_found")
	}
	if err != nil {
		return nil, fmt.Errorf("getting review round %d: %w", id, err)
	}
	return round, nil
}

func (s *Store) SetVerdict(ctx context.Context, id int64, verdict string, decidedBy string, notes string, decidedAt time.Time) (*ReviewRound, error) {
	round, err := scanRound(s.pool.QueryRow(ctx, setVerdictSQL, id, verdict, decidedBy, emptyToNil(notes), decidedAt, decidedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingRound, id), "round_not_found")
	}
	if err != nil {
		return nil, fmt.Errorf("setting review verdict: %w", err)
	}
	return round, nil
}

func (s *Store) CreateFinding(ctx context.Context, finding *ReviewFinding) (*ReviewFinding, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning create finding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var findingNumber int
	if err := tx.QueryRow(ctx, nextFindingNumberSQL, finding.ProjectID, finding.TaskID).Scan(&findingNumber); err != nil {
		return nil, fmt.Errorf("getting next finding number: %w", err)
	}
	finding.FindingNumber = findingNumber
	finding.FindingKey = fmt.Sprintf("R%d-%d", finding.TaskID, findingNumber)
	created, err := scanFinding(tx.QueryRow(ctx, createFindingSQL, finding.ProjectID, finding.FindingKey, finding.TaskID, finding.ReviewRoundID,
		finding.FindingNumber, finding.CreatedBy, finding.Category, finding.Summary, emptyToNil(finding.Notes),
		jsonOrNil(finding.FileReferences), jsonOrNil(finding.TestCommands), finding.Status, emptyToNil(finding.RunID),
		emptyToNil(finding.SubagentRole), finding.CreatedAt, finding.UpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("creating review finding: %w", err)
	}
	if err := writeFindingEvent(ctx, tx, created, "finding_created", created.CreatedBy, "", created.Status, "", "", nil, created.RunID, created.SubagentRole, created.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing review finding: %w", err)
	}
	return created, nil
}

func (s *Store) ListFindings(ctx context.Context, query ListFindingsQuery) ([]*ReviewFinding, error) {
	rows, err := s.pool.Query(ctx, listFindingsSQL, query.ProjectID, query.TaskID, query.ReviewRoundID, query.Statuses, query.Resolved)
	if err != nil {
		return nil, fmt.Errorf("listing review findings: %w", err)
	}
	defer rows.Close()
	return scanFindings(rows)
}

func (s *Store) GetFinding(ctx context.Context, id int64) (*ReviewFinding, error) {
	finding, err := scanFinding(s.pool.QueryRow(ctx, getFindingSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingFinding, id), "finding_not_found")
	}
	if err != nil {
		return nil, fmt.Errorf("getting review finding %d: %w", id, err)
	}
	return finding, nil
}

func (s *Store) RespondToFinding(ctx context.Context, id int64, response FindingResponseUpdate, updatedAt time.Time) (*ReviewFinding, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning finding response: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := getFindingTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	updated, err := scanFinding(tx.QueryRow(ctx, respondFindingSQL, id, response.RespondedBy, emptyToNil(response.ResponseNotes),
		emptyToNil(response.Status), emptyToNil(response.StatusNotes), response.FollowUpTaskID, emptyToNil(response.RunID), emptyToNil(response.SubagentRole), updatedAt))
	if err != nil {
		return nil, fmt.Errorf("responding to review finding: %w", err)
	}
	if err := writeFindingEvent(ctx, tx, updated, "implementer_response", response.RespondedBy, current.Status, updated.Status, response.StatusNotes, response.ResponseNotes, response.FollowUpTaskID, response.RunID, response.SubagentRole, updatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing finding response: %w", err)
	}
	return updated, nil
}

func (s *Store) SetFindingStatus(ctx context.Context, id int64, update FindingStatusUpdate, updatedAt time.Time) (*ReviewFinding, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning finding status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := getFindingTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	updated, err := scanFinding(tx.QueryRow(ctx, setFindingStatusSQL, id, update.Status, update.UpdatedBy, emptyToNil(update.Notes), update.FollowUpTaskID, emptyToNil(update.RunID), emptyToNil(update.SubagentRole), updatedAt))
	if err != nil {
		return nil, fmt.Errorf("setting review finding status: %w", err)
	}
	if err := writeFindingEvent(ctx, tx, updated, "status_changed", update.UpdatedBy, current.Status, updated.Status, update.Notes, "", update.FollowUpTaskID, update.RunID, update.SubagentRole, updatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing finding status: %w", err)
	}
	return updated, nil
}

func (s *Store) StorePacket(ctx context.Context, packet *ReviewPacket) (*ReviewPacket, error) {
	stored, err := scanPacket(s.pool.QueryRow(ctx, storePacketSQL, packet.ProjectID, packet.TaskID, packet.ReviewRoundID, packet.PacketKind,
		packet.Sender, packet.MessageID, jsonOrNil(packet.FrontMatter), jsonOrNil(packet.TypedEnvelope), packet.MarkdownBody,
		packet.SourceMarkdown, packet.ValidationStatus, jsonOrNil(packet.ValidationErrors), emptyToNil(packet.IdempotencyKey), packet.CreatedAt, packet.AcceptedAt))
	if err != nil {
		return nil, fmt.Errorf("storing review packet: %w", err)
	}
	return stored, nil
}

func (s *Store) GetPacket(ctx context.Context, id int64) (*ReviewPacket, error) {
	packet, err := scanPacket(s.pool.QueryRow(ctx, getPacketSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(fmt.Errorf("review packet not found: %d", id), "packet_not_found")
	}
	if err != nil {
		return nil, fmt.Errorf("getting review packet %d: %w", id, err)
	}
	return packet, nil
}

func (s *Store) GetPacketByIdempotency(ctx context.Context, projectID string, idempotencyKey string) (*ReviewPacket, error) {
	if idempotencyKey == "" {
		return nil, nil
	}
	packet, err := scanPacket(s.pool.QueryRow(ctx, getPacketByIdempotencySQL, projectID, idempotencyKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting review packet by idempotency key: %w", err)
	}
	return packet, nil
}

func (s *Store) WorkflowSummary(ctx context.Context, projectID string, taskID int64) (WorkflowSummary, error) {
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

func writeFindingEvent(ctx context.Context, tx pgx.Tx, finding *ReviewFinding, kind string, actor string, oldStatus string, newStatus string, notes string, responseNotes string, followUpTaskID *int64, runID string, subagentRole string, createdAt time.Time) error {
	_, err := tx.Exec(ctx, insertFindingEventSQL, finding.ProjectID, finding.TaskID, finding.ReviewRoundID, finding.ID, kind, actor, emptyToNil(oldStatus), emptyToNil(newStatus), emptyToNil(notes), emptyToNil(responseNotes), followUpTaskID, emptyToNil(runID), emptyToNil(subagentRole), createdAt)
	if err != nil {
		return fmt.Errorf("writing finding event: %w", err)
	}
	return nil
}

func getFindingTx(ctx context.Context, tx pgx.Tx, id int64) (*ReviewFinding, error) {
	finding, err := scanFinding(tx.QueryRow(ctx, getFindingSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(fmt.Errorf("%w: %d", ErrMissingFinding, id), "finding_not_found")
	}
	return finding, err
}

func buildWorkflowSummary(rounds []*ReviewRound, findings []*ReviewFinding) WorkflowSummary {
	var summary WorkflowSummary
	summary.ReviewRoundCount = len(rounds)
	if len(rounds) > 0 {
		summary.CurrentRound = rounds[len(rounds)-1]
		summary.CurrentVerdict = summary.CurrentRound.Verdict
	}
	byRound := map[int64][]*ReviewFinding{}
	for _, finding := range findings {
		byRound[finding.ReviewRoundID] = append(byRound[finding.ReviewRoundID], finding)
		if resolvedStatus(finding.Status) {
			summary.ResolvedFindingCount++
			summary.ResolvedFindings = append(summary.ResolvedFindings, finding)
		} else {
			summary.UnresolvedFindingCount++
			summary.OpenFindings = append(summary.OpenFindings, finding)
		}
		if finding.Status == StatusClaimedFixed || resolvedStatus(finding.Status) {
			summary.AddressedFindingCount++
		}
	}
	for _, round := range rounds {
		entry := ReviewTimelineEntry{ReviewRoundID: round.ID, RoundNumber: round.RoundNumber, Branch: round.Branch, RequestedBy: round.RequestedBy, RequestedAt: round.RequestedAt, HeadCommit: round.HeadCommit, LastReviewedHeadCommit: round.LastReviewedHeadCommit, CommitsSinceLastReview: round.CommitsSinceLastReview, Verdict: round.Verdict, VerdictBy: round.VerdictBy, VerdictAt: round.VerdictAt}
		for _, finding := range byRound[round.ID] {
			entry.TotalFindings++
			if resolvedStatus(finding.Status) {
				entry.ResolvedFindings++
				entry.AddressedFindings++
			} else {
				entry.OpenFindings++
			}
			if finding.Status == StatusClaimedFixed {
				entry.ClaimedFixedFindings++
				entry.AddressedFindings++
			}
		}
		summary.Timeline = append(summary.Timeline, entry)
	}
	return summary
}

func scanRounds(rows pgx.Rows) ([]*ReviewRound, error) {
	var rounds []*ReviewRound
	for rows.Next() {
		round, err := scanRound(rows)
		if err != nil {
			return nil, err
		}
		rounds = append(rounds, round)
	}
	return rounds, rows.Err()
}

func scanFindings(rows pgx.Rows) ([]*ReviewFinding, error) {
	var findings []*ReviewFinding
	for rows.Next() {
		finding, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRound(row rowScanner) (*ReviewRound, error) {
	var round ReviewRound
	var tests []byte
	err := row.Scan(&round.ID, &round.ProjectID, &round.TaskID, &round.RoundNumber, &round.RequestedBy, &round.Branch, &round.BaseBranch,
		&round.BaseCommit, &round.HeadCommit, &round.LastReviewedHeadCommit, &round.CommitsSinceLastReview, &tests, &round.Notes,
		&round.PreferredDiffBaseRef, &round.PreferredDiffBaseCommit, &round.PreferredDiffHeadRef, &round.PreferredDiffHeadCommit,
		&round.AlternateDiffBaseRef, &round.AlternateDiffBaseCommit, &round.AlternateDiffHeadRef, &round.AlternateDiffHeadCommit,
		&round.DeltaBaseCommit, &round.InheritedCommitCount, &round.TaskLocalCommitCount, &round.Verdict, &round.VerdictBy,
		&round.VerdictNotes, &round.RequestedAt, &round.VerdictAt, &round.CreatedAt, &round.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(tests, &round.TestsRun)
	return &round, nil
}

func scanFinding(row rowScanner) (*ReviewFinding, error) {
	var finding ReviewFinding
	var fileRefs []byte
	var testCommands []byte
	err := row.Scan(&finding.ID, &finding.ProjectID, &finding.FindingKey, &finding.TaskID, &finding.ReviewRoundID, &finding.RoundNumber,
		&finding.FindingNumber, &finding.CreatedBy, &finding.Category, &finding.Summary, &finding.Notes, &fileRefs, &testCommands,
		&finding.Status, &finding.StatusUpdatedBy, &finding.StatusNotes, &finding.StatusUpdatedAt, &finding.ResponseBy,
		&finding.ResponseNotes, &finding.ResponseAt, &finding.FollowUpTaskID, &finding.RunID, &finding.SubagentRole,
		&finding.CreatedAt, &finding.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(fileRefs, &finding.FileReferences)
	_ = json.Unmarshal(testCommands, &finding.TestCommands)
	return &finding, nil
}

func scanPacket(row rowScanner) (*ReviewPacket, error) {
	var packet ReviewPacket
	var frontMatter []byte
	var typedEnvelope []byte
	var validationErrors []byte
	err := row.Scan(&packet.ID, &packet.ProjectID, &packet.TaskID, &packet.ReviewRoundID, &packet.PacketKind, &packet.Sender, &packet.MessageID,
		&frontMatter, &typedEnvelope, &packet.MarkdownBody, &packet.SourceMarkdown, &packet.ValidationStatus, &validationErrors,
		&packet.IdempotencyKey, &packet.CreatedAt, &packet.AcceptedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(frontMatter, &packet.FrontMatter)
	_ = json.Unmarshal(typedEnvelope, &packet.TypedEnvelope)
	_ = json.Unmarshal(validationErrors, &packet.ValidationErrors)
	return &packet, nil
}

func jsonOrNil(value any) any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	if string(data) == "null" || string(data) == "[]" {
		return nil
	}
	return data
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

const roundColumns = `id, project_id, task_id, round_number, requested_by, branch, base_branch, base_commit, head_commit, coalesce(last_reviewed_head_commit, ''), commits_since_last_review, coalesce(tests_run, '[]'::jsonb), coalesce(notes, ''), coalesce(preferred_diff_base_ref, ''), coalesce(preferred_diff_base_commit, ''), coalesce(preferred_diff_head_ref, ''), coalesce(preferred_diff_head_commit, ''), coalesce(alternate_diff_base_ref, ''), coalesce(alternate_diff_base_commit, ''), coalesce(alternate_diff_head_ref, ''), coalesce(alternate_diff_head_commit, ''), coalesce(delta_base_commit, ''), inherited_commit_count, task_local_commit_count, coalesce(verdict, ''), coalesce(verdict_by, ''), coalesce(verdict_notes, ''), requested_at, verdict_at, created_at, updated_at`
const findingColumns = `f.id, f.project_id, f.finding_key, f.task_id, f.review_round_id, r.round_number, f.finding_number, f.created_by, f.category, f.summary, coalesce(f.notes, ''), coalesce(f.file_references, '[]'::jsonb), coalesce(f.test_commands, '[]'::jsonb), f.status, coalesce(f.status_updated_by, ''), coalesce(f.status_notes, ''), f.status_updated_at, coalesce(f.response_by, ''), coalesce(f.response_notes, ''), f.response_at, f.follow_up_task_id, coalesce(f.run_id, ''), coalesce(f.subagent_role, ''), f.created_at, f.updated_at`
const packetColumns = `id, project_id, task_id, review_round_id, packet_kind, sender, message_id, front_matter, typed_envelope, markdown_body, source_markdown, validation_status, coalesce(validation_errors, '[]'::jsonb), coalesce(idempotency_key, ''), created_at, accepted_at`

const latestRoundSQL = `select round_number, head_commit from den_review.review_rounds where project_id = $1 and task_id = $2 order by round_number desc limit 1`
const createRoundSQL = `insert into den_review.review_rounds(project_id, task_id, round_number, requested_by, branch, base_branch, base_commit, head_commit, last_reviewed_head_commit, commits_since_last_review, tests_run, notes, preferred_diff_base_ref, preferred_diff_base_commit, preferred_diff_head_ref, preferred_diff_head_commit, alternate_diff_base_ref, alternate_diff_base_commit, alternate_diff_head_ref, alternate_diff_head_commit, delta_base_commit, inherited_commit_count, task_local_commit_count, requested_at, created_at, updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26) returning ` + roundColumns
const listRoundsSQL = `select ` + roundColumns + ` from den_review.review_rounds where project_id = $1 and task_id = $2 order by round_number asc`
const getRoundSQL = `select ` + roundColumns + ` from den_review.review_rounds where id = $1`
const setVerdictSQL = `update den_review.review_rounds set verdict = $2, verdict_by = $3, verdict_notes = $4, verdict_at = $5, updated_at = $6 where id = $1 returning ` + roundColumns
const nextFindingNumberSQL = `select coalesce(max(finding_number), 0) + 1 from den_review.review_findings where project_id = $1 and task_id = $2`
const createFindingSQL = `insert into den_review.review_findings(project_id, finding_key, task_id, review_round_id, finding_number, created_by, category, summary, notes, file_references, test_commands, status, run_id, subagent_role, created_at, updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) returning ` + findingColumns
const listFindingsSQL = `select ` + findingColumns + ` from den_review.review_findings f join den_review.review_rounds r on r.id = f.review_round_id where f.project_id = $1 and f.task_id = $2 and ($3::bigint is null or f.review_round_id = $3) and (coalesce(cardinality($4::text[]), 0) = 0 or f.status = any($4)) and ($5::bool is null or ($5 = true and f.status in ('verified_fixed','superseded','split_to_follow_up')) or ($5 = false and f.status not in ('verified_fixed','superseded','split_to_follow_up'))) order by f.finding_number asc`
const getFindingSQL = `select ` + findingColumns + ` from den_review.review_findings f join den_review.review_rounds r on r.id = f.review_round_id where f.id = $1`
const respondFindingSQL = `update den_review.review_findings set response_by = $2, response_notes = $3, response_at = $9, status = coalesce($4, status), status_updated_by = case when $4::text is null then status_updated_by else $2 end, status_notes = case when $4::text is null then status_notes else $5 end, status_updated_at = case when $4::text is null then status_updated_at else $9 end, follow_up_task_id = case when $4 = 'split_to_follow_up' then $6 when $4::text is null then follow_up_task_id else null end, run_id = coalesce($7, run_id), subagent_role = coalesce($8, subagent_role), updated_at = $9 where id = $1 returning ` + findingColumns
const setFindingStatusSQL = `update den_review.review_findings set status = $2, status_updated_by = $3, status_notes = $4, status_updated_at = $8, follow_up_task_id = case when $2 = 'split_to_follow_up' then $5 else null end, run_id = coalesce($6, run_id), subagent_role = coalesce($7, subagent_role), updated_at = $8 where id = $1 returning ` + findingColumns
const insertFindingEventSQL = `insert into den_review.review_finding_events(project_id, task_id, review_round_id, review_finding_id, event_kind, actor, old_status, new_status, notes, response_notes, follow_up_task_id, run_id, subagent_role, created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`
const storePacketSQL = `insert into den_review.review_packets(project_id, task_id, review_round_id, packet_kind, sender, message_id, front_matter, typed_envelope, markdown_body, source_markdown, validation_status, validation_errors, idempotency_key, created_at, accepted_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) on conflict(project_id, idempotency_key) do update set message_id = excluded.message_id, front_matter = excluded.front_matter, typed_envelope = excluded.typed_envelope, markdown_body = excluded.markdown_body, source_markdown = excluded.source_markdown, validation_status = excluded.validation_status, validation_errors = excluded.validation_errors, accepted_at = excluded.accepted_at returning ` + packetColumns
const getPacketSQL = `select ` + packetColumns + ` from den_review.review_packets where id = $1`
const getPacketByIdempotencySQL = `select ` + packetColumns + ` from den_review.review_packets where project_id = $1 and idempotency_key = $2`
