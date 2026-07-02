package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"den-services/mcp/internal/config"
)

func TestLocatorComposesTaskWorkflowSummary(t *testing.T) {
	var sawCoderPacket bool
	var sawReviewerPacket bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/3889":
			_, _ = w.Write([]byte(`{
				"task":{"id":3889,"project_id":"den-services","title":"Compose workflow summary","status":"in_progress","assigned_to":"codex"},
				"dependencies":[{"task_id":3889,"depends_on":3880}],
				"subtasks":[],
				"history":[{"field":"status","new_value":"in_progress"}]
			}`))
		case "/v1/projects/den-services/tasks/3889/review/workflow-summary":
			_, _ = w.Write([]byte(`{
				"current_round":{"id":42,"round_number":3},
				"current_verdict":"changes_requested",
				"review_round_count":3,
				"unresolved_finding_count":2,
				"resolved_finding_count":5,
				"addressed_finding_count":1,
				"open_findings":[{"id":701,"severity":"major"}],
				"resolved_findings":[{"id":702,"severity":"minor"}],
				"timeline":[{"round_number":3,"open_findings":2}]
			}`))
		case "/v1/projects/den-services/tasks/3889/packets/latest":
			switch r.URL.Query().Get("role") {
			case "coder":
				sawCoderPacket = true
				_, _ = w.Write([]byte(`{
					"id":901,
					"project_id":"den-services",
					"task_id":3889,
					"thread_id":777,
					"sender":"orchestrator",
					"content":"full body must not leak",
					"intent":"context_packet",
					"metadata":{"type":"coder_context_packet","role":"coder"},
					"created_at":"2026-07-01T10:00:00Z"
				}`))
			case "reviewer":
				sawReviewerPacket = true
				http.Error(w, "not found", http.StatusNotFound)
			default:
				http.Error(w, "not found", http.StatusNotFound)
			}
		default:
			t.Fatalf("unexpected request path %s", r.URL.String())
		}
	}))
	defer server.Close()

	locator := newTaskWorkflowSummaryTestLocator(t, server)
	result, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_task_workflow_summary",
		Operation: "get_task_workflow_summary",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":3889}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if !sawCoderPacket || !sawReviewerPacket {
		t.Fatalf("packet probes coder=%t reviewer=%t, want both true", sawCoderPacket, sawReviewerPacket)
	}

	var toolResult mcpToolResult
	if err := json.Unmarshal(result.Value, &toolResult); err != nil {
		t.Fatalf("Unmarshal(result) error = %v", err)
	}
	text := toolResult.Content[0].Text
	for _, want := range []string{
		`"task_id":3889`,
		`"project_id":"den-services"`,
		`"current_verdict":"changes_requested"`,
		`"unresolved_finding_count":2`,
		`"coder":{"id":901`,
		`"reviewer":null`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %s, missing %s", text, want)
		}
	}
	if strings.Contains(text, "full body must not leak") {
		t.Fatalf("workflow summary leaked packet body: %s", text)
	}
	if strings.Contains(text, `"history"`) || strings.Contains(text, `"review_timeline"`) {
		t.Fatalf("compact workflow summary included verbose fields: %s", text)
	}
}

func TestLocatorComposesVerboseTaskWorkflowSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/3889":
			_, _ = w.Write([]byte(`{"task":{"id":3889,"project_id":"den-services","title":"Compose workflow summary","status":"done"},"history":[{"field":"status"}]}`))
		case "/v1/projects/den-services/tasks/3889/review/workflow-summary":
			_, _ = w.Write([]byte(`{"review_round_count":0,"unresolved_finding_count":0,"resolved_finding_count":0,"addressed_finding_count":0,"timeline":[]}`))
		case "/v1/projects/den-services/tasks/3889/packets/latest":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			t.Fatalf("unexpected request path %s", r.URL.String())
		}
	}))
	defer server.Close()

	locator := newTaskWorkflowSummaryTestLocator(t, server)
	result, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_task_workflow_summary",
		Operation: "get_task_workflow_summary",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":3889,"verbose":true}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if !strings.Contains(string(result.Value), `"history":[{"field":"status"}]`) {
		t.Fatalf("verbose result missing task history: %s", result.Value)
	}
	if !strings.Contains(string(result.Value), `"review_timeline":[]`) {
		t.Fatalf("verbose result missing review timeline: %s", result.Value)
	}
}

func TestTaskWorkflowSummaryFailsClosedWhenReviewUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/3889":
			_, _ = w.Write([]byte(`{"task":{"id":3889,"project_id":"den-services","title":"Compose workflow summary","status":"in_progress"}}`))
		case "/v1/projects/den-services/tasks/3889/review/workflow-summary":
			http.Error(w, "review down", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request path %s", r.URL.String())
		}
	}))
	defer server.Close()

	locator := newTaskWorkflowSummaryTestLocator(t, server)
	_, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_task_workflow_summary",
		Operation: "get_task_workflow_summary",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":3889}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure == nil {
		t.Fatal("failure = nil")
	}
	if failure.Backend != "review" || failure.StatusCode == nil || *failure.StatusCode != http.StatusBadGateway {
		t.Fatalf("failure = %#v", failure)
	}
}

func newTaskWorkflowSummaryTestLocator(t *testing.T, server *httptest.Server) *Locator {
	t.Helper()
	table, err := NewRouteTable([]Route{
		{
			Operation:       "get_task_workflow_summary",
			Backend:         "tasks",
			Method:          http.MethodGet,
			Path:            "/v1/tasks/{task_id}/workflow-summary",
			RequestAdapter:  RequestAdapterMCPTaskWorkflowSummaryCompose,
			ResponseAdapter: ResponseAdapterMCPToolResultJSON,
		},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	locator, err := NewLocator([]config.BackendConfig{
		testBackend("tasks", server.URL),
		testBackend("review", server.URL),
		testBackend("messages", server.URL),
	}, table, server.Client())
	if err != nil {
		t.Fatalf("NewLocator() error = %v", err)
	}
	return locator
}
