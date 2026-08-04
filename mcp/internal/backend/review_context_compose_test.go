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

func TestLocatorComposesBoundedReviewContextWithoutTaskBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/6608":
			_, _ = w.Write([]byte(`{"task":{"id":6608,"project_id":"den-services","title":"Review context","description":"this must not be copied into the reviewer context","status":"review"}}`))
		case "/v1/projects/den-services/tasks/6608/review/workflow-summary":
			_, _ = w.Write([]byte(`{"current_round":{"id":88,"project_id":"den-services","task_id":6608,"round_number":2,"base_commit":"abc","head_commit":"def","delta_base_commit":"abc"},"open_findings":[{"id":11,"status":"open"}]}`))
		case "/v1/projects/den-services/tasks/6608/review/github-check-gates/def":
			_, _ = w.Write([]byte(`{"id":9,"status":"passed","required_checks":["verify"]}`))
		case "/v1/projects/den-services/tasks/6608/packets/latest":
			_, _ = w.Write([]byte(`{"id":41,"project_id":"den-services","task_id":6608,"sender":"reviewer","intent":"review_request","metadata":{"kind":"review_request"},"created_at":"2026-08-03T01:02:03Z"}`))
		case "/v1/projects/den-services/agent-guidance":
			_, _ = w.Write([]byte(`{"sources":[{"source_scope":"den-services","document_project_id":"den-services","document_slug":"go-codestyle","document_title":"Go style"}]}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	table, err := NewRouteTable([]Route{{Operation: "get_review_context", Backend: "tasks", Method: http.MethodGet, Path: "/v1/tasks/{task_id}/review-context", RequestAdapter: RequestAdapterMCPReviewContextCompose, ResponseAdapter: ResponseAdapterMCPToolResultJSON}})
	if err != nil {
		t.Fatal(err)
	}
	backends := []config.BackendConfig{testBackend("tasks", server.URL), testBackend("review", server.URL), testBackend("messages", server.URL), testBackend("guidance", server.URL)}
	locator, err := NewLocator(backends, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ToolName: "get_review_context", Operation: "get_review_context", RequestID: json.RawMessage(`1`), Arguments: json.RawMessage(`{"task_id":6608}`)}
	first, failure, err := locator.Call(context.Background(), call)
	if err != nil || failure != nil {
		t.Fatalf("first Call() = %v, %#v", err, failure)
	}
	second, failure, err := locator.Call(context.Background(), call)
	if err != nil || failure != nil {
		t.Fatalf("second Call() = %v, %#v", err, failure)
	}
	if string(first.Value) != string(second.Value) {
		t.Fatalf("review context is not deterministic:\n%s\n%s", first.Value, second.Value)
	}
	if len(first.Value) > reviewContextMaxBytes {
		t.Fatalf("review context bytes = %d, want <= %d", len(first.Value), reviewContextMaxBytes)
	}
	for _, want := range []string{`"schema":"den_review.reviewer_context.v1"`, `"head_commit":"def"`, `"status":"passed"`, `"document_slug":"go-codestyle"`, `"material_digest":"sha256:`} {
		if !strings.Contains(string(first.Value), want) {
			t.Fatalf("review context missing %s: %s", want, first.Value)
		}
	}
	if strings.Contains(string(first.Value), "this must not be copied") || strings.Contains(string(first.Value), "review_request") == false {
		t.Fatalf("review context leaked task body or packet identity: %s", first.Value)
	}
}

func TestLocatorReturnsTypedReviewContextUnavailableWithoutCurrentRound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/6608":
			_, _ = w.Write([]byte(`{"task":{"id":6608,"project_id":"den-services","title":"No round","status":"in_progress"}}`))
		case "/v1/projects/den-services/tasks/6608/review/workflow-summary":
			_, _ = w.Write([]byte(`{"current_round":null}`))
		default:
			t.Fatalf("unexpected optional request %s", r.URL.String())
		}
	}))
	defer server.Close()
	table, err := NewRouteTable([]Route{{Operation: "get_review_context", Backend: "tasks", Method: http.MethodGet, Path: "/v1/tasks/{task_id}/review-context", RequestAdapter: RequestAdapterMCPReviewContextCompose, ResponseAdapter: ResponseAdapterMCPToolResultJSON}})
	if err != nil {
		t.Fatal(err)
	}
	backends := []config.BackendConfig{testBackend("tasks", server.URL), testBackend("review", server.URL), testBackend("messages", server.URL)}
	locator, err := NewLocator(backends, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, failure, err := locator.Call(context.Background(), ToolCall{ToolName: "get_review_context", Operation: "get_review_context", RequestID: json.RawMessage(`1`), Arguments: json.RawMessage(`{"task_id":6608}`)})
	if err != nil || failure != nil {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	if !strings.Contains(string(result.Value), `"error_code":"review_context_unavailable"`) || !strings.Contains(string(result.Value), `"reason":"no_current_round"`) {
		t.Fatalf("missing typed unavailable result: %s", result.Value)
	}
}
