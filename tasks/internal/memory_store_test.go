package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type memoryStore struct {
	mu            sync.Mutex
	nextTaskID    int64
	nextHistoryID int64
	nextEventID   int64
	tasks         map[int64]*Task
	dependencies  map[int64]map[int64]bool
	history       []TaskHistoryEntry
	changes       []TaskChangeEvent
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		nextTaskID:    1,
		nextHistoryID: 1,
		nextEventID:   1,
		tasks:         make(map[int64]*Task),
		dependencies:  make(map[int64]map[int64]bool),
	}
}

func (s *memoryStore) Ping(context.Context) error {
	return nil
}

func (s *memoryStore) CreateTask(_ context.Context, task *Task, dependsOn []int64) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.validateParentLocked(task.ProjectID(), 0, task.ParentID()); err != nil {
		return nil, err
	}
	taskID := s.nextTaskID
	s.nextTaskID++
	created, err := NewTask(NewTaskParams{
		ID:                        taskID,
		ProjectID:                 task.ProjectID(),
		ParentID:                  task.ParentID(),
		Title:                     task.Title(),
		Description:               task.Description(),
		Status:                    task.Status(),
		Priority:                  task.Priority(),
		AssignedTo:                task.AssignedTo(),
		Tags:                      task.Tags(),
		BlockerSummary:            task.BlockerSummary(),
		BlockerReason:             task.BlockerReason(),
		BlockerAttemptedRemedies:  task.BlockerAttemptedRemedies(),
		BlockerSuggestedNextStep:  task.BlockerSuggestedNextStep(),
		BlockerRequiresHumanInput: task.BlockerRequiresHumanInput(),
		CreatedAt:                 task.CreatedAt(),
		UpdatedAt:                 task.UpdatedAt(),
	})
	if err != nil {
		return nil, err
	}
	s.tasks[taskID] = created
	for _, depID := range dependsOn {
		if err := s.addDependencyLocked(taskID, depID); err != nil {
			delete(s.tasks, taskID)
			return nil, err
		}
	}
	s.appendChangeLocked("created", taskID, task.CreatedAt())
	if parentID := created.ParentID(); parentID != nil {
		s.appendChangeLocked("subtask_created", *parentID, task.CreatedAt())
	}
	return cloneTask(created), nil
}

func (s *memoryStore) GetTask(_ context.Context, id int64) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, notFound(id)
	}
	return cloneTask(task), nil
}

func (s *memoryStore) GetDetail(_ context.Context, id int64) (TaskDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return TaskDetail{}, notFound(id)
	}
	return TaskDetail{
		Task:         cloneTask(task),
		Dependencies: s.dependenciesLocked(id),
		Subtasks:     s.listLocked(ListTasksQuery{ProjectID: task.ProjectID(), ParentID: &id}),
		History:      s.historyLocked(id),
	}, nil
}

func (s *memoryStore) ListTasks(_ context.Context, query ListTasksQuery) ([]TaskSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(query), nil
}

