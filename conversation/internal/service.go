package conversation

import (
	"context"
	"time"
)

type ConversationStore interface {
	Ping(ctx context.Context) error
}

type Service struct {
	store ConversationStore
	clock func() time.Time
}

func NewService(store ConversationStore, clock func() time.Time) *Service {
	return &Service{
		store: store,
		clock: clock,
	}
}

func (s *Service) CheckStore(ctx context.Context) error {
	return s.store.Ping(ctx)
}
