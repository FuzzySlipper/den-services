package observation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerCreatesLifecycleEvent(t *testing.T) {
	store := newFakeObservationStore()
	handler := NewHandler(NewObservationService(store, fixedClock, 10, 100))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{
		"source_domain":"runtime",
		"event_type":"agent_session_started",
		"payload":{
			"kind":"agent_activity.v1",
			"schema_version":1,
			"summary":"Hermes session started.",
			"severity":"info",
			"visibility":"agent",
			"adapter":"hermes",
			"surface":"worker",
			"session_key":"session-1"
		}
	}`
	request := httptest.NewRequest(http.MethodPost, "/v1/observation/lifecycle-events", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var response ActivityEventResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !response.DisplayOnly {
		t.Fatal("response display_only = false, want true")
	}
}

func TestHandlerCreatesActivityEventAlias(t *testing.T) {
	store := newFakeObservationStore()
	handler := NewHandler(NewObservationService(store, fixedClock, 10, 100))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{
		"source_domain":"runtime",
		"event_type":"work_checkpoint",
		"agent_identity":{"profile":"pi-crew-runner","instance_id":"pi-crew-runner@host-1"},
		"payload":{
			"kind":"agent_activity.v1",
			"schema_version":1,
			"summary":"pi-crew checkpointed work.",
			"severity":"info",
			"visibility":"task",
			"adapter":"pi-crew",
			"surface":"worker",
			"work_ref":{"project_id":"den-services","task_id":2810}
		}
	}`
	request := httptest.NewRequest(http.MethodPost, "/v1/observation/activity-events", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if len(store.appended) != 1 {
		t.Fatalf("append count = %d, want 1", len(store.appended))
	}
}

func TestHandlerRejectsMalformedActivityEnvelope(t *testing.T) {
	handler := NewHandler(NewObservationService(newFakeObservationStore(), fixedClock, 10, 100))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{
		"source_domain":"runtime",
		"event_type":"agent_session_started",
		"payload":{
			"kind":"agent_activity.v1",
			"schema_version":1,
			"summary":"Missing session key.",
			"severity":"info",
			"visibility":"agent",
			"adapter":"hermes",
			"surface":"worker"
		}
	}`
	request := httptest.NewRequest(http.MethodPost, "/v1/observation/activity-events", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "payload.session_key") {
		t.Fatalf("body = %s, want missing session_key detail", recorder.Body.String())
	}
}

func TestHandlerLaneDoesNotMutateObservation(t *testing.T) {
	store := newFakeObservationStore()
	store.activityEvents = []LaneEvent{eventAt("observation:1", SourceDomainObservation, fixedTime())}
	handler := NewHandler(NewObservationService(store, fixedClock, 10, 100))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodGet, "/v1/observation/lane", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if len(store.appended) != 0 {
		t.Fatalf("lane append count = %d, want 0", len(store.appended))
	}
}

func TestHandlerAgentsOverview(t *testing.T) {
	store := newFakeObservationStore()
	store.agentIDs = []string{"den-mcp-runner"}
	store.runtimes = []RuntimeProjection{{
		RuntimeInstanceID: "den-mcp-runner@host-1",
		ProfileIdentity:   "den-mcp-runner",
		Host:              "host-1",
		State:             "active",
		StartedAt:         fixedTime(),
		DisplayOnly:       true,
	}}
	store.activityEventsForAgent = []LaneEvent{eventAt("observation:1", SourceDomainRuntime, fixedTime())}
	handler := NewHandler(NewObservationService(store, fixedClock, 10, 100))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodGet, "/v1/observation/agents/overview", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response AgentsOverviewResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(response.Agents) != 1 || response.Agents[0].AgentID != "den-mcp-runner" {
		t.Fatalf("agents overview = %#v", response)
	}
}

func TestHandlerAssignmentTraceKeepsAuthorityBoundariesExplicit(t *testing.T) {
	store := newFakeObservationStore()
	store.assignmentMessages = []AssignmentMessage{{
		MessageID:      10,
		ChannelID:      42,
		SenderType:     "agent",
		SenderIdentity: "den-mcp-runner",
		Body:           "checkpoint",
		MessageKind:    "gateway_delivery",
		SourceKind:     "gateway_delivery",
		AssignmentID:   "123",
		Metadata:       json.RawMessage(`{}`),
		CreatedAt:      fixedTime(),
	}}
	store.activityEventsForAssignment = []LaneEvent{eventAt("observation:123", SourceDomainRuntime, fixedTime())}
	handler := NewHandler(NewObservationService(store, fixedClock, 10, 100))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodGet, "/v1/observation/assignments/123/trace", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response AssignmentTraceResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if response.AssignmentID != "123" || response.TranscriptAvailability != "available" || response.ActivityAvailability != "available" {
		t.Fatalf("trace response = %#v", response)
	}
	if response.CoreAvailability != "not_observation_owned" || response.ExecutableStateOwner != "den-core/delivery" || response.ConversationOwner != "conversation" {
		t.Fatalf("authority fields = %#v", response)
	}
}

func TestHandlerActivityHistoryReadAndStatus(t *testing.T) {
	store := newFakeObservationStore()
	store.activityEventsForAssignment = []LaneEvent{eventAt("observation:assignment", SourceDomainRuntime, fixedTime())}
	handler := NewHandler(NewObservationService(store, fixedClock, 10, 100))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	readRequest := httptest.NewRequest(http.MethodGet, "/v1/observation/activity-events?assignment_id=123", nil)
	readRecorder := httptest.NewRecorder()
	mux.ServeHTTP(readRecorder, readRequest)
	if readRecorder.Code != http.StatusOK {
		t.Fatalf("read status = %d, want %d; body=%s", readRecorder.Code, http.StatusOK, readRecorder.Body.String())
	}
	var lane LaneResponse
	if err := json.Unmarshal(readRecorder.Body.Bytes(), &lane); err != nil {
		t.Fatalf("Unmarshal lane error = %v", err)
	}
	if len(lane.Events) != 1 {
		t.Fatalf("activity history events = %d, want 1", len(lane.Events))
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/observation/activity-events/status", nil)
	statusRecorder := httptest.NewRecorder()
	mux.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status status = %d, want %d; body=%s", statusRecorder.Code, http.StatusOK, statusRecorder.Body.String())
	}
	var status ActivityHistoryStatusResponse
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("Unmarshal status error = %v", err)
	}
	if !status.Writable || status.PatchSupported || status.ObservationProjection != "display_only" {
		t.Fatalf("status response = %#v", status)
	}
}