func (s *memoryStore) UpdateTask(_ context.Context, id int64, patch TaskPatch, agent string, updatedAt time.Time) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.tasks[id]
	if !ok {
		return nil, notFound(id)
	}
	if patch.HasParent {
		if err := s.validateParentLocked(current.ProjectID(), id, patch.ParentID); err != nil {
			return nil, err
		}
	}
	params := NewTaskParams{
		ID:                        current.ID(),
		ProjectID:                 current.ProjectID(),
		ParentID:                  current.ParentID(),
		Title:                     current.Title(),
		Description:               current.Description(),
		Status:                    current.Status(),
		Priority:                  current.Priority(),
		AssignedTo:                current.AssignedTo(),
		Tags:                      current.Tags(),
		BlockerSummary:            current.BlockerSummary(),
		BlockerReason:             current.BlockerReason(),
		BlockerAttemptedRemedies:  current.BlockerAttemptedRemedies(),
		BlockerSuggestedNextStep:  current.BlockerSuggestedNextStep(),
		BlockerRequiresHumanInput: current.BlockerRequiresHumanInput(),
		CreatedAt:                 current.CreatedAt(),
		UpdatedAt:                 updatedAt,
	}
	changes := map[string][2]string{}
	if patch.Title != nil {
		changes["title"] = [2]string{params.Title, *patch.Title}
		params.Title = *patch.Title
	}
	if patch.Description != nil {
		changes["description"] = [2]string{params.Description, *patch.Description}
		params.Description = *patch.Description
	}
	if patch.Status != nil {
		changes["status"] = [2]string{params.Status, *patch.Status}
		params.Status = *patch.Status
	}
	if patch.Priority != nil {
		changes["priority"] = [2]string{intString(params.Priority), intString(*patch.Priority)}
		params.Priority = *patch.Priority
	}
	if patch.AssignedTo != nil {
		changes["assigned_to"] = [2]string{params.AssignedTo, *patch.AssignedTo}
		params.AssignedTo = *patch.AssignedTo
	}
	if patch.HasTags {
		changes["tags"] = [2]string{stringsForHistory(params.Tags), stringsForHistory(patch.Tags)}
		params.Tags = patch.Tags
	}
	if patch.HasParent {
		changes["parent_id"] = [2]string{int64String(params.ParentID), int64String(patch.ParentID)}
		params.ParentID = patch.ParentID
	}
	if patch.BlockerSummary != nil {
		changes["blocker_summary"] = [2]string{params.BlockerSummary, *patch.BlockerSummary}
		params.BlockerSummary = *patch.BlockerSummary
	}
	if patch.BlockerReason != nil {
		changes["blocker_reason"] = [2]string{params.BlockerReason, *patch.BlockerReason}
		params.BlockerReason = *patch.BlockerReason
	}
	if patch.BlockerAttemptedRemedies != nil {
		changes["blocker_attempted_remedies"] = [2]string{params.BlockerAttemptedRemedies, *patch.BlockerAttemptedRemedies}
		params.BlockerAttemptedRemedies = *patch.BlockerAttemptedRemedies
	}
	if patch.BlockerSuggestedNextStep != nil {
		changes["blocker_suggested_next_step"] = [2]string{params.BlockerSuggestedNextStep, *patch.BlockerSuggestedNextStep}
		params.BlockerSuggestedNextStep = *patch.BlockerSuggestedNextStep
	}
	if patch.BlockerRequiresHumanInput != nil {
		changes["blocker_requires_human_input"] = [2]string{boolString(params.BlockerRequiresHumanInput), boolString(*patch.BlockerRequiresHumanInput)}
		params.BlockerRequiresHumanInput = *patch.BlockerRequiresHumanInput
	}
	updated, err := NewTask(params)
	if err != nil {
		return nil, err
	}
	s.tasks[id] = updated
	for field, values := range changes {
		if values[0] != values[1] {
			s.appendHistoryLocked(id, field, values[0], values[1], agent, updatedAt)
		}
	}
	s.appendChangeLocked("updated", id, updatedAt)
	if patch.Status != nil {
		for dependentID := range s.dependentTaskIDsLocked(id) {
			s.appendChangeLocked("updated", dependentID, updatedAt)
		}
	}
	if patch.HasParent {
		if oldParent := current.ParentID(); oldParent != nil {
			s.appendChangeLocked("updated", *oldParent, updatedAt)
		}
		if newParent := updated.ParentID(); newParent != nil {
			s.appendChangeLocked("updated", *newParent, updatedAt)
		}
	}
	return cloneTask(updated), nil
}

func (s *memoryStore) AddDependency(_ context.Context, taskID int64, dependsOn int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.addDependencyLocked(taskID, dependsOn); err != nil {
		return err
	}
	s.appendChangeLocked("dependency_added", taskID, time.Now().UTC())
	return nil
}

func (s *memoryStore) RemoveDependency(_ context.Context, taskID int64, dependsOn int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dependencies[taskID] != nil {
		delete(s.dependencies[taskID], dependsOn)
	}
	s.appendChangeLocked("dependency_removed", taskID, time.Now().UTC())
	return nil
}

