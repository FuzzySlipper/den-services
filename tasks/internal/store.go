package tasks

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
		return fmt.Errorf("pinging tasks store: %w", err)
	}
	return nil
}

func (s *Store) CreateTask(ctx context.Context, task *Task, dependsOn []int64) (*Task, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning create task: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := validateParent(ctx, tx, task.ProjectID(), 0, task.ParentID()); err != nil {
		return nil, err
	}
	created, err := scanTask(tx.QueryRow(ctx, createTaskSQL,
		task.ProjectID(),
		task.ParentID(),
		task.Title(),
		emptyToNil(task.Description()),
		task.Status(),
		task.Priority(),
		emptyToNil(task.AssignedTo()),
		jsonOrNil(task.Tags()),
		emptyToNil(task.BlockerSummary()),
		emptyToNil(task.BlockerReason()),
		emptyToNil(task.BlockerAttemptedRemedies()),
		emptyToNil(task.BlockerSuggestedNextStep()),
		task.BlockerRequiresHumanInput(),
		task.CreatedAt(),
		task.UpdatedAt(),
	))
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}
	for _, dependencyID := range dependsOn {
		if err := addDependencyTx(ctx, tx, created.ID(), dependencyID); err != nil {
			return nil, err
		}
	}
	if err := recordTaskChangesTx(ctx, tx, "created", created.ID()); err != nil {
		return nil, err
	}
	if parentID := created.ParentID(); parentID != nil {
		if err := recordTaskChangesTx(ctx, tx, "subtask_created", *parentID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing create task: %w", err)
	}
	return created, nil
}

func (s *Store) GetTask(ctx context.Context, id int64) (*Task, error) {
	task, err := scanTask(s.pool.QueryRow(ctx, getTaskSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting task %d: %w", id, err)
	}
	return task, nil
}

func (s *Store) GetDetail(ctx context.Context, id int64) (TaskDetail, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	dependencies, err := s.dependencies(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	taskID := task.ID()
	subtasks, err := s.ListTasks(ctx, ListTasksQuery{ProjectID: task.ProjectID(), ParentID: &taskID})
	if err != nil {
		return TaskDetail{}, err
	}
	history, err := s.History(ctx, id)
	if err != nil {
		return TaskDetail{}, err
	}
	return TaskDetail{Task: task, Dependencies: dependencies, Subtasks: subtasks, History: history}, nil
}

func (s *Store) ListTasks(ctx context.Context, query ListTasksQuery) ([]TaskSummary, error) {
	rows, err := s.pool.Query(ctx, listTasksSQL,
		query.ProjectID,
		query.Statuses,
		emptyToNil(query.AssignedTo),
		query.Tags,
		query.MaxPriority,
		query.ParentID,
		query.IncludeAll,
	)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()
	return scanTaskSummaries(rows)
}

func (s *Store) UpdateTask(ctx context.Context, id int64, patch TaskPatch, agent string, updatedAt time.Time) (*Task, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning update task: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := getTaskTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if patch.HasParent {
		if err := validateParent(ctx, tx, current.ProjectID(), id, patch.ParentID); err != nil {
			return nil, err
		}
	}
	if err := writeHistory(ctx, tx, id, current, patch, agent); err != nil {
		return nil, err
	}
	updated, err := scanTask(tx.QueryRow(ctx, updateTaskSQL,
		id,
		patch.Title,
		patch.Description,
		patch.Status,
		patch.Priority,
		patch.AssignedTo,
		patch.HasTags,
		jsonOrNil(patch.Tags),
		patch.HasParent,
		patch.ParentID,
		patch.BlockerSummary,
		patch.BlockerReason,
		patch.BlockerAttemptedRemedies,
		patch.BlockerSuggestedNextStep,
		patch.BlockerRequiresHumanInput,
		updatedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("updating task %d: %w", id, err)
	}
	if err := recordUpdateTaskChangesTx(ctx, tx, current, updated, patch); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing update task: %w", err)
	}
	return updated, nil
}

func (s *Store) AddDependency(ctx context.Context, taskID int64, dependsOn int64) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning add dependency: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := addDependencyTx(ctx, tx, taskID, dependsOn); err != nil {
		return err
	}
	if err := recordTaskChangesTx(ctx, tx, "dependency_added", taskID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing add dependency: %w", err)
	}
	return nil
}

func (s *Store) RemoveDependency(ctx context.Context, taskID int64, dependsOn int64) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning remove dependency: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, removeDependencySQL, taskID, dependsOn)
	if err != nil {
		return fmt.Errorf("removing dependency %d -> %d: %w", taskID, dependsOn, err)
	}
	if err := recordTaskChangesTx(ctx, tx, "dependency_removed", taskID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing remove dependency: %w", err)
	}
	return nil
}

