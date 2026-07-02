package projects

import (
	"context"
	"sort"
	"sync"
	"time"
)

type memoryStore struct {
	mu     sync.Mutex
	scopes map[string]*Scope
}

func newMemoryStore() *memoryStore {
	return &memoryStore{scopes: make(map[string]*Scope)}
}

func (s *memoryStore) Ping(context.Context) error {
	return nil
}

func (s *memoryStore) CreateScope(_ context.Context, scope *Scope) (*Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.scopes[scope.ID()]; exists {
		return nil, conflict(ErrDuplicateScope, "scope_already_exists")
	}
	clone := cloneScope(scope)
	s.scopes[clone.ID()] = clone
	return cloneScope(clone), nil
}

func (s *memoryStore) GetScope(_ context.Context, id string) (*Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scope, ok := s.scopes[id]
	if !ok {
		return nil, notFound(id)
	}
	return cloneScope(scope), nil
}

func (s *memoryStore) ListScopes(_ context.Context, query ListScopesQuery) ([]*Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var scopes []*Scope
	for _, scope := range s.scopes {
		if query.Kind != "" && scope.Kind() != query.Kind {
			continue
		}
		if !query.IncludeHidden && scope.Visibility() == VisibilityHidden {
			continue
		}
		if !query.IncludeArchived && scope.Visibility() == VisibilityArchived {
			continue
		}
		scopes = append(scopes, cloneScope(scope))
	}
	sort.Slice(scopes, func(left int, right int) bool {
		return scopes[left].ID() < scopes[right].ID()
	})
	return scopes, nil
}

func (s *memoryStore) UpdateScope(_ context.Context, id string, patch ScopePatch, updatedAt time.Time) (*Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scope, ok := s.scopes[id]
	if !ok {
		return nil, notFound(id)
	}
	params := NewScopeParams{
		ID:           scope.ID(),
		Name:         scope.Name(),
		Kind:         scope.Kind(),
		Visibility:   scope.Visibility(),
		Owner:        scope.Owner(),
		RootPath:     scope.RootPath(),
		Description:  scope.Description(),
		SettingsJSON: scope.SettingsJSON(),
		CreatedAt:    scope.CreatedAt(),
		UpdatedAt:    updatedAt,
	}
	if patch.Name != nil {
		params.Name = *patch.Name
	}
	if patch.RootPath != nil {
		params.RootPath = *patch.RootPath
	}
	if patch.Description != nil {
		params.Description = *patch.Description
	}
	if patch.Owner != nil {
		params.Owner = *patch.Owner
	}
	if patch.HasSettings {
		params.SettingsJSON = patch.SettingsJSON
	}
	updated, err := NewScope(params)
	if err != nil {
		return nil, err
	}
	s.scopes[id] = updated
	return cloneScope(updated), nil
}

func (s *memoryStore) UpdateVisibility(_ context.Context, id string, visibility string, updatedAt time.Time) (*Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scope, ok := s.scopes[id]
	if !ok {
		return nil, notFound(id)
	}
	updated, err := NewScope(NewScopeParams{
		ID:           scope.ID(),
		Name:         scope.Name(),
		Kind:         scope.Kind(),
		Visibility:   visibility,
		Owner:        scope.Owner(),
		RootPath:     scope.RootPath(),
		Description:  scope.Description(),
		SettingsJSON: scope.SettingsJSON(),
		CreatedAt:    scope.CreatedAt(),
		UpdatedAt:    updatedAt,
	})
	if err != nil {
		return nil, err
	}
	s.scopes[id] = updated
	return cloneScope(updated), nil
}

func (s *memoryStore) DeleteScope(_ context.Context, id string) (*Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scope, ok := s.scopes[id]
	if !ok {
		return nil, notFound(id)
	}
	delete(s.scopes, id)
	return cloneScope(scope), nil
}

func cloneScope(scope *Scope) *Scope {
	clone, err := NewScope(NewScopeParams{
		ID:           scope.ID(),
		Name:         scope.Name(),
		Kind:         scope.Kind(),
		Visibility:   scope.Visibility(),
		Owner:        scope.Owner(),
		RootPath:     scope.RootPath(),
		Description:  scope.Description(),
		SettingsJSON: scope.SettingsJSON(),
		CreatedAt:    scope.CreatedAt(),
		UpdatedAt:    scope.UpdatedAt(),
	})
	if err != nil {
		panic(err)
	}
	return clone
}