func (s *memoryStore) NextTask(_ context.Context, projectID string, assignedTo string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidates := make([]struct {
		task *Task
		tier int
		deps int
	}, 0)
	for _, task := range s.tasks {
		if task.ProjectID() != projectID || !s.unblockedLocked(task.ID()) {
			continue
		}
		if assignedTo != "" && task.AssignedTo() != assignedTo {
			continue
		}
		if parentID := task.ParentID(); parentID != nil {
			parent := s.tasks[*parentID]
			if parent != nil && parent.Status() == StatusInProgress && (task.Status() == StatusPlanned || task.Status() == StatusInProgress) {
				candidates = append(candidates, struct {
					task *Task
					tier int
					deps int
				}{task: task, tier: 0, deps: len(s.dependencies[task.ID()])})
			}
			continue
		}
		if task.Status() == StatusPlanned {
			candidates = append(candidates, struct {
				task *Task
				tier int
				deps int
			}{task: task, tier: 1, deps: len(s.dependencies[task.ID()])})
		}
	}
	sort.Slice(candidates, func(left int, right int) bool {
		a, b := candidates[left], candidates[right]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.task.Priority() != b.task.Priority() {
			return a.task.Priority() < b.task.Priority()
		}
		if a.deps != b.deps {
			return a.deps < b.deps
		}
		return a.task.ID() < b.task.ID()
	})
	if len(candidates) == 0 {
		return nil, nil
	}
	return cloneTask(candidates[0].task), nil
}

func (s *memoryStore) History(_ context.Context, taskID int64) ([]TaskHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.historyLocked(taskID), nil
}

func (s *memoryStore) ListTaskChanges(_ context.Context, query TaskChangeQuery) ([]TaskChangeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var events []TaskChangeEvent
	for _, event := range s.changes {
		if event.ID <= query.AfterID || event.Summary.Task.ProjectID() != query.ProjectID {
			continue
		}
		events = append(events, event)
		if len(events) == query.Limit {
			break
		}
	}
	return events, nil
}

func (s *memoryStore) addDependencyLocked(taskID int64, dependsOn int64) error {
	task := s.tasks[taskID]
	dep := s.tasks[dependsOn]
	if task == nil {
		return notFound(taskID)
	}
	if dep == nil {
		return notFound(dependsOn)
	}
	if taskID == dependsOn {
		return validationFailed(ErrDependencyCycle)
	}
	if task.ProjectID() != dep.ProjectID() {
		return validationFailed(ErrDependencyProjectMismatch)
	}
	if s.dependencyCycleLocked(taskID, dependsOn) {
		return conflict(ErrDependencyCycle, "dependency_cycle")
	}
	if s.dependencies[taskID] == nil {
		s.dependencies[taskID] = make(map[int64]bool)
	}
	s.dependencies[taskID][dependsOn] = true
	return nil
}

