package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ScopeValidator interface {
	AssertWritable(ctx context.Context, projectID string) error
}

type TaskStore interface {
	Ping(ctx context.Context) error
	CreateTask(ctx context.Context, task *Task, dependsOn []int64) (*Task, error)
	GetTask(ctx context.Context, id int64) (*Task, error)
	GetDetail(ctx context.Context, id int64) (TaskDetail, error)
	ListTasks(ctx context.Context, query ListTasksQuery) ([]TaskSummary, error)
	UpdateTask(ctx context.Context, id int64, patch TaskPatch, agent string, updatedAt time.Time) (*Task, error)
	AddDependency(ctx context.Context, taskID int64, dependsOn int64) error
	RemoveDependency(ctx context.Context, taskID int64, dependsOn int64) error
	NextTask(ctx context.Context, projectID string, assignedTo string) (*Task, error)
	History(ctx context.Context, taskID int64) ([]TaskHistoryEntry, error)
	ListTaskChanges(ctx context.Context, query TaskChangeQuery) ([]TaskChangeEvent, error)
}

type ListTasksQuery struct {
	ProjectID   string
	Statuses    []string
	AssignedTo  string
	Tags        []string
	MaxPriority *int
	ParentID    *int64
	IncludeAll  bool
}

type TaskChangeQuery struct {
	ProjectID string
	AfterID   int64
	Limit     int
}

type TaskPatch struct {
	Title                     *string
	Description               *string
	Status                    *string
	Priority                  *int
	AssignedTo                *string
	Tags                      []string
	HasTags                   bool
	ParentID                  *int64
	HasParent                 bool
	BlockerSummary            *string
	BlockerReason             *string
	BlockerAttemptedRemedies  *string
	BlockerSuggestedNextStep  *string
	BlockerRequiresHumanInput *bool
}

type Service struct {
	store    TaskStore
	projects ScopeValidator
	clock    func() time.Time
}

func NewService(store TaskStore, projects ScopeValidator, clock func() time.Time) *Service {
	return &Service{store: store, projects: projects, clock: clock}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) CreateTask(ctx context.Context, projectID string, req CreateTaskRequest) (*Task, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	if err := s.projects.AssertWritable(ctx, projectID); err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	task, err := NewTask(NewTaskParams{
		ProjectID:   projectID,
		ParentID:    req.ParentID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    req.Priority,
		Tags:        req.Tags,
		AssignedTo:  req.AssignedTo,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.CreateTask(ctx, task, req.DependsOn)
}

func (s *Service) GetTask(ctx context.Context, taskID int64) (TaskDetail, error) {
	return s.store.GetDetail(ctx, taskID)
}

func (s *Service) ListTasks(ctx context.Context, projectID string, query ListTasksQuery) ([]TaskSummary, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	for _, status := range query.Statuses {
		if !validStatus(status) {
			return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidStatus, status))
		}
	}
	if query.MaxPriority != nil && (*query.MaxPriority < 1 || *query.MaxPriority > 5) {
		return nil, validationFailed(ErrInvalidPriority)
	}
	query.ProjectID = projectID
	return s.store.ListTasks(ctx, query)
}

func (s *Service) UpdateTask(ctx context.Context, taskID int64, req UpdateTaskRequest) (*Task, error) {
	if strings.TrimSpace(req.Agent) == "" {
		return nil, validationFailed(ErrMissingAgent)
	}
	if !req.HasChanges() {
		return nil, validationFailed(ErrEmptyPatch)
	}
	current, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := s.projects.AssertWritable(ctx, current.ProjectID()); err != nil {
		return nil, err
	}
	patch, err := buildPatch(current, req)
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.UpdateTask(ctx, taskID, patch, strings.TrimSpace(req.Agent), s.clock().UTC())
}

func (s *Service) AddDependency(ctx context.Context, taskID int64, dependsOn int64) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if err := s.projects.AssertWritable(ctx, task.ProjectID()); err != nil {
		return err
	}
	return s.store.AddDependency(ctx, taskID, dependsOn)
}

func (s *Service) RemoveDependency(ctx context.Context, taskID int64, dependsOn int64) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if err := s.projects.AssertWritable(ctx, task.ProjectID()); err != nil {
		return err
	}
	return s.store.RemoveDependency(ctx, taskID, dependsOn)
}

func (s *Service) NextTask(ctx context.Context, projectID string, assignedTo string) (*Task, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	return s.store.NextTask(ctx, projectID, strings.TrimSpace(assignedTo))
}

func (s *Service) History(ctx context.Context, taskID int64) ([]TaskHistoryEntry, error) {
	return s.store.History(ctx, taskID)
}

func (s *Service) ListTaskChanges(ctx context.Context, projectID string, afterID int64, limit int) ([]TaskChangeEvent, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, validationFailed(ErrMissingProjectID)
	}
	if afterID < 0 {
		return nil, validationFailed(fmt.Errorf("after must be non-negative"))
	}
	if limit <= 0 {
		return nil, validationFailed(fmt.Errorf("limit must be positive"))
	}
	return s.store.ListTaskChanges(ctx, TaskChangeQuery{ProjectID: projectID, AfterID: afterID, Limit: limit})
}

func buildPatch(current *Task, req UpdateTaskRequest) (TaskPatch, error) {
	patch := TaskPatch{
		Title:                     trimStringPointer(req.Title),
		Description:               trimStringPointer(req.Description),
		Priority:                  req.Priority,
		AssignedTo:                trimStringPointer(req.AssignedTo),
		Tags:                      normalizeTags(req.Tags),
		HasTags:                   req.Tags != nil,
		ParentID:                  req.ParentID,
		HasParent:                 req.ParentID != nil || req.ClearParent,
		BlockerSummary:            trimStringPointer(req.BlockerSummary),
		BlockerReason:             trimStringPointer(req.BlockerReason),
		BlockerAttemptedRemedies:  trimStringPointer(req.BlockerAttemptedRemedies),
		BlockerSuggestedNextStep:  trimStringPointer(req.BlockerSuggestedNextStep),
		BlockerRequiresHumanInput: req.BlockerRequiresHumanInput,
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !validStatus(status) {
			return TaskPatch{}, fmt.Errorf("%w: %s", ErrInvalidStatus, status)
		}
		patch.Status = &status
		if status == StatusBlocked && current.Status() != StatusBlocked {
			if patch.BlockerSummary == nil || *patch.BlockerSummary == "" || patch.BlockerReason == nil || *patch.BlockerReason == "" {
				return TaskPatch{}, ErrBlockedContextMissing
			}
		}
	}
	if patch.Title != nil && *patch.Title == "" {
		return TaskPatch{}, ErrMissingTitle
	}
	if patch.Priority != nil && (*patch.Priority < 1 || *patch.Priority > 5) {
		return TaskPatch{}, ErrInvalidPriority
	}
	if req.ClearParent {
		patch.ParentID = nil
	}
	return patch, nil
}

func trimStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
