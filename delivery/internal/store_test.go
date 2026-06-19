package delivery

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"den-services/shared/identity"
)

type memoryIntentStore struct {
	mu      sync.Mutex
	nextID  int64
	intents map[int64]*DeliveryIntent
}

func newMemoryIntentStore(t *testing.T) *memoryIntentStore {
	t.Helper()
	return &memoryIntentStore{
		nextID:  1,
		intents: make(map[int64]*DeliveryIntent),
	}
}

func (s *memoryIntentStore) mustCreateIntent(t *testing.T, state IntentState) *DeliveryIntent {
	t.Helper()
	return s.mustCreateIntentFor(t, testIdentity(), state)
}

func (s *memoryIntentStore) mustCreateIntentFor(t *testing.T, target identity.AgentIdentity, state IntentState) *DeliveryIntent {
	t.Helper()
	key := fmt.Sprintf("op:1:planner:nonce-%d", s.nextID)
	intent, err := NewDeliveryIntent(target, key, 5*time.Minute, nil, nil, fixedClock()())
	if err != nil {
		t.Fatalf("NewDeliveryIntent() error = %v", err)
	}
	intent.state = state
	created, err := s.Create(context.Background(), intent)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return created
}

func (s *memoryIntentStore) Create(_ context.Context, intent *DeliveryIntent) (*DeliveryIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.intents {
		if existing.IdempotencyKey() == intent.IdempotencyKey() {
			return existing, nil
		}
	}
	intent.id = s.nextID
	s.nextID++
	s.intents[intent.ID()] = intent
	return intent, nil
}

func (s *memoryIntentStore) GetByID(_ context.Context, id int64) (*DeliveryIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.intents[id]
	if !ok {
		return nil, notFound(id)
	}
	return intent, nil
}

func (s *memoryIntentStore) List(_ context.Context, state *IntentState) ([]*DeliveryIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var intents []*DeliveryIntent
	for _, intent := range s.intents {
		if state == nil || intent.State() == *state {
			intents = append(intents, intent)
		}
	}
	return intents, nil
}

func (s *memoryIntentStore) ClaimPending(_ context.Context, id int64, token string, claimedBy identity.AgentIdentity, at time.Time) (*DeliveryIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.intents[id]
	if !ok {
		return nil, notFound(id)
	}
	if intent.State() != IntentStatePending {
		switch intent.State() {
		case IntentStateCompleted, IntentStateFailed, IntentStateCancelled, IntentStateDisplayOnly:
			return nil, conflict(ErrIntentAlreadyCompleted)
		case IntentStateExpired:
			return nil, conflict(ErrIntentExpired)
		default:
			return nil, conflict(ErrIntentAlreadyClaimed)
		}
	}
	if err := intent.applyClaim(token, claimedBy, at); err != nil {
		return nil, conflict(err)
	}
	return intent, nil
}

func (s *memoryIntentStore) ExpireIfPending(_ context.Context, id int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.intents[id]
	if ok && intent.State() == IntentStatePending {
		intent.state = IntentStateExpired
		completedAt := at.UTC()
		intent.completedAt = &completedAt
	}
	return nil
}

func (s *memoryIntentStore) ReportEvent(_ context.Context, id int64, claimToken string, eventType string, _ []byte, at time.Time) (*DeliveryIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.intents[id]
	if !ok {
		return nil, notFound(id)
	}
	if err := intent.canReport(eventType, claimToken); err != nil {
		return nil, conflict(err)
	}
	if eventType == "running" {
		intent.state = IntentStateRunning
		return intent, nil
	}
	if eventType == "completed" {
		intent.state = IntentStateCompleted
	} else {
		intent.state = IntentStateFailed
	}
	completedAt := at.UTC()
	intent.completedAt = &completedAt
	return intent, nil
}

func (s *memoryIntentStore) ExpirePendingBefore(_ context.Context, before time.Time, at time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int64
	for _, intent := range s.intents {
		if intent.State() == IntentStatePending && !intent.CreatedAt().After(before) {
			intent.state = IntentStateExpired
			completedAt := at.UTC()
			intent.completedAt = &completedAt
			count++
		}
	}
	return count, nil
}

func (s *memoryIntentStore) ListRunningBefore(_ context.Context, before time.Time) ([]*DeliveryIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var intents []*DeliveryIntent
	for _, intent := range s.intents {
		if intent.State() == IntentStateRunning && intent.ClaimedAt() != nil && !intent.ClaimedAt().After(before) {
			intents = append(intents, intent)
		}
	}
	return intents, nil
}

func (s *memoryIntentStore) FailRunning(_ context.Context, id int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.intents[id]
	if ok && intent.State() == IntentStateRunning {
		intent.state = IntentStateFailed
		completedAt := at.UTC()
		intent.completedAt = &completedAt
	}
	return nil
}
