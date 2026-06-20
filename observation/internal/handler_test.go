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
