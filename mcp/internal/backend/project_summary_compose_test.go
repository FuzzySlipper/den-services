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

func TestLocatorComposesProjectSummary(t *testing.T) {
	var sawTasksQuery string
	var sawUnreadQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/projects/den-services":
			_, _ = w.Write([]byte(`{"id":"den-services","name":"Den Services","kind":"project","visibility":"normal","root_path":"/home/dev/den-services","description":"Go successors","created_at":"2026-06-18T01:44:05Z","updated_at":"2026-06-18T01:44:05Z"}`))
		case "/v1/projects/den-services/tasks":
			sawTasksQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`[
				{"id":1,"project_id":"den-services","title":"A","status":"planned","priority":1},
				{"id":2,"project_id":"den-services","title":"B","status":"planned","priority":2},
				{"id":3,"project_id":"den-services","title":"C","status":"done","priority":3},
				{"id":4,"project_id":"den-services","title":"D","status":"blocked","priority":4}
			]`))
		case "/v1/projects/den-services/messages/unread-count":
			sawUnreadQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"unread_message_count":17}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.String())
		}
	}))
	defer server.Close()

	locator := newProjectSummaryTestLocator(t, server)
	result, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_project",
		Operation: "get_project",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"den-services","agent":"codex"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawTasksQuery != "tree=true" {
		t.Fatalf("tasks query = %q, want tree=true", sawTasksQuery)
	}
	if sawUnreadQuery != "unread_for=codex" {
		t.Fatalf("unread query = %q, want unread_for=codex", sawUnreadQuery)
	}
	var toolResult mcpToolResult
	if err := json.Unmarshal(result.Value, &toolResult); err != nil {
		t.Fatalf("Unmarshal(result) error = %v", err)
	}
	if len(toolResult.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(toolResult.Content))
	}
	for _, want := range []string{
		`"project":{"id":"den-services"`,
		`"task_counts_by_status":{"planned":2,"in_progress":0,"review":0,"blocked":1,"done":1,"cancelled":0}`,
		`"unread_message_count":17`,
	} {
		if !strings.Contains(toolResult.Content[0].Text, want) {
			t.Fatalf("text = %s, missing %s", toolResult.Content[0].Text, want)
		}
	}
}

func TestLocatorComposesSpaceSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/spaces/assistant-home":
			_, _ = w.Write([]byte(`{"id":"assistant-home","name":"Assistant Home","kind":"assistant","visibility":"hidden","created_at":"2026-06-18T01:44:05Z","updated_at":"2026-06-18T01:44:05Z"}`))
		case "/v1/projects/assistant-home/tasks":
			_, _ = w.Write([]byte(`[]`))
		case "/v1/projects/assistant-home/messages/unread-count":
			_, _ = w.Write([]byte(`{"unread_message_count":0}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.String())
		}
	}))
	defer server.Close()

	locator := newProjectSummaryTestLocator(t, server)
	result, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_space",
		Operation: "get_space",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"space_id":"assistant-home","agent":"codex"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if !strings.Contains(string(result.Value), `"project":{"id":"assistant-home"`) {
		t.Fatalf("result = %s", result.Value)
	}
}

func TestProjectSummaryFailsClosedWhenMessagesUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/projects/den-services":
			_, _ = w.Write([]byte(`{"id":"den-services","name":"Den Services","kind":"project"}`))
		case "/v1/projects/den-services/tasks":
			_, _ = w.Write([]byte(`[]`))
		case "/v1/projects/den-services/messages/unread-count":
			http.Error(w, "messages down", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request path %s", r.URL.String())
		}
	}))
	defer server.Close()

	locator := newProjectSummaryTestLocator(t, server)
	_, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName:  "get_project",
		Operation: "get_project",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"den-services","agent":"codex"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure == nil {
		t.Fatal("failure = nil")
	}
	if failure.Backend != "messages" || failure.StatusCode == nil || *failure.StatusCode != http.StatusBadGateway {
		t.Fatalf("failure = %#v", failure)
	}
}

func newProjectSummaryTestLocator(t *testing.T, server *httptest.Server) *Locator {
	t.Helper()
	table, err := NewRouteTable([]Route{
		{
			Operation:       "get_project",
			Backend:         "projects",
			Method:          http.MethodGet,
			Path:            "/v1/projects/{project_id}/summary",
			RequestAdapter:  RequestAdapterMCPProjectSummaryCompose,
			ResponseAdapter: ResponseAdapterMCPToolResultJSON,
		},
		{
			Operation:       "get_space",
			Backend:         "projects",
			Method:          http.MethodGet,
			Path:            "/v1/spaces/{space_id}/summary",
			RequestAdapter:  RequestAdapterMCPProjectSummaryCompose,
			ResponseAdapter: ResponseAdapterMCPToolResultJSON,
		},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	locator, err := NewLocator([]config.BackendConfig{
		testBackend("projects", server.URL),
		testBackend("tasks", server.URL),
		testBackend("messages", server.URL),
	}, table, server.Client())
	if err != nil {
		t.Fatalf("NewLocator() error = %v", err)
	}
	return locator
}
