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

	body := `{"source_domain":"observation","event_type":"agent_work_lifecycle","payload":{"status":"seen"}}`
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
