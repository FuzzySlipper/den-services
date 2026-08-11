package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) RecordHumanAcceptance(
	ctx context.Context,
	command HumanAcceptanceCommand,
) (HumanAcceptanceMutation, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return HumanAcceptanceMutation{}, fmt.Errorf("beginning human acceptance: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := getTaskForUpdateTx(ctx, tx, command.TaskID)
	if err != nil {
		return HumanAcceptanceMutation{}, err
	}
	existing, err := getHumanAcceptanceTx(ctx, tx, command.TaskID, command.IdempotencyKey)
	if err == nil {
		if existing.RequestFingerprint != command.RequestFingerprint {
			return HumanAcceptanceMutation{}, conflict(ErrAcceptanceIdempotencyConflict, "human_acceptance_idempotency_conflict")
		}
		result, err := readHumanAcceptanceMutationTx(ctx, tx, existing, current)
		if err != nil {
			return HumanAcceptanceMutation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return HumanAcceptanceMutation{}, fmt.Errorf("committing human acceptance retry: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return HumanAcceptanceMutation{}, err
	}
	if command.ExpectedTaskUpdatedAt != nil && !current.UpdatedAt().Equal(command.ExpectedTaskUpdatedAt.UTC()) {
		return HumanAcceptanceMutation{}, conflict(ErrAcceptanceTaskChanged, "human_acceptance_task_changed")
	}
	if current.Status() == StatusCancelled && command.Facts.LifecycleEffect != HumanAcceptanceRecordOnly {
		return HumanAcceptanceMutation{}, conflict(ErrAcceptanceTaskCancelled, "human_acceptance_task_cancelled")
	}

	taskBefore := current.Status()
	taskAfter := current
	changedTaskIDs := make([]int64, 0, 2)
	unchangedTaskIDs := make([]int64, 0, 2)
	if command.Facts.LifecycleEffect != HumanAcceptanceRecordOnly {
		taskAfter, err = completeTaskTx(ctx, tx, current, command.Facts.ReviewerIdentity, command.CreatedAt)
		if err != nil {
			return HumanAcceptanceMutation{}, err
		}
		if taskBefore == taskAfter.Status() {
			unchangedTaskIDs = append(unchangedTaskIDs, taskAfter.ID())
		} else {
			changedTaskIDs = append(changedTaskIDs, taskAfter.ID())
		}
	} else {
		unchangedTaskIDs = append(unchangedTaskIDs, taskAfter.ID())
	}

	var parentBefore string
	var parentAfter *Task
	if command.Facts.LifecycleEffect == HumanAcceptanceCompleteTaskAndParent {
		parentID := taskAfter.ParentID()
		if parentID == nil {
			return HumanAcceptanceMutation{}, conflict(ErrAcceptanceParentMissing, "human_acceptance_parent_missing")
		}
		parent, parentErr := getTaskForUpdateTx(ctx, tx, *parentID)
		if parentErr != nil {
			return HumanAcceptanceMutation{}, parentErr
		}
		if parent.Status() == StatusCancelled {
			return HumanAcceptanceMutation{}, conflict(ErrAcceptanceParentIneligible, "human_acceptance_parent_ineligible")
		}
		eligible, eligibilityErr := parentCompletionEligibleTx(ctx, tx, parent.ID())
		if eligibilityErr != nil {
			return HumanAcceptanceMutation{}, eligibilityErr
		}
		if !eligible {
			return HumanAcceptanceMutation{}, conflict(ErrAcceptanceParentIneligible, "human_acceptance_parent_ineligible")
		}
		parentBefore = parent.Status()
		parentAfter, err = completeTaskTx(ctx, tx, parent, command.Facts.ReviewerIdentity, command.CreatedAt)
		if err != nil {
			return HumanAcceptanceMutation{}, err
		}
		if parentBefore == parentAfter.Status() {
			unchangedTaskIDs = append(unchangedTaskIDs, parentAfter.ID())
		} else {
			changedTaskIDs = append(changedTaskIDs, parentAfter.ID())
		}
	}

	review := &HumanAcceptanceReview{
		TaskID: command.TaskID, ProjectID: current.ProjectID(), IdempotencyKey: command.IdempotencyKey,
		RequestFingerprint: command.RequestFingerprint, ReviewerIdentity: command.Facts.ReviewerIdentity,
		Verdict: command.Facts.Verdict, Rationale: command.Facts.Rationale,
		ReviewedRevision: command.Facts.ReviewedRevision, ReviewedBuild: command.Facts.ReviewedBuild,
		ReviewedEnvironment: command.Facts.ReviewedEnvironment, EvidenceLinks: command.Facts.EvidenceLinks,
		LifecycleEffect: command.Facts.LifecycleEffect, NoteMarkdown: humanAcceptanceNote(command.Facts),
		TaskStatusBefore: taskBefore, TaskStatusAfter: taskAfter.Status(), ParentStatusBefore: parentBefore,
		CreatedAt: command.CreatedAt,
	}
	if parentAfter != nil {
		review.ParentTaskID = int64PtrValue(parentAfter.ID())
		review.ParentStatusAfter = parentAfter.Status()
	}
	review, err = insertHumanAcceptanceTx(ctx, tx, review)
	if err != nil {
		return HumanAcceptanceMutation{}, err
	}
	if err := recordTaskChangesTx(ctx, tx, "human_acceptance_recorded", taskAfter.ID()); err != nil {
		return HumanAcceptanceMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return HumanAcceptanceMutation{}, fmt.Errorf("committing human acceptance: %w", err)
	}
	return HumanAcceptanceMutation{
		Review: review, Task: taskAfter, Parent: parentAfter,
		ChangedTaskIDs: changedTaskIDs, UnchangedTaskIDs: unchangedTaskIDs,
	}, nil
}

func completeTaskTx(ctx context.Context, tx pgx.Tx, current *Task, reviewer string, at time.Time) (*Task, error) {
	if current.Status() == StatusDone {
		return current, nil
	}
	done := StatusDone
	patch := TaskPatch{Status: &done}
	if current.Status() == StatusBlocked {
		empty := ""
		falseValue := false
		patch.BlockerSummary = &empty
		patch.BlockerReason = &empty
		patch.BlockerAttemptedRemedies = &empty
		patch.BlockerSuggestedNextStep = &empty
		patch.BlockerRequiresHumanInput = &falseValue
	}
	return updateTaskTx(ctx, tx, current.ID(), current, patch, reviewer, at)
}

func parentCompletionEligibleTx(ctx context.Context, tx pgx.Tx, parentID int64) (bool, error) {
	var unfinishedChildren int
	if err := tx.QueryRow(ctx, unfinishedChildrenSQL, parentID).Scan(&unfinishedChildren); err != nil {
		return false, fmt.Errorf("checking parent children: %w", err)
	}
	var unfinishedDependencies int
	if err := tx.QueryRow(ctx, unfinishedParentDependenciesSQL, parentID).Scan(&unfinishedDependencies); err != nil {
		return false, fmt.Errorf("checking parent dependencies: %w", err)
	}
	return unfinishedChildren == 0 && unfinishedDependencies == 0, nil
}

func insertHumanAcceptanceTx(ctx context.Context, tx pgx.Tx, review *HumanAcceptanceReview) (*HumanAcceptanceReview, error) {
	evidence, err := json.Marshal(review.EvidenceLinks)
	if err != nil {
		return nil, fmt.Errorf("encoding human acceptance evidence: %w", err)
	}
	created, err := scanHumanAcceptance(tx.QueryRow(ctx, insertHumanAcceptanceSQL,
		review.TaskID, review.ProjectID, review.IdempotencyKey, review.RequestFingerprint,
		review.ReviewerIdentity, review.Verdict, review.Rationale, emptyToNil(review.ReviewedRevision),
		emptyToNil(review.ReviewedBuild), emptyToNil(review.ReviewedEnvironment), evidence,
		review.LifecycleEffect, review.NoteMarkdown, review.TaskStatusBefore, review.TaskStatusAfter,
		review.ParentTaskID, emptyToNil(review.ParentStatusBefore), emptyToNil(review.ParentStatusAfter), review.CreatedAt,
	))
	if err != nil {
		return nil, fmt.Errorf("inserting human acceptance: %w", err)
	}
	return created, nil
}

func getHumanAcceptanceTx(ctx context.Context, tx pgx.Tx, taskID int64, key string) (*HumanAcceptanceReview, error) {
	return scanHumanAcceptance(tx.QueryRow(ctx, getHumanAcceptanceSQL, taskID, key))
}

func readHumanAcceptanceMutationTx(
	ctx context.Context,
	tx pgx.Tx,
	review *HumanAcceptanceReview,
	current *Task,
) (HumanAcceptanceMutation, error) {
	var parent *Task
	changed := make([]int64, 0, 2)
	unchanged := make([]int64, 0, 2)
	if review.TaskStatusBefore != review.TaskStatusAfter {
		changed = append(changed, current.ID())
	} else {
		unchanged = append(unchanged, current.ID())
	}
	if review.ParentTaskID != nil {
		var err error
		parent, err = getTaskTx(ctx, tx, *review.ParentTaskID)
		if err != nil {
			return HumanAcceptanceMutation{}, err
		}
		if review.ParentStatusBefore != review.ParentStatusAfter {
			changed = append(changed, parent.ID())
		} else {
			unchanged = append(unchanged, parent.ID())
		}
	}
	return HumanAcceptanceMutation{
		Review: review, Task: current, Parent: parent, ChangedTaskIDs: changed, UnchangedTaskIDs: unchanged,
	}, nil
}

func (s *Store) humanAcceptanceReviews(ctx context.Context, taskID int64) ([]*HumanAcceptanceReview, error) {
	rows, err := s.pool.Query(ctx, listHumanAcceptancesSQL, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing human acceptance reviews: %w", err)
	}
	defer rows.Close()
	result := make([]*HumanAcceptanceReview, 0)
	for rows.Next() {
		review, scanErr := scanHumanAcceptance(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, review)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading human acceptance reviews: %w", err)
	}
	return result, nil
}

func scanHumanAcceptance(row rowScanner) (*HumanAcceptanceReview, error) {
	var review HumanAcceptanceReview
	var revision, build, environment, parentBefore, parentAfter *string
	var evidence []byte
	if err := row.Scan(
		&review.ID, &review.TaskID, &review.ProjectID, &review.IdempotencyKey, &review.RequestFingerprint,
		&review.ReviewerIdentity, &review.Verdict, &review.Rationale, &revision, &build, &environment,
		&evidence, &review.LifecycleEffect, &review.NoteMarkdown, &review.TaskStatusBefore, &review.TaskStatusAfter,
		&review.ParentTaskID, &parentBefore, &parentAfter, &review.CreatedAt,
	); err != nil {
		return nil, err
	}
	review.ReviewedRevision = nilToString(revision)
	review.ReviewedBuild = nilToString(build)
	review.ReviewedEnvironment = nilToString(environment)
	review.ParentStatusBefore = nilToString(parentBefore)
	review.ParentStatusAfter = nilToString(parentAfter)
	if len(evidence) > 0 {
		if err := json.Unmarshal(evidence, &review.EvidenceLinks); err != nil {
			return nil, fmt.Errorf("decoding human acceptance evidence: %w", err)
		}
	}
	return &review, nil
}

func int64PtrValue(value int64) *int64 { return &value }

const humanAcceptanceColumns = `
id, task_id, project_id, idempotency_key, request_fingerprint,
reviewer_identity, verdict, rationale, reviewed_revision, reviewed_build, reviewed_environment,
evidence_links, lifecycle_effect, note_markdown, task_status_before, task_status_after,
parent_task_id, parent_status_before, parent_status_after, created_at`

const insertHumanAcceptanceSQL = `
insert into den_tasks.human_acceptance_reviews (
	task_id, project_id, idempotency_key, request_fingerprint, reviewer_identity, verdict, rationale,
	reviewed_revision, reviewed_build, reviewed_environment, evidence_links, lifecycle_effect,
	note_markdown, task_status_before, task_status_after, parent_task_id, parent_status_before,
	parent_status_after, created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15, $16, $17, $18, $19)
returning ` + humanAcceptanceColumns

const getHumanAcceptanceSQL = `select ` + humanAcceptanceColumns + `
from den_tasks.human_acceptance_reviews where task_id = $1 and idempotency_key = $2`

const listHumanAcceptancesSQL = `select ` + humanAcceptanceColumns + `
from den_tasks.human_acceptance_reviews where task_id = $1 order by created_at desc, id desc`

const unfinishedChildrenSQL = `select count(*) from den_tasks.tasks where parent_id = $1 and status not in ('done', 'cancelled')`

const unfinishedParentDependenciesSQL = `
select count(*)
from den_tasks.task_dependencies td
join den_tasks.tasks dependency on dependency.id = td.depends_on
where td.task_id = $1 and dependency.status not in ('review', 'done', 'cancelled')`
