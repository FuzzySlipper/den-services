package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ScopeStore interface {
	Ping(ctx context.Context) error
	CreateScope(ctx context.Context, scope *Scope) (*Scope, error)
	GetScope(ctx context.Context, id string) (*Scope, error)
	ListScopes(ctx context.Context, query ListScopesQuery) ([]*Scope, error)
	UpdateScope(ctx context.Context, id string, patch ScopePatch, updatedAt time.Time) (*Scope, error)
	UpdateVisibility(ctx context.Context, id string, visibility string, updatedAt time.Time) (*Scope, error)
}

type ListScopesQuery struct {
	Kind            string
	IncludeHidden   bool
	IncludeArchived bool
}

type ScopePatch struct {
	Name         *string
	RootPath     *string
	Description  *string
	Owner        *string
	SettingsJSON []byte
	HasSettings  bool
}

type Service struct {
	store ScopeStore
	clock func() time.Time
}

func NewService(store ScopeStore, clock func() time.Time) *Service {
	return &Service{store: store, clock: clock}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}

func (s *Service) CreateProject(ctx context.Context, req CreateProjectRequest) (*Scope, error) {
	scope, err := NewScope(NewScopeParams{
		ID:          req.ID,
		Name:        req.Name,
		Kind:        KindProject,
		Visibility:  VisibilityNormal,
		RootPath:    req.RootPath,
		Description: req.Description,
		CreatedAt:   s.clock().UTC(),
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.CreateScope(ctx, scope)
}

func (s *Service) CreateSpace(ctx context.Context, req CreateSpaceRequest) (*Scope, error) {
	settings, err := normalizeSettings(req.SettingsJSON)
	if err != nil {
		return nil, validationFailed(err)
	}
	now := s.clock().UTC()
	scope, err := NewScope(NewScopeParams{
		ID:           req.ID,
		Name:         req.Name,
		Kind:         req.Kind,
		Visibility:   req.Visibility,
		Owner:        req.Owner,
		RootPath:     req.RootPath,
		Description:  req.Description,
		SettingsJSON: settings,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.CreateScope(ctx, scope)
}

func (s *Service) GetScope(ctx context.Context, id string) (*Scope, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, validationFailed(ErrMissingID)
	}
	return s.store.GetScope(ctx, id)
}

func (s *Service) ListProjects(ctx context.Context, includeHidden bool, includeArchived bool) ([]*Scope, error) {
	return s.store.ListScopes(ctx, ListScopesQuery{
		Kind:            KindProject,
		IncludeHidden:   includeHidden,
		IncludeArchived: includeArchived,
	})
}

func (s *Service) ListSpaces(ctx context.Context, kind string, includeHidden bool, includeArchived bool) ([]*Scope, error) {
	kind = strings.TrimSpace(kind)
	if kind != "" && !validKind(kind) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidKind, kind))
	}
	return s.store.ListScopes(ctx, ListScopesQuery{
		Kind:            kind,
		IncludeHidden:   includeHidden,
		IncludeArchived: includeArchived,
	})
}

func (s *Service) UpdateProject(ctx context.Context, id string, req UpdateProjectRequest) (*Scope, error) {
	if strings.TrimSpace(id) == "" {
		return nil, validationFailed(ErrMissingID)
	}
	if !req.HasChanges() {
		return nil, validationFailed(ErrEmptyPatch)
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return nil, validationFailed(ErrMissingName)
	}
	settings, hasSettings, err := normalizePatchSettings(req.SettingsJSON)
	if err != nil {
		return nil, validationFailed(err)
	}
	patch := ScopePatch{
		Name:         trimStringPointer(req.Name),
		RootPath:     trimStringPointer(req.RootPath),
		Description:  trimStringPointer(req.Description),
		Owner:        trimStringPointer(req.Owner),
		SettingsJSON: settings,
		HasSettings:  hasSettings,
	}
	return s.store.UpdateScope(ctx, strings.TrimSpace(id), patch, s.clock().UTC())
}

func (s *Service) UpdateVisibility(ctx context.Context, id string, visibility string) (*Scope, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, validationFailed(ErrMissingID)
	}
	visibility = strings.TrimSpace(visibility)
	if !validVisibility(visibility) {
		return nil, validationFailed(fmt.Errorf("%w: %s", ErrInvalidVisibility, visibility))
	}
	return s.store.UpdateVisibility(ctx, id, visibility, s.clock().UTC())
}

func (s *Service) ArchiveSpace(ctx context.Context, id string) (*Scope, error) {
	return s.UpdateVisibility(ctx, id, VisibilityArchived)
}

func (s *Service) AssertWritable(ctx context.Context, id string, allowArchived bool) (*Scope, error) {
	scope, err := s.GetScope(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope.Visibility() == VisibilityArchived && !allowArchived {
		return nil, conflict(fmt.Errorf("%w: %s", ErrArchivedScopeWrite, scope.ID()), "archived_scope")
	}
	return scope, nil
}

func normalizeSettings(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("%w: settings_json must be valid json", ErrInvalidScope)
	}
	return append([]byte(nil), raw...), nil
}

func normalizePatchSettings(raw json.RawMessage) ([]byte, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	settings, err := normalizeSettings(raw)
	if err != nil {
		return nil, false, err
	}
	return settings, true, nil
}

func trimStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
