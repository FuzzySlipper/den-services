package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"den-services/shared/identity"
)

func TestRuntimeClientChecksInstanceLiveness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runtime/instances/planner@den-srv" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"active"}`))
	}))
	defer server.Close()

	client := NewRuntimeClient(server.URL, time.Second)
	alive, err := client.IsAlive(context.Background(), identity.AgentInstanceID("planner@den-srv"))
	if err != nil {
		t.Fatalf("IsAlive() error = %v", err)
	}
	if !alive {
		t.Fatal("IsAlive() = false, want true")
	}
}

func TestRuntimeClientErrorStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewRuntimeClient(server.URL, time.Second)
	alive, err := client.IsAlive(context.Background(), identity.AgentInstanceID("planner@den-srv"))
	if err == nil {
		t.Fatal("IsAlive() error is nil, want status error")
	}
	if alive {
		t.Fatal("IsAlive() = true, want false")
	}
	if !strings.Contains(err.Error(), "500 Internal Server Error") {
		t.Fatalf("IsAlive() error = %v, want 500 status", err)
	}
}
