package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/mcp/internal/config"
)

func TestLocatorRejectsMissingBackend(t *testing.T) {
	table, err := NewRouteTable([]Route{testRoute("get_task", "missing")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	_, err = NewLocator([]config.BackendConfig{testBackend("den-core", "http://127.0.0.1:1")}, table, nil)
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("NewLocator() error = %v, want %v", err, ErrBackendNotFound)
	}
}

func TestLocatorInjectsTokenAndProxiesJSONRPCResult(t *testing.T) {
	var sawToken string
	var sawTool string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawToken = r.Header.Get("Authorization")
		var request struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		sawTool = request.Params.Name
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}],"isError":false}}`))
	}))
	defer server.Close()

	table, err := NewRouteTable([]Route{testRoute("get_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	backendConfig := testBackend("den-core", server.URL)
	backendConfig.ServiceToken = "secret"
	locator, err := NewLocator([]config.BackendConfig{backendConfig}, table, server.Client())
	if err != nil {
		t.Fatalf("NewLocator() error = %v", err)
	}

	result, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_task",
		Operation: "get_task",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":42}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawToken != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", sawToken)
	}
	if sawTool != "get_task" {
		t.Fatalf("tool = %q, want get_task", sawTool)
	}
	if len(result.Value) == 0 {
		t.Fatal("result value is empty")
	}
}

func TestLocatorClassifiesTimeoutAndMarksBackendUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	table, err := NewRouteTable([]Route{testRoute("get_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	backendConfig := testBackend("den-core", server.URL)
	backendConfig.Timeout = 5 * time.Millisecond
	locator, err := NewLocator([]config.BackendConfig{backendConfig}, table, server.Client())
	if err != nil {
		t.Fatalf("NewLocator() error = %v", err)
	}

	_, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_task",
		Operation: "get_task",
		RequestID: json.RawMessage(`1`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure == nil {
		t.Fatal("Call() failure = nil")
	}
	if !failure.Retryable || failure.Error != "den_backend_unavailable" {
		t.Fatalf("failure = %#v", failure)
	}
	state, ok := locator.BackendState("den-core")
	if !ok || state != StateUnavailable {
		t.Fatalf("BackendState = %s/%t, want %s/true", state, ok, StateUnavailable)
	}
}

func TestLocatorReloadRoutes(t *testing.T) {
	table, err := NewRouteTable([]Route{testRoute("get_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	locator, err := NewLocator([]config.BackendConfig{testBackend("den-core", "http://127.0.0.1:1")}, table, nil)
	if err != nil {
		t.Fatalf("NewLocator() error = %v", err)
	}
	newTable, err := NewRouteTable([]Route{testRoute("create_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable(new) error = %v", err)
	}

	if err := locator.ReloadRoutes(newTable); err != nil {
		t.Fatalf("ReloadRoutes() error = %v", err)
	}
	_, _, err = locator.Resolve("create_task")
	if err != nil {
		t.Fatalf("Resolve(create_task) error = %v", err)
	}
}

func TestLocatorReadinessUsesConfiguredHealthPath(t *testing.T) {
	var sawPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	table, err := NewRouteTable([]Route{testRoute("get_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	backendConfig := testBackend("den-core", server.URL)
	backendConfig.HealthPath = "/ready"
	locator, err := NewLocator([]config.BackendConfig{backendConfig}, table, server.Client())
	if err != nil {
		t.Fatalf("NewLocator() error = %v", err)
	}

	failure := locator.CheckReadiness(context.Background(), "den-core")
	if failure != nil {
		t.Fatalf("CheckReadiness() failure = %#v", failure)
	}
	if sawPath != "/ready" {
		t.Fatalf("health path = %q, want /ready", sawPath)
	}
	state, ok := locator.BackendState("den-core")
	if !ok || state != StateReady {
		t.Fatalf("BackendState = %s/%t, want %s/true", state, ok, StateReady)
	}
}

func testBackend(name string, baseURL string) config.BackendConfig {
	return config.BackendConfig{
		Name:       name,
		BaseURL:    baseURL,
		HealthPath: "/health",
		Timeout:    time.Second,
	}
}
