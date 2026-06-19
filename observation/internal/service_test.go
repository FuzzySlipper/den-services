package observation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"den-services/shared/identity"
)

func TestAppendLifecycleEventAlwaysDisplayOnly(t *testing.T) {
	store := newFakeObservationStore()
	service := NewObservationService(store, fixedClock, 10, 100)
	instanceID := identity.AgentInstanceID("planner@host-1")
	event, err := service.AppendLifecycleEvent(context.Background(), CreateLifecycleEventRequest{
		SourceDomain:      SourceDomainObservation,
		EventType:         "agent_work_lifecycle",
		AgentIdentity:     ptrIdentity(testIdentity()),
		RuntimeInstanceID: &instanceID,
		Payload:           json.RawMessage(`{"status":"seen"}`),
	})
	if err != nil {
		t.Fatalf("AppendLifecycleEvent() error = %v", err)
	}
	if !event.DisplayOnly() {
		t.Fatal("lifecycle event display_only = false, want true")
	}
	if len(store.appended) != 1 {
		t.Fatalf("append count = %d, want 1", len(store.appended))
	}
}

func TestLaneMarksStaleRuntimeBreadcrumbDisplayOnly(t *testing.T) {
	store := newFakeObservationStore()
	instanceID := identity.AgentInstanceID("planner@old")
	store.runtimeEvents = []LaneEvent{{
		EventID:           "runtime:planner@old",
		SourceDomain:      SourceDomainRuntime,
		EventType:         "runtime_stale",
		AgentIdentity:     ptrIdentity(testIdentity()),
		RuntimeInstanceID: &instanceID,
		Payload:           json.RawMessage(`{"state":"stale"}`),
		DisplayOnly:       true,
		CreatedAt:         fixedTime(),
	}}
	service := NewObservationService(store, fixedClock, 10, 100)
	events, err := service.Lane(context.Background(), "")
	if err != nil {
		t.Fatalf("Lane() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if !events[0].DisplayOnly {
		t.Fatal("stale runtime event display_only = false, want true")
	}
	if len(store.appended) != 0 {
		t.Fatalf("lane appended %d events, want 0", len(store.appended))
	}
}

func TestProjectionDoesNotDriveExecution(t *testing.T) {
	store := newFakeObservationStore()
	store.deliveryEvents = []LaneEvent{{
		EventID:      "delivery:42",
		SourceDomain: SourceDomainDelivery,
		EventType:    "intent_pending",
		Payload:      json.RawMessage(`{"intent_id":42}`),
		DisplayOnly:  false,
		CreatedAt:    fixedTime(),
	}}
	service := NewObservationService(store, fixedClock, 10, 100)
	if _, err := service.Lane(context.Background(), "10"); err != nil {
		t.Fatalf("Lane() error = %v", err)
	}
	if len(store.appended) != 0 {
		t.Fatalf("projection append count = %d, want 0", len(store.appended))
	}
	if store.activeWorkCalls != 0 {
		t.Fatalf("projection active-work calls = %d, want 0", store.activeWorkCalls)
	}
}

func TestLaneSortsAndLimitsComposedEvents(t *testing.T) {
	store := newFakeObservationStore()
	store.activityEvents = []LaneEvent{eventAt("observation:1", SourceDomainObservation, fixedTime().Add(-time.Minute))}
	store.deliveryEvents = []LaneEvent{eventAt("delivery:1", SourceDomainDelivery, fixedTime())}
	store.runtimeEvents = []LaneEvent{eventAt("runtime:1", SourceDomainRuntime, fixedTime().Add(-2*time.Minute))}
	service := NewObservationService(store, fixedClock, 2, 100)
	events, err := service.Lane(context.Background(), "")
	if err != nil {
		t.Fatalf("Lane() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].EventID != "delivery:1" || events[1].EventID != "observation:1" {
		t.Fatalf("event order = %s, %s", events[0].EventID, events[1].EventID)
	}
}

func TestAgentOverviewRequiresProjection(t *testing.T) {
	service := NewObservationService(newFakeObservationStore(), fixedClock, 10, 100)
	_, err := service.AgentOverview(context.Background(), "missing")
	if err == nil {
		t.Fatal("AgentOverview() error is nil, want not found")
	}
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.HTTPStatus() != 404 {
		t.Fatalf("AgentOverview() error = %v, want 404 ServiceError", err)
	}
}

type fakeObservationStore struct {
	appended        []*ActivityEvent
	activityEvents  []LaneEvent
	deliveryEvents  []LaneEvent
	runtimeEvents   []LaneEvent
	chatEvents      []LaneEvent
	activeWork      []ActiveWorkItem
	runtimes        []RuntimeProjection
	activeWorkCalls int
}

func newFakeObservationStore() *fakeObservationStore {
	return &fakeObservationStore{}
}

func (s *fakeObservationStore) AppendActivityEvent(_ context.Context, event *ActivityEvent) (*ActivityEvent, error) {
	inserted, err := rehydrateActivityEvent(
		int64(len(s.appended)+1),
		event.SourceDomain(),
		event.EventType(),
		event.AgentIdentity(),
		event.RuntimeInstanceID(),
		event.Payload(),
		event.DisplayOnly(),
		event.CreatedAt(),
	)
	if err != nil {
		return nil, err
	}
	s.appended = append(s.appended, inserted)
	return inserted, nil
}

func (s *fakeObservationStore) ListActivityEvents(_ context.Context, _ int) ([]LaneEvent, error) {
	return append([]LaneEvent(nil), s.activityEvents...), nil
}

func (s *fakeObservationStore) ListDeliveryEvents(_ context.Context, _ int) ([]LaneEvent, error) {
	return append([]LaneEvent(nil), s.deliveryEvents...), nil
}

func (s *fakeObservationStore) ListRuntimeEvents(_ context.Context, _ int) ([]LaneEvent, error) {
	return append([]LaneEvent(nil), s.runtimeEvents...), nil
}

func (s *fakeObservationStore) ListChatEvents(_ context.Context, _ int) ([]LaneEvent, error) {
	return append([]LaneEvent(nil), s.chatEvents...), nil
}

func (s *fakeObservationStore) ListActiveWork(_ context.Context) ([]ActiveWorkItem, error) {
	s.activeWorkCalls++
	return append([]ActiveWorkItem(nil), s.activeWork...), nil
}

func (s *fakeObservationStore) ListRuntimeProjections(_ context.Context, _ string) ([]RuntimeProjection, error) {
	return append([]RuntimeProjection(nil), s.runtimes...), nil
}

func (s *fakeObservationStore) ListActiveWorkForAgent(_ context.Context, _ string) ([]ActiveWorkItem, error) {
	return append([]ActiveWorkItem(nil), s.activeWork...), nil
}

func eventAt(id string, source SourceDomain, at time.Time) LaneEvent {
	return LaneEvent{
		EventID:      id,
		SourceDomain: source,
		EventType:    "test",
		Payload:      json.RawMessage(`{}`),
		DisplayOnly:  true,
		CreatedAt:    at,
	}
}

func testIdentity() identity.AgentIdentity {
	return identity.AgentIdentity{
		Profile:    "planner",
		InstanceID: "planner@host-1",
	}
}

func ptrIdentity(value identity.AgentIdentity) *identity.AgentIdentity {
	return &value
}

func fixedClock() time.Time {
	return fixedTime()
}

func fixedTime() time.Time {
	return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
}
