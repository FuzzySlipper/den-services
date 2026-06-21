package observation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"den-services/shared/identity"
)

func TestAppendLifecycleEventAcceptsHermesAgentActivity(t *testing.T) {
	store := newFakeObservationStore()
	service := NewObservationService(store, fixedClock, 10, 100)
	instanceID := identity.AgentInstanceID("planner@host-1")
	event, err := service.AppendLifecycleEvent(context.Background(), CreateLifecycleEventRequest{
		SourceDomain:      SourceDomainRuntime,
		EventType:         "agent_session_started",
		AgentIdentity:     ptrIdentity(hermesIdentity()),
		RuntimeInstanceID: &instanceID,
		Payload: activityPayload(t, map[string]any{
			"summary":     "Hermes runner session started.",
			"adapter":     "hermes",
			"surface":     "worker",
			"session_key": "hermes-session-1",
		}),
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
	var payload map[string]any
	if err := json.Unmarshal(event.Payload(), &payload); err != nil {
		t.Fatalf("payload JSON error = %v", err)
	}
	if payload["summary"] != "Hermes runner session started." || payload["severity"] != "info" || payload["visibility"] != "agent" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestAppendLifecycleEventAcceptsPiCrewWorkActivity(t *testing.T) {
	store := newFakeObservationStore()
	service := NewObservationService(store, fixedClock, 10, 100)
	event, err := service.AppendLifecycleEvent(context.Background(), CreateLifecycleEventRequest{
		SourceDomain:  SourceDomainRuntime,
		EventType:     "work_completed",
		AgentIdentity: ptrIdentity(piCrewIdentity()),
		Payload: activityPayload(t, map[string]any{
			"summary":    "pi-crew reviewer completed task 2810 review.",
			"severity":   "success",
			"visibility": "task",
			"adapter":    "pi-crew",
			"surface":    "review",
			"work_ref": map[string]any{
				"project_id":    "den-services",
				"task_id":       2810,
				"assignment_id": "42",
				"run_id":        "run-pi-crew-1",
			},
			"result_ref": map[string]any{
				"message_id": 15743,
			},
		}),
	})
	if err != nil {
		t.Fatalf("AppendLifecycleEvent() error = %v", err)
	}
	if event.AgentIdentity().Profile != "pi-crew-reviewer-worker" {
		t.Fatalf("profile = %s", event.AgentIdentity().Profile)
	}
	if !event.DisplayOnly() {
		t.Fatal("pi-crew event display_only = false, want true")
	}
}

func TestAppendLifecycleEventAcceptsUnknownFutureEventTypeWithEnvelope(t *testing.T) {
	service := NewObservationService(newFakeObservationStore(), fixedClock, 10, 100)
	event, err := service.AppendLifecycleEvent(context.Background(), CreateLifecycleEventRequest{
		SourceDomain: SourceDomainRuntime,
		EventType:    "agent_custom_future_event",
		Payload: activityPayload(t, map[string]any{
			"summary":    "Future breadcrumb rendered generically.",
			"visibility": "debug",
			"adapter":    "den-services",
			"surface":    "observation",
			"custom_ref": "future-field",
		}),
	})
	if err != nil {
		t.Fatalf("AppendLifecycleEvent() error = %v", err)
	}
	if event.EventType() != "agent_custom_future_event" {
		t.Fatalf("event type = %s", event.EventType())
	}
}

func TestAppendLifecycleEventRejectsMalformedAgentActivityPayload(t *testing.T) {
	service := NewObservationService(newFakeObservationStore(), fixedClock, 10, 100)
	_, err := service.AppendLifecycleEvent(context.Background(), CreateLifecycleEventRequest{
		SourceDomain: SourceDomainRuntime,
		EventType:    "work_started",
		Payload: activityPayload(t, map[string]any{
			"summary": "Missing work ref should fail.",
			"adapter": "hermes",
			"surface": "worker",
		}),
	})
	if err == nil {
		t.Fatal("AppendLifecycleEvent() error is nil, want validation error")
	}
	if !errors.Is(err, ErrInvalidActivityEvent) {
		t.Fatalf("AppendLifecycleEvent() error = %v, want ErrInvalidActivityEvent", err)
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

func TestLanePreservesActivityEventEnvelope(t *testing.T) {
	store := newFakeObservationStore()
	createdAt := fixedTime().Add(-3 * time.Minute)
	store.activityEvents = []LaneEvent{{
		EventID:       "observation:9",
		SourceDomain:  SourceDomainRuntime,
		EventType:     "tool_call_completed",
		AgentIdentity: ptrIdentity(hermesIdentity()),
		Payload: activityPayload(t, map[string]any{
			"summary":   "Hermes completed a visible tool call.",
			"adapter":   "hermes",
			"surface":   "worker",
			"tool_name": "den.task.read",
			"result_ref": map[string]any{
				"message_id": 15743,
			},
		}),
		DisplayOnly: true,
		CreatedAt:   createdAt,
	}}
	service := NewObservationService(store, fixedClock, 10, 100)
	events, err := service.Lane(context.Background(), "")
	if err != nil {
		t.Fatalf("Lane() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	event := events[0]
	if event.EventType != "tool_call_completed" || event.AgentIdentity.Profile != "den-mcp-runner" || !event.CreatedAt.Equal(createdAt) {
		t.Fatalf("lane event = %+v", event)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("payload JSON error = %v", err)
	}
	if payload["summary"] != "Hermes completed a visible tool call." || payload["severity"] != "info" || payload["visibility"] != "agent" {
		t.Fatalf("payload = %#v", payload)
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

func TestAgentOverviewIncludesActivityEvents(t *testing.T) {
	store := newFakeObservationStore()
	createdAt := fixedTime().Add(-5 * time.Minute)
	store.activityEventsForAgent = []LaneEvent{{
		EventID:       "observation:7",
		SourceDomain:  SourceDomainRuntime,
		EventType:     "work_checkpoint",
		AgentIdentity: ptrIdentity(piCrewIdentity()),
		Payload: activityPayload(t, map[string]any{
			"summary":    "Checkpoint reached.",
			"visibility": "task",
			"adapter":    "pi-crew",
			"surface":    "worker",
			"work_ref": map[string]any{
				"project_id": "den-services",
				"task_id":    2810,
			},
		}),
		DisplayOnly: true,
		CreatedAt:   createdAt,
	}}
	service := NewObservationService(store, fixedClock, 10, 100)
	overview, err := service.AgentOverview(context.Background(), "pi-crew-reviewer-worker")
	if err != nil {
		t.Fatalf("AgentOverview() error = %v", err)
	}
	if len(overview.ActivityEvents) != 1 {
		t.Fatalf("activity events len = %d, want 1", len(overview.ActivityEvents))
	}
	event := overview.ActivityEvents[0]
	if event.EventType != "work_checkpoint" || event.AgentIdentity.Profile != "pi-crew-reviewer-worker" || !event.CreatedAt.Equal(createdAt) {
		t.Fatalf("overview activity event = %+v", event)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("payload JSON error = %v", err)
	}
	if payload["summary"] != "Checkpoint reached." || payload["visibility"] != "task" || payload["severity"] != "info" {
		t.Fatalf("payload = %#v", payload)
	}
}

type fakeObservationStore struct {
	appended                    []*ActivityEvent
	activityEvents              []LaneEvent
	activityEventsForAgent      []LaneEvent
	activityEventsForAssignment []LaneEvent
	deliveryEvents              []LaneEvent
	runtimeEvents               []LaneEvent
	chatEvents                  []LaneEvent
	activeWork                  []ActiveWorkItem
	runtimes                    []RuntimeProjection
	agentIDs                    []string
	assignmentMessages          []AssignmentMessage
	activeWorkCalls             int
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

func (s *fakeObservationStore) ListActivityEventsForAgent(_ context.Context, _ string, _ int) ([]LaneEvent, error) {
	return append([]LaneEvent(nil), s.activityEventsForAgent...), nil
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

func (s *fakeObservationStore) ListAgentIDs(_ context.Context, _ int) ([]string, error) {
	return append([]string(nil), s.agentIDs...), nil
}

func (s *fakeObservationStore) ListAssignmentMessages(_ context.Context, _ string, _ int) ([]AssignmentMessage, error) {
	return append([]AssignmentMessage(nil), s.assignmentMessages...), nil
}

func (s *fakeObservationStore) ListActivityEventsForAssignment(_ context.Context, _ string, _ int) ([]LaneEvent, error) {
	return append([]LaneEvent(nil), s.activityEventsForAssignment...), nil
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

func hermesIdentity() identity.AgentIdentity {
	return identity.AgentIdentity{
		Profile:    "den-mcp-runner",
		InstanceID: "den-mcp-runner@hermes-1",
	}
}

func piCrewIdentity() identity.AgentIdentity {
	return identity.AgentIdentity{
		Profile:    "pi-crew-reviewer-worker",
		InstanceID: "pi-crew-reviewer-worker@pool-1",
	}
}

func activityPayload(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	payload := map[string]any{
		"kind":           "agent_activity.v1",
		"schema_version": 1,
		"summary":        "Agent activity breadcrumb.",
		"severity":       "info",
		"visibility":     "agent",
		"adapter":        "hermes",
		"surface":        "worker",
	}
	for key, value := range fields {
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return data
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
