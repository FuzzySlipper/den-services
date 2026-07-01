package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"den-services/shared/health"

	"den-services/mcp/internal/config"
)

func TestNewHTTPServerRegistersHealthAndVersion(t *testing.T) {
	server := newTestServer(t, true, nil)

	for _, path := range []string{"/health", "/version"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusOK)
		}
	}
}

func TestMCPInitializeIsWired(t *testing.T) {
	server := newTestServer(t, true, nil)

	response := postJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-11-25",
		},
	}, "")

	if response.Code != http.StatusOK {
		t.Fatalf("POST /mcp initialize status = %d, want %d", response.Code, http.StatusOK)
	}
	var body map[string]any
	decodeResponse(t, response, &body)
	result := body["result"].(map[string]any)
	if result["protocolVersion"] != "2025-11-25" {
		t.Fatalf("protocolVersion = %v", result["protocolVersion"])
	}
	capabilities := result["capabilities"].(map[string]any)
	if _, ok := capabilities["tools"]; !ok {
		t.Fatalf("capabilities missing tools: %#v", capabilities)
	}
}

func TestMCPToolsListIsStatic(t *testing.T) {
	server := newTestServer(t, true, nil)

	response := postJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      "tools",
		"method":  "tools/list",
	}, "")

	if response.Code != http.StatusOK {
		t.Fatalf("POST /mcp tools/list status = %d, want %d", response.Code, http.StatusOK)
	}
	var body map[string]any
	decodeResponse(t, response, &body)
	result := body["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 61 {
		t.Fatalf("tool count = %d, want 61", len(tools))
	}
	first := tools[0].(map[string]any)
	if first["name"] != "search_documents" {
		t.Fatalf("first tool name = %v, want search_documents", first["name"])
	}
}

func TestMCPBatchRequests(t *testing.T) {
	server := newTestServer(t, true, nil)
	requestBody := []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-11-25",
			},
		},
		{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/list",
		},
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("POST /mcp batch status = %d, want %d", response.Code, http.StatusOK)
	}
	var responses []map[string]any
	decodeResponse(t, response, &responses)
	if len(responses) != 2 {
		t.Fatalf("batch response count = %d, want 2", len(responses))
	}
}

func TestMCPHandlerRequiresAuthWhenConfigured(t *testing.T) {
	server := newTestServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("POST /mcp without token status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	request = httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("POST /mcp with token status = %d, want %d", response.Code, http.StatusAccepted)
	}
}

func TestMCPRejectsNonPost(t *testing.T) {
	server := newTestServer(t, true, nil)

	request := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestToolsCallReturnsBackendFailureResult(t *testing.T) {
	server := newTestServer(t, true, nil)

	response := postJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "get_task",
			"arguments": map[string]any{"task_id": 1},
		},
	}, "")

	if response.Code != http.StatusOK {
		t.Fatalf("POST /mcp tools/call status = %d, want %d", response.Code, http.StatusOK)
	}
	var body map[string]any
	decodeResponse(t, response, &body)
	result := body["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("isError = %v, want true", result["isError"])
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["error"] != "den_backend_unavailable" {
		t.Fatalf("structured error = %v", structured["error"])
	}
	if structured["tool"] != "get_task" {
		t.Fatalf("structured tool = %v", structured["tool"])
	}
	if structured["operation"] != "get_task" {
		t.Fatalf("structured operation = %v", structured["operation"])
	}
	if structured["circuit_state"] != "unavailable" {
		t.Fatalf("structured circuit_state = %v", structured["circuit_state"])
	}
}

func TestToolsCallReturnsRetiredToolTombstone(t *testing.T) {
	server := newTestServer(t, true, nil)

	response := postJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "lease_worker",
			"arguments": map[string]any{"project_id": "den-services"},
		},
	}, "")

	if response.Code != http.StatusOK {
		t.Fatalf("POST /mcp tools/call status = %d, want %d", response.Code, http.StatusOK)
	}
	var body map[string]any
	decodeResponse(t, response, &body)
	result := body["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("isError = %v, want true", result["isError"])
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["error"] != "den_mcp_tool_retired" {
		t.Fatalf("structured error = %v", structured["error"])
	}
	if structured["tool"] != "lease_worker" {
		t.Fatalf("structured tool = %v", structured["tool"])
	}
	if structured["retired"] != true {
		t.Fatalf("structured retired = %v", structured["retired"])
	}
	if structured["hidden_from"] != "tools/list" {
		t.Fatalf("structured hidden_from = %v", structured["hidden_from"])
	}
}

func newTestServer(t *testing.T, allowUnauthenticatedLocalDev bool, handler MCPHandler) *http.Server {
	t.Helper()
	info, err := health.NewBuildInfo("mcp", "dev", "test", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(&config.Config{
		Server: config.ServerConfig{
			ListenAddr:        "127.0.0.1:0",
			MCPEndpointPath:   "/mcp",
			ReadHeaderTimeout: 5 * time.Second,
		},
		Routes: config.RouteConfig{
			TablePath: testRouteTable(t),
		},
		Backends: []config.BackendConfig{
			{
				Name:       "den-core",
				BaseURL:    "http://127.0.0.1:1",
				HealthPath: "/health",
				Timeout:    20 * time.Millisecond,
			},
		},
		Security: config.SecurityConfig{
			ServiceToken:                 "test-token",
			AllowUnauthenticatedLocalDev: allowUnauthenticatedLocalDev,
		},
	}, info, handler)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server
}

func testRouteTable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "routes.yaml")
	content := `routes:
  - operation: "get_task"
    backend: "den-core"
    method: "POST"
    path: "/mcp"
    request_adapter: "mcp_tools_call"
    response_adapter: "mcp_jsonrpc_result"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing route table: %v", err)
	}
	return path
}

func postJSON(t *testing.T, server *http.Server, payload map[string]any, token string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", response.Body.String(), err)
	}
}
