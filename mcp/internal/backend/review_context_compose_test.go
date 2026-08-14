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
			_, _ = w.Write([]byte(`{"current_round":{"id":88,"project_id":"den-services","task_id":6608,"round_number":2,"target_kind":"code_diff","base_branch":"main","campaign_children":[{"project_id":"den-services","task_id":6607,"review_round_id":87}],"campaign_repositories":[{"repository":"FuzzySlipper/den-services"}]},"open_findings":[{"id":11,"status":"open"}]}`))
		case "/v1/projects/den-services":
			_, _ = w.Write([]byte(`{"id":"den-services","root_path":"/home/dev/den-services","settings_json":{"repository":"FuzzySlipper/den-services"}}`))
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
	backends := []config.BackendConfig{testBackend("tasks", server.URL), testBackend("review", server.URL), testBackend("messages", server.URL), testBackend("guidance", server.URL), testBackend("projects", server.URL)}
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
	for _, want := range []string{`"schema":"den_review.reviewer_context.v1"`, `"campaign_children"`, `"campaign_repositories"`, `"next_state":"source_review_ready"`, `"repository":"FuzzySlipper/den-services"`, `"root_path":"/home/dev/den-services"`, `"detail_refs"`, `"document_slug":"go-codestyle"`, `"material_digest":"sha256:`} {
		if !strings.Contains(string(first.Value), want) {
			t.Fatalf("review context missing %s: %s", want, first.Value)
		}
	}
	for _, unwanted := range []string{"head_commit", "base_commit", "head_sha", "preferred_diff", "alternate_diff", "delta_base_commit"} {
		if strings.Contains(string(first.Value), unwanted) {
			t.Fatalf("review context retained revision-specific field %q: %s", unwanted, first.Value)
		}
	}
	if strings.Contains(string(first.Value), "this must not be copied") || strings.Contains(string(first.Value), "review_request") == false {
		t.Fatalf("review context leaked task body or packet identity: %s", first.Value)
	}
}

func TestLocatorExpandsReviewContextEvidenceWhenVerbose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/6608":
			_, _ = w.Write([]byte(`{"task":{"id":6608,"project_id":"den-services","title":"Review context","status":"review"}}`))
		case "/v1/projects/den-services/tasks/6608/review/workflow-summary":
			_, _ = w.Write([]byte(`{"current_round":{"id":88,"project_id":"den-services","task_id":6608,"round_number":2},"open_findings":[{"id":11,"status":"open","summary":"bounded finding"}]}`))
		case "/v1/tasks/6608/review/findings":
			_, _ = w.Write([]byte(`[{"id":11,"finding_key":"R6608-1","status":"open","summary":"full finding evidence","notes":"full finding notes"}]`))
		case "/v1/projects/den-services":
			_, _ = w.Write([]byte(`{"id":"den-services","root_path":"/home/dev/den-services","settings_json":{"repository":"FuzzySlipper/den-services"}}`))
		case "/v1/projects/den-services/agent-guidance":
			_, _ = w.Write([]byte(`{"project_id":"den-services","content_markdown":"full guidance evidence","sources":[{"source_scope":"den-services","document_project_id":"den-services","document_slug":"go-codestyle","document_title":"Go style"}]}`))
		case "/v1/projects/den-services/tasks/6608/packets/latest":
			_, _ = w.Write([]byte(`{"id":41,"project_id":"den-services","task_id":6608,"sender":"reviewer","content":"full packet evidence","intent":"review_request","metadata":{"kind":"review_request"},"created_at":"2026-08-03T01:02:03Z"}`))
		case "/v1/projects/den-services/tasks/6608/review/github-check-gates/def":
			_, _ = w.Write([]byte(`{"id":9,"repository":"FuzzySlipper/den-services","status":"passed","required_checks":["verify"]}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	locator, err := newReviewContextTestLocator(t, server)
	if err != nil {
		t.Fatal(err)
	}
	result, failure, err := locator.Call(context.Background(), ToolCall{
		ToolName: "get_review_context", Operation: "get_review_context", RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":6608,"verbose":true}`),
	})
	if err != nil || failure != nil {
		t.Fatalf("verbose Call() = %v, %#v", err, failure)
	}
	for _, want := range []string{`"expanded_findings"`, `full finding evidence`, `"expanded_packets"`, `full packet evidence`, `"expanded_guidance"`, `full guidance evidence`} {
		if !strings.Contains(string(result.Value), want) {
			t.Fatalf("expanded result missing %s: %s", want, result.Value)
		}
	}
}