func (s *Store) NextTask(ctx context.Context, projectID string, assignedTo string) (*Task, error) {
	task, err := scanTask(s.pool.QueryRow(ctx, nextTaskSQL, projectID, emptyToNil(assignedTo)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting next task: %w", err)
	}
	return task, nil
}

func (s *Store) History(ctx context.Context, taskID int64) ([]TaskHistoryEntry, error) {
	rows, err := s.pool.Query(ctx, historySQL, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing task history: %w", err)
	}
	defer rows.Close()
	return scanHistory(rows)
}

func (s *Store) ListTaskChanges(ctx context.Context, query TaskChangeQuery) ([]TaskChangeEvent, error) {
	rows, err := s.pool.Query(ctx, listTaskChangesSQL, query.ProjectID, query.AfterID, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("listing task changes: %w", err)
	}
	defer rows.Close()
	return scanTaskChangeEvents(rows)
}

func recordUpdateTaskChangesTx(ctx context.Context, tx pgx.Tx, current *Task, updated *Task, patch TaskPatch) error {
	taskIDs := []int64{updated.ID()}
	if patch.Status != nil {
		dependentIDs, err := dependentTaskIDsTx(ctx, tx, updated.ID())
		if err != nil {
			return err
		}
		taskIDs = append(taskIDs, dependentIDs...)
	}
	if patch.HasParent {
		if oldParent := current.ParentID(); oldParent != nil {
			taskIDs = append(taskIDs, *oldParent)
		}
		if newParent := updated.ParentID(); newParent != nil {
			taskIDs = append(taskIDs, *newParent)
		}
	}
	return recordTaskChangesTx(ctx, tx, "updated", taskIDs...)
}

func recordTaskChangesTx(ctx context.Context, tx pgx.Tx, kind string, taskIDs ...int64) error {
	seen := make(map[int64]bool, len(taskIDs))
	for _, taskID := range taskIDs {
		if taskID <= 0 || seen[taskID] {
			continue
		}
		seen[taskID] = true
		if _, err := tx.Exec(ctx, insertTaskChangeSQL, taskID, kind); err != nil {
			return fmt.Errorf("recording task change %d: %w", taskID, err)
		}
	}
	return nil
}

func dependentTaskIDsTx(ctx context.Context, tx pgx.Tx, taskID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, dependentTaskIDsSQL, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing task dependents: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading task dependents: %w", err)
	}
	return ids, nil
}

func (s *Store) dependencies(ctx context.Context, taskID int64) ([]DependencyInfo, error) {
	rows, err := s.pool.Query(ctx, dependenciesSQL, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing task dependencies: %w", err)
	}
	defer rows.Close()
	var dependencies []DependencyInfo
	for rows.Next() {
		var dependency DependencyInfo
		if err := rows.Scan(&dependency.TaskID, &dependency.ProjectID, &dependency.Title, &dependency.Status); err != nil {
			return nil, err
		}
		dependencies = append(dependencies, dependency)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading dependencies: %w", err)
	}
	return dependencies, nil
}

func addDependencyTx(ctx context.Context, tx pgx.Tx, taskID int64, dependsOn int64) error {
	if taskID == dependsOn {
		return validationFailed(ErrDependencyCycle)
	}
	if _, err := getTaskTx(ctx, tx, taskID); err != nil {
		return err
	}
	if _, err := getTaskTx(ctx, tx, dependsOn); err != nil {
		return err
	}
	if createsDependencyCycle(ctx, tx, taskID, dependsOn) {
		return conflict(fmt.Errorf("%w: %d depends on %d", ErrDependencyCycle, taskID, dependsOn), "dependency_cycle")
	}
	_, err := tx.Exec(ctx, addDependencySQL, taskID, dependsOn)
	if err != nil {
		return fmt.Errorf("adding dependency %d -> %d: %w", taskID, dependsOn, err)
	}
	return nil
}

