package health

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthHandlerReturnsBuildMetadata(t *testing.T) {
	builtAt := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	info, err := NewBuildInfo("gateway", "1.2.3", "abc123", builtAt)
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	handler, err := HealthHandler(info)
	if err != nil {
		t.Fatalf("HealthHandler() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var got HealthResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.Status != "ok" || got.ServiceName != "gateway" || got.Version != "1.2.3" || got.Commit != "abc123" {
		t.Fatalf("health response = %#v", got)
	}
	if !got.BuiltAt.Equal(builtAt) {
		t.Fatalf("built_at = %v, want %v", got.BuiltAt, builtAt)
	}
}

func TestVersionHandlerReturnsBuildMetadata(t *testing.T) {
	builtAt := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	info, err := NewBuildInfo("runtime", "1.2.3", "abc123", builtAt)
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	handler, err := VersionHandler(info)
	if err != nil {
		t.Fatalf("VersionHandler() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/version", nil))

	var got VersionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.ServiceName != "runtime" || got.Version != "1.2.3" || got.Commit != "abc123" {
		t.Fatalf("version response = %#v", got)
	}
}

func TestBuildInfoValidatesRequiredFields(t *testing.T) {
	_, err := NewBuildInfo("", "1.2.3", "abc123", time.Now())
	if !errors.Is(err, ErrMissingServiceName) {
		t.Fatalf("NewBuildInfo() error = %v, want %v", err, ErrMissingServiceName)
	}
}