func (s *memoryStore) dependencyCycleLocked(taskID int64, dependsOn int64) bool {
	queue := []int64{dependsOn}
	seen := map[int64]bool{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == taskID {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		for next := range s.dependencies[current] {
			queue = append(queue, next)
		}
	}
	return false
}

func (s *memoryStore) validateParentLocked(projectID string, taskID int64, parentID *int64) error {
	if parentID == nil {
		return nil
	}
	parent := s.tasks[*parentID]
	if parent == nil {
		return notFound(*parentID)
	}
	if parent.ProjectID() != projectID {
		return validationFailed(ErrParentProjectMismatch)
	}
	if taskID != 0 {
		current := parentID
		for current != nil {
			if *current == taskID {
				return conflict(ErrParentCycle, "parent_cycle")
			}
			nextParent := s.tasks[*current]
			if nextParent == nil {
				return nil
			}
			current = nextParent.ParentID()
		}
	}
	return nil
}

func (s *memoryStore) listLocked(query ListTasksQuery) []TaskSummary {
	var summaries []TaskSummary
	for _, task := range s.tasks {
		if task.ProjectID() != query.ProjectID {
			continue
		}
		if len(query.Statuses) > 0 && !contains(query.Statuses, task.Status()) {
			continue
		}
		if query.AssignedTo != "" && task.AssignedTo() != query.AssignedTo {
			continue
		}
		if query.MaxPriority != nil && task.Priority() > *query.MaxPriority {
			continue
		}
		if query.ParentID != nil {
			parentID := task.ParentID()
			if parentID == nil || *parentID != *query.ParentID {
				continue
			}
		} else if !query.IncludeAll && task.ParentID() != nil {
			continue
		}
		if !containsAll(task.Tags(), query.Tags) {
			continue
		}
		summaries = append(summaries, TaskSummary{
			Task:                      cloneTask(task),
			DependencyCount:           len(s.dependencies[task.ID()]),
			UnfinishedDependencyCount: s.unfinishedDependencyCountLocked(task.ID()),
			SubtaskCount:              s.subtaskCountLocked(task.ID()),
		})
	}
	sort.Slice(summaries, func(left int, right int) bool {
		if summaries[left].Task.Priority() != summaries[right].Task.Priority() {
			return summaries[left].Task.Priority() < summaries[right].Task.Priority()
		}
		return summaries[left].Task.ID() < summaries[right].Task.ID()
	})
	return summaries
}

func (s *memoryStore) dependenciesLocked(taskID int64) []DependencyInfo {
	var dependencies []DependencyInfo
	for depID := range s.dependencies[taskID] {
		dep := s.tasks[depID]
		if dep != nil {
			dependencies = append(dependencies, DependencyInfo{TaskID: dep.ID(), Title: dep.Title(), Status: dep.Status()})
		}
	}
	sort.Slice(dependencies, func(left int, right int) bool {
		return dependencies[left].TaskID < dependencies[right].TaskID
	})
	return dependencies
}

func (s *memoryStore) unfinishedDependencyCountLocked(taskID int64) int {
	count := 0
	for depID := range s.dependencies[taskID] {
		dep := s.tasks[depID]
		if dep != nil && !terminalStatus(dep.Status()) {
			count++
		}
	}
	return count
}

func (s *memoryStore) unblockedLocked(taskID int64) bool {
	return s.unfinishedDependencyCountLocked(taskID) == 0
}

func (s *memoryStore) subtaskCountLocked(taskID int64) int {
	count := 0
	for _, task := range s.tasks {
		parentID := task.ParentID()
		if parentID != nil && *parentID == taskID {
			count++
		}
	}
	return count
}

func (s *memoryStore) appendHistoryLocked(taskID int64, field string, oldValue string, newValue string, agent string, at time.Time) {
	entry := TaskHistoryEntry{
		ID:        s.nextHistoryID,
		TaskID:    taskID,
		Field:     field,
		OldValue:  oldValue,
		NewValue:  newValue,
		ChangedBy: agent,
		ChangedAt: at,
	}
	s.nextHistoryID++
	s.history = append(s.history, entry)
}

func (s *memoryStore) appendChangeLocked(kind string, taskID int64, at time.Time) {
	task := s.tasks[taskID]
	if task == nil {
		return
	}
	event := TaskChangeEvent{
		ID:      s.nextEventID,
		Kind:    kind,
		Changed: at,
		Summary: TaskSummary{
			Task:                      cloneTask(task),
			DependencyCount:           len(s.dependencies[taskID]),
			UnfinishedDependencyCount: s.unfinishedDependencyCountLocked(taskID),
			SubtaskCount:              s.subtaskCountLocked(taskID),
		},
	}
	s.nextEventID++
	s.changes = append(s.changes, event)
}

func (s *memoryStore) dependentTaskIDsLocked(taskID int64) map[int64]bool {
	result := make(map[int64]bool)
	for candidateID, deps := range s.dependencies {
		if deps[taskID] {
			result[candidateID] = true
		}
	}
	return result
}

func (s *memoryStore) historyLocked(taskID int64) []TaskHistoryEntry {
	var entries []TaskHistoryEntry
	for _, entry := range s.history {
		if entry.TaskID == taskID {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(left int, right int) bool {
		return entries[left].ID > entries[right].ID
	})
	return entries
}

func cloneTask(task *Task) *Task {
	clone, err := NewTask(NewTaskParams{
		ID:                        task.ID(),
		ProjectID:                 task.ProjectID(),
		ParentID:                  task.ParentID(),
		Title:                     task.Title(),
		Description:               task.Description(),
		Status:                    task.Status(),
		Priority:                  task.Priority(),
		AssignedTo:                task.AssignedTo(),
		Tags:                      task.Tags(),
		BlockerSummary:            task.BlockerSummary(),
		BlockerReason:             task.BlockerReason(),
		BlockerAttemptedRemedies:  task.BlockerAttemptedRemedies(),
		BlockerSuggestedNextStep:  task.BlockerSuggestedNextStep(),
		BlockerRequiresHumanInput: task.BlockerRequiresHumanInput(),
		CreatedAt:                 task.CreatedAt(),
		UpdatedAt:                 task.UpdatedAt(),
	})
	if err != nil {
		panic(err)
	}
	return clone
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsAll(values []string, required []string) bool {
	for _, item := range required {
		if !contains(values, item) {
			return false
		}
	}
	return true
}

func intString(value int) string {
	return fmt.Sprint(value)
}

func boolString(value bool) string {
	return fmt.Sprint(value)
}

func stringsForHistory(values []string) string {
	return strings.Join(values, ",")
}
