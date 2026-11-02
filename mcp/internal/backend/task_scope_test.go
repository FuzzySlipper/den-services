package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestGetDocumentDiscussionRejectsRemovedCreateIfMissingBeforeDispatch(t *testing.T) {
	var backendRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendRequests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	table, err := NewRouteTable([]Route{{
		Operation: "get_document_discussion", Backend: "documents", Method: http.MethodGet,
		Path:           "/v1/projects/{project_id}/documents/{slug}/discussion",
		RequestAdapter: RequestAdapterMCPDocumentsREST, ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}})
	if err != nil {
		t.Fatal(err)
	}
	locator, err := NewLocator([]config.BackendConfig{testBackend("documents", server.URL)}, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName: "get_document_discussion", Operation: "get_document_discussion",
		Arguments: json.RawMessage(`{"project_id":"den-services","slug":"spec","create_if_missing":true}`),
	})
	if err == nil || failure != nil || !strings.Contains(err.Error(), `unknown field "create_if_missing"`) {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	if got := backendRequests.Load(); got != 0 {
		t.Fatalf("backend requests = %d, want 0", got)
	}
}

func TestSubmitForReviewAndProjectToolsDeriveTheTaskProject(t *testing.T) {
	var taskReads atomic.Int32
	var submitted map[string]any
	var messagesPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/tasks/42":
			taskReads.Add(1)
			_, _ = w.Write([]byte(`{"task":{"id":42,"project_id":"den-services","title":"Task","status":"in_progress"}}`))
		case r.URL.Path == "/v1/review-submissions":
			_ = json.NewDecoder(r.Body).Decode(&submitted)
			_, _ = w.Write([]byte(`{"review_round_id":9}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			messagesPath = r.URL.Path
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()
	table, err := NewRouteTable([]Route{{
		Operation: "submit_task_for_review", Backend: "crew-review", Method: http.MethodPost, Path: "/v1/review-submissions",
		RequestAdapter: RequestAdapterMCPReviewREST, ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}, {
		Operation: "get_messages", Backend: "messages", Method: http.MethodGet, Path: "/v1/projects/{project_id}/messages",
		RequestAdapter: RequestAdapterMCPMessagesREST, ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}})
	if err != nil {
		t.Fatal(err)
	}
	locator, err := NewLocator([]config.BackendConfig{
		testBackend("tasks", server.URL), testBackend("crew-review", server.URL), testBackend("messages", server.URL),
	}, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	call := func(operation, arguments string) {
		t.Helper()
		if _, failure, err := locator.Call(context.Background(), ToolCall{
			ToolName: operation, Operation: operation, RequestID: json.RawMessage(`1`), Arguments: json.RawMessage(arguments),
		}); err != nil || failure != nil {
			t.Fatalf("%s: Call() = %v, %#v", operation, err, failure)
		}
	}

	call("submit_task_for_review", `{"task_id":42,"repository":"owner/repo","ref":"main","required_checks":[],"review_summary_md":"s"}`)
	if submitted["project_id"] != "den-services" {
		t.Fatalf("submission = %v", submitted)
	}
	// Sessions started on the old schema still send the task's own project.
	call("submit_task_for_review", `{"project_id":"den-services","task_id":42,"repository":"owner/repo","ref":"main","required_checks":[],"review_summary_md":"s"}`)
	if _, _, err := locator.Call(context.Background(), ToolCall{
		ToolName: "submit_task_for_review", Operation: "submit_task_for_review", RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"den-web","task_id":42,"repository":"owner/repo","ref":"main","required_checks":[],"review_summary_md":"s"}`),
	}); err == nil || !strings.Contains(err.Error(), "not task 42's project") {
		t.Fatalf("mismatched project_id: err = %v", err)
	}
	call("get_messages", `{"task_id":42}`)
	if messagesPath != "/v1/projects/den-services/messages" {
		t.Fatalf("messages path = %q", messagesPath)
	}
	// A project-scoped call that names its project needs no task read.
	before := taskReads.Load()
	call("get_messages", `{"project_id":"other","task_id":42}`)
	if messagesPath != "/v1/projects/other/messages" || taskReads.Load() != before {
		t.Fatalf("explicit project: path = %q, task reads %d -> %d", messagesPath, before, taskReads.Load())
	}
}
