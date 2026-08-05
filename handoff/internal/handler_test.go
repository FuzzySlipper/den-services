package handoff

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerSetAndGetExactHandoff(t *testing.T) {
	service := NewService(newMemoryStore(), func() time.Time {
		return time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	})
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	body := "---\ndatetime: unchanged\n---\n\n# Resume"
	payload, _ := json.Marshal(SetHandoffRequest{Label: "task/6651", BodyMarkdown: body})
	setRecorder := httptest.NewRecorder()
	mux.ServeHTTP(setRecorder, httptest.NewRequest(http.MethodPost, "/v1/handoffs", bytes.NewReader(payload)))
	if setRecorder.Code != http.StatusOK {
		t.Fatalf("set status/body = %d %s", setRecorder.Code, setRecorder.Body.String())
	}
	getRecorder := httptest.NewRecorder()
	mux.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/v1/handoffs?label=task%2F6651", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status/body = %d %s", getRecorder.Code, getRecorder.Body.String())
	}
	var response HandoffResponse
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Label != "task/6651" || response.BodyMarkdown != body || response.Revision != 1 {
		t.Fatalf("response = %#v", response)
	}
}

func TestHandlerIgnoresCallerSuppliedUpdatedBy(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store, func() time.Time {
		return time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	})
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	payload := []byte(`{"label":"task/6651","body_markdown":"body","updated_by":"forged-reviewer"}`)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/handoffs", bytes.NewReader(payload)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if len(store.current) != 0 || len(store.history) != 0 {
		t.Fatalf("forged write changed current/history: %#v/%#v", store.current, store.history)
	}
}

func TestHandlerMissingHandoffIsNotFound(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(NewService(newMemoryStore(), time.Now)).RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/handoffs?label=missing", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
}