func TestReviewContextNextStateUsesExplicitGateStatus(t *testing.T) {
	round := reviewContextRound{ID: 1}
	for _, test := range []struct {
		name, status, want string
	}{
		{name: "pending", status: "pending", want: "gate_pending"},
		{name: "passed", status: "passed", want: "source_review_ready"},
		{name: "failed", status: "failed", want: "gate_failed"},
		{name: "timed out", status: "timed_out", want: "gate_failed"},
		{name: "superseded", status: "superseded", want: "round_superseded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := reviewContextNextState("review", round, taskWorkflowReviewSummary{}, &reviewContextGate{Status: test.status}); got != test.want {
				t.Fatalf("next state = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReviewContextNextStateWithoutGateIsReadyForCurrentSource(t *testing.T) {
	if got := reviewContextNextState("review", reviewContextRound{ID: 1}, taskWorkflowReviewSummary{}, nil); got != "source_review_ready" {
		t.Fatalf("next state = %q, want source_review_ready", got)
	}
}

func TestBoundReviewContextRetainsFindingAndPacketPointersWhenCompacting(t *testing.T) {
	response := reviewContextResponse{
		SchemaVersion: 1, Schema: reviewContextSchema, ProjectID: "den-services", TaskID: 6608,
		Task:         reviewContextTask{ID: 6608, ProjectID: "den-services", Status: "review", RootPath: "/home/dev/den-services", RepositoryHandle: "/home/dev/den-services"},
		CurrentRound: &reviewContextRound{ID: 88}, CurrentStatus: "review", NextState: "source_review_ready",
		PriorFindings: []json.RawMessage{json.RawMessage(`{"id":11,"finding_key":"R6608-1","category":"acceptance_gap","status":"open","summary":"` + strings.Repeat("x", 3000) + `"}`)},
		PacketHeaders: map[string]*taskWorkflowPacketHeader{"review_request": {ID: 41, Sender: "reviewer", Metadata: map[string]any{"body": strings.Repeat("x", 3000)}}},
		Guidance:      []taskContextDocHandle{{DocumentProjectID: "den-services", DocumentSlug: "go-codestyle", Notes: strings.Repeat("x", 3000)}},
		DetailRefs:    &reviewContextDetailRefs{Findings: "/v1/tasks/6608/review/findings", Packets: "/v1/projects/den-services/tasks/6608/packets/latest", Guidance: "/v1/projects/den-services/agent-guidance"},
	}
	encoded, err := boundReviewContext(&response)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > reviewContextMaxBytes {
		t.Fatalf("bounded response = %d bytes, want <= %d", len(encoded), reviewContextMaxBytes)
	}
	for _, want := range []string{`"prior_findings"`, `"packet_headers"`, `"detail_refs"`, `"finding_key":"R6608-1"`, `"id":41`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("bounded response missing %s: %s", want, encoded)
		}
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

func newReviewContextTestLocator(t *testing.T, server *httptest.Server) (*Locator, error) {
	t.Helper()
	table, err := NewRouteTable([]Route{{Operation: "get_review_context", Backend: "tasks", Method: http.MethodGet, Path: "/v1/tasks/{task_id}/review-context", RequestAdapter: RequestAdapterMCPReviewContextCompose, ResponseAdapter: ResponseAdapterMCPToolResultJSON}})
	if err != nil {
		return nil, err
	}
	return NewLocator([]config.BackendConfig{testBackend("tasks", server.URL), testBackend("review", server.URL), testBackend("messages", server.URL), testBackend("guidance", server.URL), testBackend("projects", server.URL)}, table, server.Client())
}
