package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"den-services/shared/identity"
)

func TestHeartbeatTimeoutTransitions(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	lastHeartbeat := now.Add(-91 * time.Second)
	if got := stateForHeartbeat(&lastHeartbeat, RuntimeStateActive, now, 90*time.Second, 300*time.Second); got != RuntimeStateStale {
		t.Fatalf("stateForHeartbeat stale = %s, want %s", got, RuntimeStateStale)
	}
	lastHeartbeat = now.Add(-301 * time.Second)
	if got := stateForHeartbeat(&lastHeartbeat, RuntimeStateActive, now, 90*time.Second, 300*time.Second); got != RuntimeStateDead {
		t.Fatalf("stateForHeartbeat dead = %s, want %s", got, RuntimeStateDead)
	}
}

func TestLeftMembershipDoesNotAffectRuntimeState(t *testing.T) {
	store := newMemoryRuntimeStore(t)
	service := NewRuntimeService(store, fixedClock(), 90*time.Second, 300*time.Second)

	instance, err := service.Register(context.Background(), RegisterInstanceRequest{
		InstanceID:      identity.AgentInstanceID("planner@den-srv"),
		ProfileIdentity: identity.ProfileIdentity("planner"),
		Host:            "den-srv",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if instance.State() != RuntimeStateStarting {
		t.Fatalf("state = %s, want starting", instance.State())
	}

	heartbeat, err := service.Heartbeat(context.Background(), identity.AgentInstanceID("planner@den-srv"))
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if heartbeat.State() != RuntimeStateActive {
		t.Fatalf("heartbeat state = %s, want active", heartbeat.State())
	}
}

func TestRuntimeReconnectUsesNewInstanceID(t *testing.T) {
	store := newMemoryRuntimeStore(t)
	service := NewRuntimeService(store, fixedClock(), 90*time.Second, 300*time.Second)

	if _, err := service.Register(context.Background(), RegisterInstanceRequest{InstanceID: "planner@old", ProfileIdentity: "planner", Host: "den-srv"}); err != nil {
		t.Fatalf("Register old error = %v", err)
	}
	if _, err := service.Register(context.Background(), RegisterInstanceRequest{InstanceID: "planner@new", ProfileIdentity: "planner", Host: "den-srv"}); err != nil {
		t.Fatalf("Register new error = %v", err)
	}

	if _, err := service.Get(context.Background(), identity.AgentInstanceID("planner@new")); err != nil {
		t.Fatalf("Get new error = %v", err)
	}
	if _, err := service.Get(context.Background(), identity.AgentInstanceID("planner@missing")); !errors.Is(err, ErrInstanceNotFound) {
		t.Fatalf("Get missing error = %v, want %v", err, ErrInstanceNotFound)
	}
}

func TestCursorReplayCannotMoveSubscriptionBackward(t *testing.T) {
	store := newMemoryRuntimeStore(t)
	service := NewRuntimeService(store, fixedClock(), 90*time.Second, 300*time.Second)

	if _, err := service.Register(context.Background(), RegisterInstanceRequest{
		InstanceID:      identity.AgentInstanceID("planner@den-srv"),
		ProfileIdentity: identity.ProfileIdentity("planner"),
		Host:            "den-srv",
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	subscription, err := service.CreateSubscription(context.Background(), CreateSubscriptionRequest{
		RuntimeInstanceID: identity.AgentInstanceID("planner@den-srv"),
		ChannelID:         42,
	})
	if err != nil {
		t.Fatalf("CreateSubscription() error = %v", err)
	}

	if _, err := service.Stream(context.Background(), subscription.SubscriptionID(), 10); err != nil {
		t.Fatalf("Stream(10) error = %v", err)
	}
	replayed, err := service.Stream(context.Background(), subscription.SubscriptionID(), 7)
	if err != nil {
		t.Fatalf("Stream(7) error = %v", err)
	}
	if replayed.CursorPosition() != 10 {
		t.Fatalf("cursor after replay = %d, want 10", replayed.CursorPosition())
	}
	if len(store.instances) != 1 || len(store.subscriptions) != 1 {
		t.Fatalf("store sizes instances=%d subscriptions=%d, want 1/1", len(store.instances), len(store.subscriptions))
	}
}

func fixedClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	}
}
