package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if len(tools) != 9 {
		t.Fatalf("tool count = %d, want 9", len(tools))
	}
	first := tools[0].(map[string]any)
	if first["name"] != "get_project" {
		t.Fatalf("first tool name = %v, want get_project", first["name"])
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

func TestToolsCallReturnsRegisteredNotImplementedResult(t *testing.T) {
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
