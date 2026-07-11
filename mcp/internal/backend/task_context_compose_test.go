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

func TestLocatorComposesBoundedTaskContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/5591":
			_, _ = w.Write([]byte(`{"task":{"id":5591,"project_id":"den-services","title":"Task context","description":"canonical description","status":"review","priority":2,"tags":["mcp","context"]},"dependencies":[{"task_id":5591,"depends_on":7}],"subtasks":[{"id":5592}]}`))
		case "/v1/projects/den-services/tasks/5591/review/workflow-summary":
			_, _ = w.Write([]byte(`{"current_round":{"id":4},"current_verdict":"changes_requested","review_round_count":2,"unresolved_finding_count":1,"open_findings":[{"id":9,"severity":"major"}]}`))
		case "/v1/projects/den-services/messages":
			if r.URL.Query().Get("task_id") != "5591" || r.URL.Query().Get("limit") != "12" || r.URL.Query().Get("verbose") != "true" {
				t.Fatalf("message query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"id":2,"sender":"planner","content":"newer","created_at":"2026-07-11T12:00:00Z"},{"id":1,"sender":"reviewer","content":"older","created_at":"2026-07-10T12:00:00Z"}]`))
		case "/v1/projects/den-services/agent-guidance":
			_, _ = w.Write([]byte(`{"project_id":"den-services","resolved_at":"2026-07-11T12:00:00Z","sources":[{"source_scope":"_global","document_project_id":"_global","document_slug":"den-connectivity-policy","document_title":"Connectivity policy","sort_order":1},{"source_scope":"den-services","document_project_id":"den-services","document_slug":"go-codestyle","document_title":"Go style","sort_order":2}]}`))
		case "/v1/projects/den-services/librarian/query":
			var request librarianQueryBody
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Query != "Task context mcp context" || request.TaskID == nil || *request.TaskID != 5591 {
				t.Fatalf("librarian request = %#v", request)
			}
			_, _ = w.Write([]byte(`{"query":"Task context mcp context","relevant_items":[{"source":"messages","source_id":"44","title":"Planner note"},{"source":"documents","source_id":"go-codestyle","title":"Go style"}],"recommendations":["Read sources first."],"confidence":"high"}`))
		case "/v1/projects/den-services/tasks/5591/packets/latest":
			if r.URL.Query().Get("role") == "coder" {
				_, _ = w.Write([]byte(`{"id":90,"project_id":"den-services","task_id":5591,"sender":"coder","intent":"context_packet","created_at":"2026-07-11T12:00:00Z"}`))
				return
			}
			http.Error(w, "not found", http.StatusNotFound)
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()

	locator := newTaskContextTestLocator(t, server, true)
	result, failure, err := locator.Call(context.Background(), ToolCall{ToolName: "get_task_context", Operation: "get_task_context", RequestID: json.RawMessage(`1`), Arguments: json.RawMessage(`{"project_id":"den-services","task_id":5591}`)})
	if err != nil || failure != nil {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	var toolResult mcpToolResult
	if err := json.Unmarshal(result.Value, &toolResult); err != nil {
		t.Fatal(err)
	}
	text := toolResult.Content[0].Text
	for _, want := range []string{
		`"schema_version":"1"`, `"unresolved_finding_count":1`, `"document_slug":"den-connectivity-policy"`, `"source":"librarian","state":"ok"`, `"messages:44"`, `"den-services/go-codestyle"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("task context missing %s: %s", want, text)
		}
	}
	if strings.Index(text, `"id":2`) > strings.Index(text, `"id":1`) {
		t.Fatalf("recent messages are not newest first: %s", text)
	}
}

func TestTaskContextReturnsPartialPacketWhenOptionalSourceUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/5591":
			_, _ = w.Write([]byte(`{"task":{"id":5591,"project_id":"den-services","title":"Task","status":"planned"}}`))
		case "/v1/projects/den-services/agent-guidance":
			http.Error(w, "guidance unavailable", http.StatusBadGateway)
		case "/v1/projects/den-services/messages", "/v1/projects/den-services/tasks/5591/review/workflow-summary", "/v1/projects/den-services/librarian/query":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	locator := newTaskContextTestLocator(t, server, false)
	result, failure, err := locator.Call(context.Background(), ToolCall{ToolName: "get_task_context", Operation: "get_task_context", RequestID: json.RawMessage(`1`), Arguments: json.RawMessage(`{"project_id":"den-services","task_id":5591}`)})
	if err != nil || failure != nil {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	if !strings.Contains(string(result.Value), `"source":"guidance","state":"unavailable","handle":"/v1/projects/den-services/agent-guidance","error_code":"den_backend_unavailable","retryable":true`) {
		t.Fatalf("missing explicit guidance status: %s", result.Value)
	}
}

func TestTaskContextFailsClosedForMissingTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "task not found", http.StatusNotFound) }))
	defer server.Close()
	locator := newTaskContextTestLocator(t, server, false)
	_, failure, err := locator.Call(context.Background(), ToolCall{ToolName: "get_task_context", Operation: "get_task_context", RequestID: json.RawMessage(`1`), Arguments: json.RawMessage(`{"project_id":"den-services","task_id":404}`)})
	if err != nil || failure == nil || failure.StatusCode == nil || *failure.StatusCode != http.StatusNotFound {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
}

func newTaskContextTestLocator(t *testing.T, server *httptest.Server, packets bool) *Locator {
	t.Helper()
	table, err := NewRouteTable([]Route{{Operation: "get_task_context", Backend: "tasks", Method: http.MethodGet, Path: "/v1/tasks/{task_id}/context", RequestAdapter: RequestAdapterMCPTaskContextCompose, ResponseAdapter: ResponseAdapterMCPToolResultJSON}})
	if err != nil {
		t.Fatal(err)
	}
	backends := []config.BackendConfig{testBackend("tasks", server.URL), testBackend("review", server.URL), testBackend("messages", server.URL), testBackend("guidance", server.URL), testBackend("librarian", server.URL)}
	if !packets {
		// The workflow packet probes are still safe: the fixture returns 404 for their absent paths.
	}
	locator, err := NewLocator(backends, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return locator
}
