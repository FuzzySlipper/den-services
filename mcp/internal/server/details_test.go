package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"den-services/shared/health"

	"den-services/mcp/internal/backend"
	"den-services/mcp/internal/config"
	"den-services/mcp/internal/registry"
)

func TestConciseReadDetailReferenceExpandsAndExpires(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks/42" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("verbose") == "true" {
			_, _ = w.Write([]byte(`{"task":{"id":42,"project_id":"den-services","description":"full detail"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"task":{"id":42,"project_id":"den-services","description_preview":"brief"}}`))
	}))
	defer backendServer.Close()

	table, err := backend.NewRouteTable([]backend.Route{{
		Operation: "get_task", Backend: "tasks", Method: http.MethodGet, Path: "/v1/tasks/{task_id}",
		RequestAdapter: backend.RequestAdapterMCPTasksREST, ResponseAdapter: backend.ResponseAdapterMCPToolResultJSON,
	}})
	if err != nil {
		t.Fatal(err)
	}
	backendConfig := config.BackendConfig{Name: "tasks", BaseURL: backendServer.URL, HealthPath: "/health", Timeout: time.Second}
	locator, err := backend.NewLocator([]config.BackendConfig{backendConfig}, table, backendServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	toolRegistry, err := registry.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	var logs bytes.Buffer
	handler := NewMCPHandlerWithOptions(toolRegistry, health.BuildInfo{}, locator, HandlerOptions{
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Clock: func() time.Time { return now },
		DetailReferenceTTL: 15 * time.Minute, DetailReferenceKey: []byte("01234567890123456789012345678901"),
	})

	concise := invokeToolForTest(t, handler, "get_task", map[string]any{"task_id": 42})
	structured := concise["structuredContent"].(map[string]any)
	detailRef, ok := structured["detail_ref"].(string)
	if !ok || detailRef == "" {
		t.Fatalf("detail_ref = %#v", structured["detail_ref"])
	}
	detailed := invokeToolForTest(t, handler, "get_details", map[string]any{"detail_ref": detailRef})
	if detailed["isError"] != false || !strings.Contains(detailed["content"].([]any)[0].(map[string]any)["text"].(string), "full detail") {
		t.Fatalf("detailed result = %#v", detailed)
	}
	tamperedRef := detailRef[:len(detailRef)-1] + "A"
	if strings.HasSuffix(detailRef, "A") {
		tamperedRef = detailRef[:len(detailRef)-1] + "B"
	}
	tampered := invokeToolForTest(t, handler, "get_details", map[string]any{"detail_ref": tamperedRef})
	if tampered["isError"] != true || !strings.Contains(tampered["content"].([]any)[0].(map[string]any)["text"].(string), "invalid detail reference") {
		t.Fatalf("tampered result = %#v", tampered)
	}

	now = now.Add(16 * time.Minute)
	expired := invokeToolForTest(t, handler, "get_details", map[string]any{"detail_ref": detailRef})
	if expired["isError"] != true || !strings.Contains(expired["content"].([]any)[0].(map[string]any)["text"].(string), "expired detail reference") {
		t.Fatalf("expired result = %#v", expired)
	}

	logText := logs.String()
	for _, want := range []string{`"msg":"mcp_tool_call"`, `"requested_tool":"get_task"`, `"canonical_tool":"get_task"`, `"requested_tool":"get_details"`, `"canonical_tool":"get_details"`, `"outcome":"success"`, `"outcome":"invalid_detail_ref"`} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s: %s", want, logText)
		}
	}
	if strings.Contains(logText, detailRef) || strings.Contains(logText, "full detail") {
		t.Fatalf("logs leaked arguments or content: %s", logText)
	}
}

func TestToolCallLogDistinguishesRequestedAliasFromCanonicalTool(t *testing.T) {
	toolRegistry, err := registry.New([]registry.ToolDefinition{{
		Name:             "get_task",
		Description:      "Get a task.",
		Backend:          "tasks",
		Operation:        "get_task",
		InputSchema:      registry.ObjectSchema(nil),
		TombstoneMessage: "retired for test",
		Aliases:          []registry.ToolAlias{{Name: "task_get"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler := NewMCPHandlerWithOptions(toolRegistry, health.BuildInfo{}, nil, HandlerOptions{
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	})

	invokeToolForTest(t, handler, "task_get", map[string]any{})
	logText := logs.String()
	for _, want := range []string{`"requested_tool":"task_get"`, `"canonical_tool":"get_task"`} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s: %s", want, logText)
		}
	}
}

func invokeToolForTest(t *testing.T, handler *Handler, name string, arguments map[string]any) map[string]any {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": arguments},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(payload))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var rpcBody map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &rpcBody); err != nil {
		t.Fatal(err)
	}
	result, ok := rpcBody["result"].(map[string]any)
	if !ok {
		t.Fatalf("RPC response = %#v", rpcBody)
	}
	return result
}
