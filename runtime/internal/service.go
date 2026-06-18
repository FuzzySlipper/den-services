package runtime

import (
	"context"
	"time"

	"den-services/shared/identity"
)

type RuntimeStore interface {
	RegisterInstance(ctx context.Context, instance *RuntimeInstance) (*RuntimeInstance, error)
	ListInstances(ctx context.Context, state *RuntimeState) ([]*RuntimeInstance, error)
	GetInstance(ctx context.Context, instanceID identity.AgentInstanceID) (*RuntimeInstance, error)
	Heartbeat(ctx context.Context, instanceID identity.AgentInstanceID, at time.Time) (*RuntimeInstance, error)
	CreateSubscription(ctx context.Context, subscription *ChannelSubscription) (*ChannelSubscription, error)
	GetSubscription(ctx context.Context, subscriptionID int64) (*ChannelSubscription, error)
	MarkSubscriptionPolled(ctx context.Context, subscriptionID int64, cursor int64, at time.Time) (*ChannelSubscription, error)
	MarkStale(ctx context.Context, before time.Time) (int64, error)
	MarkDead(ctx context.Context, before time.Time) (int64, error)
}

type RuntimeService struct {
	store          RuntimeStore
	clock          func() time.Time
	staleThreshold time.Duration
	deadThreshold  time.Duration
}

func NewRuntimeService(store RuntimeStore, clock func() time.Time, staleThreshold time.Duration, deadThreshold time.Duration) *RuntimeService {
	return &RuntimeService{
		store:          store,
		clock:          clock,
		staleThreshold: staleThreshold,
		deadThreshold:  deadThreshold,
	}
}

func (s *RuntimeService) Register(ctx context.Context, req RegisterInstanceRequest) (*RuntimeInstance, error) {
	if err := req.Validate(); err != nil {
		return nil, badRequest(err)
	}
	instance, err := NewRuntimeInstance(req.InstanceID, req.ProfileIdentity, req.Host, req.PID, s.clock())
	if err != nil {
		return nil, badRequest(err)
	}
	return s.store.RegisterInstance(ctx, instance)
}

func (s *RuntimeService) List(ctx context.Context, state *RuntimeState) ([]*RuntimeInstance, error) {
	if state != nil && !state.IsValid() {
		return nil, badRequest(ErrInvalidRuntimeState)
	}
	return s.store.ListInstances(ctx, state)
}

func (s *RuntimeService) Get(ctx context.Context, instanceID identity.AgentInstanceID) (*RuntimeInstance, error) {
	if !instanceID.IsValid() {
		return nil, badRequest(ErrInvalidRuntimeInstance)
	}
	instance, err := s.store.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return instance, nil
}

func (s *RuntimeService) Heartbeat(ctx context.Context, instanceID identity.AgentInstanceID) (*RuntimeInstance, error) {
	if !instanceID.IsValid() {
		return nil, badRequest(ErrInvalidRuntimeInstance)
	}
	return s.store.Heartbeat(ctx, instanceID, s.clock())
}

func (s *RuntimeService) CreateSubscription(ctx context.Context, req CreateSubscriptionRequest) (*ChannelSubscription, error) {
	if err := req.Validate(); err != nil {
		return nil, badRequest(err)
	}
	if _, err := s.store.GetInstance(ctx, req.RuntimeInstanceID); err != nil {
		return nil, err
	}
	subscription, err := NewChannelSubscription(req.RuntimeInstanceID, req.ChannelID, req.WakePolicyOverride, s.clock())
	if err != nil {
		return nil, badRequest(err)
	}
	return s.store.CreateSubscription(ctx, subscription)
}

func (s *RuntimeService) Stream(ctx context.Context, subscriptionID int64, after int64) (*ChannelSubscription, error) {
	if subscriptionID <= 0 || after < 0 {
		return nil, badRequest(ErrInvalidSubscription)
	}
	if _, err := s.store.GetSubscription(ctx, subscriptionID); err != nil {
		return nil, err
	}
	return s.store.MarkSubscriptionPolled(ctx, subscriptionID, after, s.clock())
}

func (s *RuntimeService) Sweep(ctx context.Context) (int64, int64, error) {
	now := s.clock()
	staleCount, err := s.store.MarkStale(ctx, now.Add(-s.staleThreshold))
	if err != nil {
		return 0, 0, err
	}
	deadCount, err := s.store.MarkDead(ctx, now.Add(-s.deadThreshold))
	if err != nil {
		return 0, 0, err
	}
	return staleCount, deadCount, nil
}