func validateParent(ctx context.Context, tx pgx.Tx, projectID string, taskID int64, parentID *int64) error {
	if parentID == nil {
		return nil
	}
	if taskID != 0 && *parentID == taskID {
		return validationFailed(ErrParentCycle)
	}
	parent, err := getTaskTx(ctx, tx, *parentID)
	if err != nil {
		return err
	}
	if parent.ProjectID() != projectID {
		return validationFailed(ErrParentProjectMismatch)
	}
	if taskID != 0 && createsParentCycle(ctx, tx, taskID, *parentID) {
		return conflict(fmt.Errorf("%w: task %d parent %d", ErrParentCycle, taskID, *parentID), "parent_cycle")
	}
	return nil
}

func getTaskTx(ctx context.Context, tx pgx.Tx, id int64) (*Task, error) {
	task, err := scanTask(tx.QueryRow(ctx, getTaskSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, err
	}
	return task, nil
}

func createsDependencyCycle(ctx context.Context, tx pgx.Tx, taskID int64, dependsOn int64) bool {
	visited := map[int64]bool{}
	queue := []int64{dependsOn}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == taskID {
			return true
		}
		if visited[current] {
			continue
		}
		visited[current] = true
		rows, err := tx.Query(ctx, dependencyIDsSQL, current)
		if err != nil {
			return true
		}
		for rows.Next() {
			var next int64
			if err := rows.Scan(&next); err == nil {
				queue = append(queue, next)
			}
		}
		rows.Close()
	}
	return false
}

func createsParentCycle(ctx context.Context, tx pgx.Tx, taskID int64, parentID int64) bool {
	current := parentID
	for current != 0 {
		if current == taskID {
			return true
		}
		var next *int64
		err := tx.QueryRow(ctx, parentIDSQL, current).Scan(&next)
		if errors.Is(err, pgx.ErrNoRows) || next == nil {
			return false
		}
		if err != nil {
			return true
		}
		current = *next
	}
	return false
}

func writeHistory(ctx context.Context, tx pgx.Tx, taskID int64, current *Task, patch TaskPatch, agent string) error {
	changes := []struct {
		field string
		old   string
		value *string
	}{
		{"title", current.Title(), patch.Title},
		{"description", current.Description(), patch.Description},
		{"status", current.Status(), patch.Status},
		{"assigned_to", current.AssignedTo(), patch.AssignedTo},
		{"blocker_summary", current.BlockerSummary(), patch.BlockerSummary},
		{"blocker_reason", current.BlockerReason(), patch.BlockerReason},
		{"blocker_attempted_remedies", current.BlockerAttemptedRemedies(), patch.BlockerAttemptedRemedies},
		{"blocker_suggested_next_step", current.BlockerSuggestedNextStep(), patch.BlockerSuggestedNextStep},
	}
	for _, change := range changes {
		if change.value == nil || change.old == *change.value {
			continue
		}
		if err := insertHistory(ctx, tx, taskID, change.field, change.old, *change.value, agent); err != nil {
			return err
		}
	}
	if patch.Priority != nil && current.Priority() != *patch.Priority {
		if err := insertHistory(ctx, tx, taskID, "priority", fmt.Sprint(current.Priority()), fmt.Sprint(*patch.Priority), agent); err != nil {
			return err
		}
	}
	if patch.HasTags {
		oldTags, _ := json.Marshal(current.Tags())
		newTags, _ := json.Marshal(patch.Tags)
		if string(oldTags) != string(newTags) {
			if err := insertHistory(ctx, tx, taskID, "tags", string(oldTags), string(newTags), agent); err != nil {
				return err
			}
		}
	}
	if patch.HasParent {
		oldParent := int64String(current.ParentID())
		newParent := int64String(patch.ParentID)
		if oldParent != newParent {
			if err := insertHistory(ctx, tx, taskID, "parent_id", oldParent, newParent, agent); err != nil {
				return err
			}
		}
	}
	if patch.BlockerRequiresHumanInput != nil && current.BlockerRequiresHumanInput() != *patch.BlockerRequiresHumanInput {
		if err := insertHistory(ctx, tx, taskID, "blocker_requires_human_input", fmt.Sprint(current.BlockerRequiresHumanInput()), fmt.Sprint(*patch.BlockerRequiresHumanInput), agent); err != nil {
			return err
		}
	}
	return nil
}

