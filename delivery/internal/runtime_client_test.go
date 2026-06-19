package delivery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"den-services/shared/identity"
)

func TestRuntimeClientSendsServiceToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer runtime-token" {
			t.Fatalf("Authorization = %q, want bearer runtime token", got)
		}
		if r.URL.Path != "/v1/runtime/instances/planner@den-srv" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"active"}`))
	}))
	defer server.Close()

	client := NewRuntimeClient(server.URL, "runtime-token", time.Second)
	alive, err := client.IsAlive(context.Background(), identity.AgentInstanceID("planner@den-srv"))
	if err != nil {
		t.Fatalf("IsAlive() error = %v", err)
	}
	if !alive {
		t.Fatal("IsAlive() = false, want true")
	}
}

func TestRuntimeClientMissingServiceTokenFailsClosed(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	defer server.Close()

	client := NewRuntimeClient(server.URL, "", time.Second)
	alive, err := client.IsAlive(context.Background(), identity.AgentInstanceID("planner@den-srv"))
	if !errors.Is(err, ErrMissingRuntimeAuth) {
		t.Fatalf("IsAlive() error = %v, want %v", err, ErrMissingRuntimeAuth)
	}
	if alive {
		t.Fatal("IsAlive() = true, want false")
	}
	if called {
		t.Fatal("runtime server was called without a runtime service token")
	}
}

func TestRuntimeClientUnauthorizedStatusNamesRuntimeTokenConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewRuntimeClient(server.URL, "wrong-runtime-token", time.Second)
	alive, err := client.IsAlive(context.Background(), identity.AgentInstanceID("planner@den-srv"))
	if err == nil {
		t.Fatal("IsAlive() error is nil, want unauthorized error")
	}
	if alive {
		t.Fatal("IsAlive() = true, want false")
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("IsAlive() error = %v, want 401 status", err)
	}
	if !strings.Contains(err.Error(), "DEN_DELIVERY_RUNTIME_SERVICE_TOKEN") {
		t.Fatalf("IsAlive() error = %v, want runtime token config hint", err)
	}
}
