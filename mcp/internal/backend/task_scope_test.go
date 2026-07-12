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

func TestTaskScopedReviewCallDerivesCanonicalProject(t *testing.T) {
	var sawReviewPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/42":
			_, _ = w.Write([]byte(`{"task":{"id":42,"project_id":"den-services","title":"Task","status":"planned"}}`))
		case "/v1/projects/den-services/tasks/42/review/github-check-gates":
			sawReviewPath = r.URL.Path
			_, _ = w.Write([]byte(`{"gate_id":7,"status":"pending"}`))
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()

	table, err := NewRouteTable([]Route{{
		Operation: "watch_github_checks", Backend: "review", Method: http.MethodPost,
		Path:           "/v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates",
		RequestAdapter: RequestAdapterMCPReviewREST, ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}})
	if err != nil {
		t.Fatal(err)
	}
	locator, err := NewLocator([]config.BackendConfig{testBackend("tasks", server.URL), testBackend("review", server.URL)}, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName: "watch_github_checks", Operation: "watch_github_checks", RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":42,"repository":"owner/repo","commit_sha":"abc","ref":"main","required_checks":["test"],"requested_by":"root"}`),
	})
	if err != nil || failure != nil {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	if sawReviewPath == "" {
		t.Fatal("review backend was not called with canonical project")
	}
}

func TestTaskScopedCallRejectsRemovedProjectArgument(t *testing.T) {
	table, err := NewRouteTable([]Route{{
		Operation: "get_latest_task_packet", Backend: "messages", Method: http.MethodGet,
		Path:           "/v1/projects/{project_id}/tasks/{task_id}/packets/latest",
		RequestAdapter: RequestAdapterMCPMessagesREST, ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("backend should not be called")
	}))
	defer server.Close()
	locator, err := NewLocator([]config.BackendConfig{testBackend("tasks", server.URL), testBackend("messages", server.URL)}, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName: "get_latest_task_packet", Operation: "get_latest_task_packet",
		Arguments: json.RawMessage(`{"task_id":42,"project_id":"den-services"}`),
	})
	if err == nil || failure != nil || !strings.Contains(err.Error(), `unknown field "project_id"`) {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
}

func TestMarkNotificationModesHaveSinglePurposeBodies(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		arguments string
		want      string
	}{
		{"explicit ids", "mark_notifications_read", `{"agent":"root","notification_ids":"1,2"}`, `{"agent":"root","notification_ids":[1,2]}`},
		{"project scope", "mark_project_notifications_read", `{"agent":"root","project_id":"den-services"}`, `{"agent":"root"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arguments, err := decodeMessagesToolArguments(json.RawMessage(tt.arguments))
			if err != nil {
				t.Fatal(err)
			}
			body, err := messagesRESTRequestBody(tt.operation, arguments)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tt.want {
				t.Fatalf("body = %s, want %s", body, tt.want)
			}
		})
	}
}

func TestEnsureDocumentDiscussionIsTheOnlyCreatingRead(t *testing.T) {
	arguments := documentsToolArguments{ProjectID: "den-services", Slug: "spec"}
	readURL, err := documentsRESTURL("http://example.test", Route{Operation: "get_document_discussion", Path: "/v1/projects/{project_id}/documents/{slug}/discussion"}, arguments)
	if err != nil {
		t.Fatal(err)
	}
	ensureURL, err := documentsRESTURL("http://example.test", Route{Operation: "ensure_document_discussion", Path: "/v1/projects/{project_id}/documents/{slug}/discussion/ensure"}, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readURL, "create_if_missing") || !strings.HasSuffix(ensureURL, "/discussion/ensure") {
		t.Fatalf("readURL = %s, ensureURL = %s", readURL, ensureURL)
	}
}
