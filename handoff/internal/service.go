package handoff

import (
	"context"
	"time"
)

type HandoffStore interface {
	Ping(ctx context.Context) error
	Set(ctx context.Context, handoff *Handoff) (*Handoff, error)
	Get(ctx context.Context, label string) (*Handoff, error)
}

type Service struct {
	store     HandoffStore
	clock     func() time.Time
	updatedBy string
}

func NewService(store HandoffStore, clock func() time.Time) *Service {
	return &Service{store: store, clock: clock, updatedBy: "handoff-service"}
}

func (s *Service) CheckStore(ctx context.Context) error { return s.store.Ping(ctx) }

func (s *Service) Set(ctx context.Context, request SetHandoffRequest) (*Handoff, error) {
	now := s.clock().UTC()
	value, err := NewHandoff(NewHandoffParams{
		Label: request.Label, BodyMarkdown: request.BodyMarkdown, UpdatedBy: s.updatedBy,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.Set(ctx, value)
}

func (s *Service) Get(ctx context.Context, rawLabel string) (*Handoff, error) {
	label, err := normalizeLabel(rawLabel)
	if err != nil {
		return nil, validationFailed(err)
	}
	return s.store.Get(ctx, label)
}