func insertHistory(ctx context.Context, tx pgx.Tx, taskID int64, field string, oldValue string, newValue string, agent string) error {
	_, err := tx.Exec(ctx, insertHistorySQL, taskID, field, emptyToNil(oldValue), emptyToNil(newValue), agent)
	if err != nil {
		return fmt.Errorf("writing task history: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (*Task, error) {
	var parentID *int64
	var description *string
	var assignedTo *string
	var tagsJSON []byte
	var blockerSummary *string
	var blockerReason *string
	var blockerAttemptedRemedies *string
	var blockerSuggestedNextStep *string
	var params NewTaskParams
	if err := row.Scan(
		&params.ID,
		&params.ProjectID,
		&parentID,
		&params.Title,
		&description,
		&params.Status,
		&params.Priority,
		&assignedTo,
		&tagsJSON,
		&blockerSummary,
		&blockerReason,
		&blockerAttemptedRemedies,
		&blockerSuggestedNextStep,
		&params.BlockerRequiresHumanInput,
		&params.CreatedAt,
		&params.UpdatedAt,
	); err != nil {
		return nil, err
	}
	params.ParentID = parentID
	params.Description = nilToString(description)
	params.AssignedTo = nilToString(assignedTo)
	params.Tags = tagsFromJSON(tagsJSON)
	params.BlockerSummary = nilToString(blockerSummary)
	params.BlockerReason = nilToString(blockerReason)
	params.BlockerAttemptedRemedies = nilToString(blockerAttemptedRemedies)
	params.BlockerSuggestedNextStep = nilToString(blockerSuggestedNextStep)
	return NewTask(params)
}

func scanTaskSummaries(rows pgx.Rows) ([]TaskSummary, error) {
	var summaries []TaskSummary
	for rows.Next() {
		summary, err := scanTaskSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading task summaries: %w", err)
	}
	return summaries, nil
}

func scanTaskSummary(row rowScanner) (TaskSummary, error) {
	var summary TaskSummary
	var parentID *int64
	var description *string
	var assignedTo *string
	var tagsJSON []byte
	var blockerSummary *string
	var blockerReason *string
	var blockerAttemptedRemedies *string
	var blockerSuggestedNextStep *string
	var params NewTaskParams
	if err := row.Scan(
		&params.ID,
		&params.ProjectID,
		&parentID,
		&params.Title,
		&description,
		&params.Status,
		&params.Priority,
		&assignedTo,
		&tagsJSON,
		&blockerSummary,
		&blockerReason,
		&blockerAttemptedRemedies,
		&blockerSuggestedNextStep,
		&params.BlockerRequiresHumanInput,
		&params.CreatedAt,
		&params.UpdatedAt,
		&summary.DependencyCount,
		&summary.UnfinishedDependencyCount,
		&summary.SubtaskCount,
	); err != nil {
		return TaskSummary{}, err
	}
	params.ParentID = parentID
	params.Description = nilToString(description)
	params.AssignedTo = nilToString(assignedTo)
	params.Tags = tagsFromJSON(tagsJSON)
	params.BlockerSummary = nilToString(blockerSummary)
	params.BlockerReason = nilToString(blockerReason)
	params.BlockerAttemptedRemedies = nilToString(blockerAttemptedRemedies)
	params.BlockerSuggestedNextStep = nilToString(blockerSuggestedNextStep)
	task, err := NewTask(params)
	if err != nil {
		return TaskSummary{}, err
	}
	summary.Task = task
	return summary, nil
}

func scanTaskChangeEvents(rows pgx.Rows) ([]TaskChangeEvent, error) {
	var events []TaskChangeEvent
	for rows.Next() {
		var event TaskChangeEvent
		summary, err := scanTaskSummaryWithPrefix(rows, &event.ID, &event.Kind, &event.Changed)
		if err != nil {
			return nil, err
		}
		event.Summary = summary
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading task changes: %w", err)
	}
	return events, nil
}

func scanTaskSummaryWithPrefix(row rowScanner, prefix ...any) (TaskSummary, error) {
	var summary TaskSummary
	var parentID *int64
	var description *string
	var assignedTo *string
	var tagsJSON []byte
	var blockerSummary *string
	var blockerReason *string
	var blockerAttemptedRemedies *string
	var blockerSuggestedNextStep *string
	var params NewTaskParams
	dest := append(prefix,
		&params.ID,
		&params.ProjectID,
		&parentID,
		&params.Title,
		&description,
		&params.Status,
		&params.Priority,
		&assignedTo,
		&tagsJSON,
		&blockerSummary,
		&blockerReason,
		&blockerAttemptedRemedies,
		&blockerSuggestedNextStep,
		&params.BlockerRequiresHumanInput,
		&params.CreatedAt,
		&params.UpdatedAt,
		&summary.DependencyCount,
		&summary.UnfinishedDependencyCount,
		&summary.SubtaskCount,
	)
	if err := row.Scan(dest...); err != nil {
		return TaskSummary{}, err
	}
	params.ParentID = parentID
	params.Description = nilToString(description)
	params.AssignedTo = nilToString(assignedTo)
	params.Tags = tagsFromJSON(tagsJSON)
	params.BlockerSummary = nilToString(blockerSummary)
	params.BlockerReason = nilToString(blockerReason)
	params.BlockerAttemptedRemedies = nilToString(blockerAttemptedRemedies)
	params.BlockerSuggestedNextStep = nilToString(blockerSuggestedNextStep)
	task, err := NewTask(params)
	if err != nil {
		return TaskSummary{}, err
	}
	summary.Task = task
	return summary, nil
}

func scanHistory(rows pgx.Rows) ([]TaskHistoryEntry, error) {
	var entries []TaskHistoryEntry
	for rows.Next() {
		var entry TaskHistoryEntry
		var oldValue *string
		var newValue *string
		var changedBy *string
		if err := rows.Scan(&entry.ID, &entry.TaskID, &entry.Field, &oldValue, &newValue, &changedBy, &entry.ChangedAt); err != nil {
			return nil, err
		}
		entry.OldValue = nilToString(oldValue)
		entry.NewValue = nilToString(newValue)
		entry.ChangedBy = nilToString(changedBy)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading history: %w", err)
	}
	return entries, nil
}

func tagsFromJSON(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var tags []string
	_ = json.Unmarshal(data, &tags)
	return normalizeTags(tags)
}

func jsonOrNil(value []string) any {
	if len(value) == 0 {
		return nil
	}
	data, _ := json.Marshal(value)
	return data
}

func emptyToNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nilToString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64String(value *int64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(*value)
}

const taskColumns = `
id, project_id, parent_id, title, description, status, priority, assigned_to, tags,
blocker_summary, blocker_reason, blocker_attempted_remedies, blocker_suggested_next_step,
blocker_requires_human_input, created_at, updated_at`

const qualifiedTaskColumns = `
t.id, t.project_id, t.parent_id, t.title, t.description, t.status, t.priority, t.assigned_to, t.tags,
t.blocker_summary, t.blocker_reason, t.blocker_attempted_remedies, t.blocker_suggested_next_step,
t.blocker_requires_human_input, t.created_at, t.updated_at`

const createTaskSQL = `
insert into den_tasks.tasks (
	project_id, parent_id, title, description, status, priority, assigned_to, tags,
	blocker_summary, blocker_reason, blocker_attempted_remedies, blocker_suggested_next_step,
	blocker_requires_human_input, created_at, updated_at
)
values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13, $14, $15)
returning ` + taskColumns

const getTaskSQL = `select ` + taskColumns + ` from den_tasks.tasks where id = $1`

const listTasksSQL = `
select ` + taskColumns + `,
	(select count(*) from den_tasks.task_dependencies where task_id = t.id) as dep_count,
	(select count(*) from den_tasks.task_dependencies td join den_tasks.tasks dep on dep.id = td.depends_on where td.task_id = t.id and dep.status not in ('review', 'done', 'cancelled')) as unfinished_dep_count,
	(select count(*) from den_tasks.tasks where parent_id = t.id) as sub_count
from den_tasks.tasks t
where t.project_id = $1
  and ($2::text[] is null or cardinality($2::text[]) = 0 or t.status = any($2::text[]))
  and ($3::text is null or t.assigned_to = $3)
  and ($4::text[] is null or cardinality($4::text[]) = 0 or t.tags ?& $4::text[])
  and ($5::integer is null or t.priority <= $5)
  and (($6::bigint is not null and t.parent_id = $6) or ($6::bigint is null and ($7::boolean or t.parent_id is null)))
order by t.priority, t.id`

const updateTaskSQL = `
update den_tasks.tasks
set title = coalesce($2, title),
    description = case when $3::text is null then description when $3::text = '' then null else $3::text end,
    status = coalesce($4, status),
    priority = coalesce($5, priority),
    assigned_to = case when $6::text is null then assigned_to when $6::text = '' then null else $6::text end,
    tags = case when $7::boolean then $8::jsonb else tags end,
    parent_id = case when $9::boolean then $10::bigint else parent_id end,
    blocker_summary = case when $11::text is null then blocker_summary when $11::text = '' then null else $11::text end,
    blocker_reason = case when $12::text is null then blocker_reason when $12::text = '' then null else $12::text end,
    blocker_attempted_remedies = case when $13::text is null then blocker_attempted_remedies when $13::text = '' then null else $13::text end,
    blocker_suggested_next_step = case when $14::text is null then blocker_suggested_next_step when $14::text = '' then null else $14::text end,
    blocker_requires_human_input = coalesce($15, blocker_requires_human_input),
    updated_at = $16
where id = $1
returning ` + taskColumns

const (
	addDependencySQL    = `insert into den_tasks.task_dependencies (task_id, depends_on) values ($1, $2) on conflict do nothing`
	removeDependencySQL = `delete from den_tasks.task_dependencies where task_id = $1 and depends_on = $2`
	dependencyIDsSQL    = `select depends_on from den_tasks.task_dependencies where task_id = $1`
	parentIDSQL         = `select parent_id from den_tasks.tasks where id = $1`
)

const dependenciesSQL = `
select t.id, t.project_id, t.title, t.status
from den_tasks.task_dependencies td
join den_tasks.tasks t on t.id = td.depends_on
where td.task_id = $1
order by t.id`

const historySQL = `
select id, task_id, field, old_value, new_value, changed_by, changed_at
from den_tasks.task_history
where task_id = $1
order by changed_at desc, id desc`

const insertHistorySQL = `
insert into den_tasks.task_history (task_id, field, old_value, new_value, changed_by)
values ($1, $2, $3, $4, $5)`

const insertTaskChangeSQL = `
insert into den_tasks.task_change_events (task_id, project_id, change_kind)
select id, project_id, $2
from den_tasks.tasks
where id = $1`

const listTaskChangesSQL = `
select e.id, e.change_kind, e.changed_at, ` + qualifiedTaskColumns + `,
	(select count(*) from den_tasks.task_dependencies where task_id = t.id) as dep_count,
	(select count(*) from den_tasks.task_dependencies td join den_tasks.tasks dep on dep.id = td.depends_on where td.task_id = t.id and dep.status not in ('review', 'done', 'cancelled')) as unfinished_dep_count,
	(select count(*) from den_tasks.tasks where parent_id = t.id) as sub_count
from den_tasks.task_change_events e
join den_tasks.tasks t on t.id = e.task_id
where e.project_id = $1 and e.id > $2
order by e.id
limit $3`

const dependentTaskIDsSQL = `select task_id from den_tasks.task_dependencies where depends_on = $1 order by task_id`

const nextTaskSQL = `
with unblocked as (
	select t.id
	from den_tasks.tasks t
	where t.project_id = $1
	  and not exists (
		select 1
		from den_tasks.task_dependencies td
		join den_tasks.tasks dep on dep.id = td.depends_on
		where td.task_id = t.id and dep.status not in ('review', 'done', 'cancelled')
	  )
),
candidates as (
	select t.*, 0 as tier, (select count(*) from den_tasks.task_dependencies where task_id = t.id) as dep_count
	from den_tasks.tasks t
	join den_tasks.tasks parent on parent.id = t.parent_id and parent.status in ('in_progress', 'review')
	where t.project_id = $1
	  and t.status in ('planned', 'in_progress')
	  and t.id in (select id from unblocked)
	  and ($2::text is null or t.assigned_to is null or t.assigned_to = $2)
	union all
	select t.*, 1 as tier, (select count(*) from den_tasks.task_dependencies where task_id = t.id) as dep_count
	from den_tasks.tasks t
	where t.project_id = $1
	  and t.parent_id is null
	  and t.status = 'planned'
	  and t.id in (select id from unblocked)
	  and ($2::text is null or t.assigned_to is null or t.assigned_to = $2)
)
select ` + taskColumns + `
from candidates
order by tier, priority, dep_count, id
limit 1`
