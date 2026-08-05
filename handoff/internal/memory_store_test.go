package handoff

import (
	"context"
	"sync"
)

type memoryStore struct {
	mu      sync.Mutex
	current map[string]*Handoff
	history map[string][]*Handoff
}

func newMemoryStore() *memoryStore {
	return &memoryStore{current: make(map[string]*Handoff), history: make(map[string][]*Handoff)}
}

func (s *memoryStore) Ping(context.Context) error { return nil }

func (s *memoryStore) Set(_ context.Context, value *Handoff) (*Handoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision := int64(1)
	createdAt := value.CreatedAt()
	if previous, ok := s.current[value.Label()]; ok {
		revision = previous.Revision() + 1
		createdAt = previous.CreatedAt()
	}
	stored, err := NewHandoff(NewHandoffParams{
		Label: value.Label(), BodyMarkdown: value.BodyMarkdown(), Revision: revision,
		CreatedAt: createdAt, UpdatedAt: value.UpdatedAt(), UpdatedBy: value.UpdatedBy(),
	})
	if err != nil {
		return nil, err
	}
	s.current[stored.Label()] = stored
	s.history[stored.Label()] = append(s.history[stored.Label()], stored)
	return cloneHandoff(stored), nil
}

func (s *memoryStore) Get(_ context.Context, label string) (*Handoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.current[label]
	if !ok {
		return nil, ErrHandoffNotFound
	}
	return cloneHandoff(value), nil
}

func cloneHandoff(value *Handoff) *Handoff {
	cloned, _ := NewHandoff(NewHandoffParams{
		Label: value.Label(), BodyMarkdown: value.BodyMarkdown(), Revision: value.Revision(),
		CreatedAt: value.CreatedAt(), UpdatedAt: value.UpdatedAt(), UpdatedBy: value.UpdatedBy(),
	})
	return cloned
}
