package delivery

import (
	"context"
	"time"

	"den-services/shared/identity"
)

type IntentStore interface {
	Create(ctx context.Context, intent *DeliveryIntent) (*DeliveryIntent, error)
	GetByID(ctx context.Context, id int64) (*DeliveryIntent, error)
	List(ctx context.Context, state *IntentState) ([]*DeliveryIntent, error)
	ClaimPending(ctx context.Context, id int64, token string, claimedBy identity.AgentIdentity, at time.Time) (*DeliveryIntent, error)
	ExpireIfPending(ctx context.Context, id int64, at time.Time) error
	ReportEvent(ctx context.Context, id int64, claimToken string, eventType string, payload []byte, at time.Time) (*DeliveryIntent, error)
	ExpirePendingBefore(ctx context.Context, before time.Time, at time.Time) (int64, error)
	ListRunningBefore(ctx context.Context, before time.Time) ([]*DeliveryIntent, error)
	FailRunning(ctx context.Context, id int64, at time.Time) error
}

type RuntimeChecker interface {
	IsAlive(ctx context.Context, instanceID identity.AgentInstanceID) (bool, error)
}

type IntentService struct {
	store      IntentStore
	runtime    RuntimeChecker
	clock      func() time.Time
	defaultTTL time.Duration
	maxTTL     time.Duration
	pendingTTL time.Duration
	runningTTL time.Duration
}

func NewIntentService(store IntentStore, runtime RuntimeChecker, clock func() time.Time, defaultTTL time.Duration, maxTTL time.Duration, pendingTTL time.Duration, runningTTL time.Duration) *IntentService {
	return &IntentService{
		store:      store,
		runtime:    runtime,
		clock:      clock,
		defaultTTL: defaultTTL,
		maxTTL:     maxTTL,
		pendingTTL: pendingTTL,
		runningTTL: runningTTL,
	}
}

func (s *IntentService) Create(ctx context.Context, req CreateIntentRequest) (*DeliveryIntent, error) {
	if err := req.Validate(); err != nil {
		return nil, badRequest(err)
	}
	ttl := s.defaultTTL
	if req.TTLSeconds != nil {
		ttl = time.Duration(*req.TTLSeconds) * time.Second
	}
	if ttl > s.maxTTL {
		return nil, badRequest(ErrInvalidIntent)
	}
	intent, err := NewDeliveryIntent(req.TargetIdentity, req.IdempotencyKey, ttl, req.SourceRef, req.ChannelMessageID, s.clock())
	if err != nil {
		return nil, badRequest(err)
	}
	return s.store.Create(ctx, intent)
}

func (s *IntentService) Claim(ctx context.Context, id int64, req ClaimRequest) (*DeliveryIntent, error) {
	if err := req.Validate(); err != nil {
		return nil, badRequest(err)
	}
	now := s.clock()
	intent, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !intent.TargetIdentity().Equal(req.ClaimedBy) {
		return nil, conflict(ErrIntentTargetMismatch)
	}
	if intent.State() == IntentStatePending && !now.Before(intent.ExpiresAt()) {
		_ = s.store.ExpireIfPending(ctx, id, now)
		return nil, conflict(ErrIntentExpired)
	}
	alive, err := s.runtime.IsAlive(ctx, req.ClaimedBy.InstanceID)
	if err != nil {
		return nil, err
	}
	if !alive {
		_ = s.store.ExpireIfPending(ctx, id, now)
		return nil, conflict(ErrRuntimeNotAlive)
	}
	return s.store.ClaimPending(ctx, id, req.ClaimToken, req.ClaimedBy, now)
}

func (s *IntentService) ReportEvent(ctx context.Context, id int64, req LifecycleEventRequest) (*DeliveryIntent, error) {
	if err := req.Validate(); err != nil {
		return nil, badRequest(err)
	}
	payload := []byte(req.Payload)
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	return s.store.ReportEvent(ctx, id, req.ClaimToken, req.EventType, payload, s.clock())
}

func (s *IntentService) Get(ctx context.Context, id int64) (*DeliveryIntent, error) {
	if id <= 0 {
		return nil, badRequest(ErrInvalidIntent)
	}
	return s.store.GetByID(ctx, id)
}

func (s *IntentService) List(ctx context.Context, state *IntentState) ([]*DeliveryIntent, error) {
	if state != nil && !state.IsValid() {
		return nil, badRequest(ErrInvalidIntentState)
	}
	return s.store.List(ctx, state)
}

func (s *IntentService) Reap(ctx context.Context) (int64, int64, error) {
	now := s.clock()
	expired, err := s.store.ExpirePendingBefore(ctx, now.Add(-s.pendingTTL), now)
	if err != nil {
		return 0, 0, err
	}
	running, err := s.store.ListRunningBefore(ctx, now.Add(-s.runningTTL))
	if err != nil {
		return 0, 0, err
	}
	var failed int64
	for _, intent := range running {
		claimedBy := intent.ClaimedBy()
		if claimedBy == nil {
			if err := s.store.FailRunning(ctx, intent.ID(), now); err != nil {
				return expired, failed, err
			}
			failed++
			continue
		}
		alive, err := s.runtime.IsAlive(ctx, claimedBy.InstanceID)
		if err != nil {
			return expired, failed, err
		}
		if alive {
			continue
		}
		if err := s.store.FailRunning(ctx, intent.ID(), now); err != nil {
			return expired, failed, err
		}
		failed++
	}
	return expired, failed, nil
}
