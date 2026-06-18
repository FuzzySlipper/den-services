package runtime

import (
	"context"
	"testing"
	"time"

	"den-services/shared/identity"
)

type memoryRuntimeStore struct {
	t             *testing.T
	instances     map[identity.AgentInstanceID]*RuntimeInstance
	subscriptions map[int64]*ChannelSubscription
	nextID        int64
}

func newMemoryRuntimeStore(t *testing.T) *memoryRuntimeStore {
	t.Helper()
	return &memoryRuntimeStore{
		t:             t,
		instances:     make(map[identity.AgentInstanceID]*RuntimeInstance),
		subscriptions: make(map[int64]*ChannelSubscription),
		nextID:        1,
	}
}

func (s *memoryRuntimeStore) RegisterInstance(_ context.Context, instance *RuntimeInstance) (*RuntimeInstance, error) {
	s.instances[instance.InstanceID()] = instance
	return instance, nil
}

func (s *memoryRuntimeStore) ListInstances(_ context.Context, state *RuntimeState) ([]*RuntimeInstance, error) {
	var instances []*RuntimeInstance
	for _, instance := range s.instances {
		if state == nil || instance.State() == *state {
			instances = append(instances, instance)
		}
	}
	return instances, nil
}

func (s *memoryRuntimeStore) GetInstance(_ context.Context, instanceID identity.AgentInstanceID) (*RuntimeInstance, error) {
	instance, ok := s.instances[instanceID]
	if !ok {
		return nil, notFound(ErrInstanceNotFound, instanceID.String())
	}
	return instance, nil
}

func (s *memoryRuntimeStore) Heartbeat(_ context.Context, instanceID identity.AgentInstanceID, at time.Time) (*RuntimeInstance, error) {
	instance, err := s.GetInstance(context.Background(), instanceID)
	if err != nil {
		return nil, err
	}
	instance.applyHeartbeat(at)
	return instance, nil
}

func (s *memoryRuntimeStore) CreateSubscription(_ context.Context, subscription *ChannelSubscription) (*ChannelSubscription, error) {
	subscription.subscriptionID = s.nextID
	s.nextID++
	s.subscriptions[subscription.SubscriptionID()] = subscription
	return subscription, nil
}

func (s *memoryRuntimeStore) GetSubscription(_ context.Context, subscriptionID int64) (*ChannelSubscription, error) {
	subscription, ok := s.subscriptions[subscriptionID]
	if !ok {
		return nil, notFound(ErrSubscriptionNotFound, "missing")
	}
	return subscription, nil
}

func (s *memoryRuntimeStore) MarkSubscriptionPolled(_ context.Context, subscriptionID int64, cursor int64, at time.Time) (*ChannelSubscription, error) {
	subscription, err := s.GetSubscription(context.Background(), subscriptionID)
	if err != nil {
		return nil, err
	}
	if cursor > subscription.cursorPosition {
		subscription.cursorPosition = cursor
	}
	polledAt := at.UTC()
	subscription.lastPolledAt = &polledAt
	return subscription, nil
}

func (s *memoryRuntimeStore) MarkStale(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *memoryRuntimeStore) MarkDead(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
