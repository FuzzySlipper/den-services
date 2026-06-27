package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if failure.Tool != "get_task" {
		t.Fatalf("failure.Tool = %q, want get_task", failure.Tool)
	}
	if failure.CircuitState != string(StateUnavailable) {
		t.Fatalf("CircuitState = %q, want %q", failure.CircuitState, StateUnavailable)
	}
	state, ok := locator.BackendState("den-core")
	if !ok || state != StateUnavailable {
		t.Fatalf("BackendState = %s/%t, want %s/true", state, ok, StateUnavailable)
	}
}

func TestLocatorClassifiesClosedPortAsRetryableUnavailable(t *testing.T) {
	baseURL := closedPortURL(t)
	table, err := NewRouteTable([]Route{testRoute("get_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	backendConfig := testBackend("den-core", baseURL)
	backendConfig.Timeout = 100 * time.Millisecond
	locator, err := NewLocator([]config.BackendConfig{backendConfig}, table, nil)
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
	if failure == nil || !failure.Retryable || failure.Error != "den_backend_unavailable" {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestLocatorClassifiesValidationAndDomainStatusAsNonRetryable(t *testing.T) {
	for _, statusCode := range []int{http.StatusBadRequest, http.StatusNotFound} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			locator, closeServer := locatorWithStatusBackend(t, statusCode, "backend says no")
			defer closeServer()

			_, failure, err := locator.Call(context.Background(), ToolCall{
				ToolName:  "get_task",
				Operation: "get_task",
				RequestID: json.RawMessage(`1`),
			})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if failure == nil {
				t.Fatal("failure = nil")
			}
			if failure.Retryable {
				t.Fatalf("Retryable = true, want false")
			}
			if failure.Error != "den_backend_request_failed" {
				t.Fatalf("Error = %q, want den_backend_request_failed", failure.Error)
			}
			if failure.StatusCode == nil || *failure.StatusCode != statusCode {
				t.Fatalf("StatusCode = %v, want %d", failure.StatusCode, statusCode)
			}
			if failure.Message != "backend says no" {
				t.Fatalf("Message = %q", failure.Message)
			}
		})
	}
}

func TestLocatorClassifiesAuthStatusAsNonRetryableConfigFailure(t *testing.T) {
	for _, statusCode := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			locator, closeServer := locatorWithStatusBackend(t, statusCode, "bad token")
			defer closeServer()

			_, failure, err := locator.Call(context.Background(), ToolCall{
				ToolName:  "get_task",
				Operation: "get_task",
				RequestID: json.RawMessage(`1`),
			})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if failure == nil {
				t.Fatal("failure = nil")
			}
			if failure.Retryable {
				t.Fatalf("Retryable = true, want false")
			}
			if failure.Error != "den_backend_auth_failed" {
				t.Fatalf("Error = %q, want den_backend_auth_failed", failure.Error)
			}
			if !strings.Contains(failure.Message, "service_token_env") {
				t.Fatalf("Message missing config hint: %q", failure.Message)
			}
		})
	}
}

func TestLocatorClassifiesGatewayStatusesAsRetryable(t *testing.T) {
	for _, statusCode := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			locator, closeServer := locatorWithStatusBackend(t, statusCode, "")
			defer closeServer()

			_, failure, err := locator.Call(context.Background(), ToolCall{
				ToolName:  "get_task",
				Operation: "get_task",
				RequestID: json.RawMessage(`1`),
			})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if failure == nil || !failure.Retryable || failure.Error != "den_backend_unavailable" {
				t.Fatalf("failure = %#v", failure)
			}
		})
	}
}

func TestLocatorTracksCircuitStatePerBackend(t *testing.T) {
	table, err := NewRouteTable([]Route{
		testRoute("get_task", "down-core"),
		testRoute("create_task", "up-core"),
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	upServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}],"isError":false}}`))
	}))
	defer upServer.Close()
	downBackend := testBackend("down-core", closedPortURL(t))
	downBackend.Timeout = 100 * time.Millisecond
	locator, err := NewLocator([]config.BackendConfig{
		downBackend,
		testBackend("up-core", upServer.URL),
	}, table, nil)
	if err != nil {
		t.Fatalf("NewLocator() error = %v", err)
	}

	_, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_task",
		Operation: "get_task",
		RequestID: json.RawMessage(`1`),
	})
	if err != nil {
		t.Fatalf("Call(down) error = %v", err)
	}
	if failure == nil {
		t.Fatal("failure = nil")
	}
	if state, _ := locator.BackendState("down-core"); state != StateUnavailable {
		t.Fatalf("down-core state = %s, want unavailable", state)
	}
	if state, _ := locator.BackendState("up-core"); state != StateReady {
		t.Fatalf("up-core state = %s, want ready", state)
	}
	_, failure, err = locator.Call(context.Background(), ToolCall{
		ToolName:  "create_task",
		Operation: "create_task",
		RequestID: json.RawMessage(`1`),
	})
	if err != nil {
		t.Fatalf("Call(up) error = %v", err)
	}
	if failure != nil {
		t.Fatalf("up failure = %#v", failure)
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

func locatorWithStatusBackend(t *testing.T, statusCode int, body string) (*Locator, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	table, err := NewRouteTable([]Route{testRoute("get_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	locator, err := NewLocator([]config.BackendConfig{testBackend("den-core", server.URL)}, table, server.Client())
	if err != nil {
		t.Fatalf("NewLocator() error = %v", err)
	}
	return locator, server.Close
}

func closedPortURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return "http://" + addr
}
