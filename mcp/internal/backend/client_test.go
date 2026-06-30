package backend

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestClientClassifiesDNSFailureAsRetryableUnavailable(t *testing.T) {
	client := NewClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &net.DNSError{
				Err:  "no such host",
				Name: "den-core.invalid",
			}
		}),
	})

	_, failure, err := client.Call(context.Background(), testBackend("den-core", "http://den-core.invalid"), testRoute("get_task", "den-core"), ToolCall{
		ToolName:  "get_task",
		Operation: "get_task",
		RequestID: json.RawMessage(`1`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure == nil {
		t.Fatal("failure = nil")
	}
	if !failure.Retryable || failure.Error != "den_backend_unavailable" {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestClientAuthFailureWithoutBodyHasConfigHint(t *testing.T) {
	failure := statusFailure("den-core", "get_task", "get_task", http.StatusUnauthorized, nil)
	if failure.Retryable {
		t.Fatal("Retryable = true, want false")
	}
	if failure.Error != "den_backend_auth_failed" {
		t.Fatalf("Error = %q, want den_backend_auth_failed", failure.Error)
	}
	if failure.Message == http.StatusText(http.StatusUnauthorized) {
		t.Fatalf("Message = %q, want config hint", failure.Message)
	}
}

func TestClientNegotiatesStreamableMCPSessionOnDemand(t *testing.T) {
	var sawInitialAccept bool
	var sawSessionHeader bool
	var sawTool string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			sawInitialAccept = true
		}
		var request struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if request.Method == "initialize" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"den-services-mcp-backend-init","result":{"protocolVersion":"2025-11-25"}}` + "\n\n"))
			return
		}
		if request.Method != "tools/call" {
			t.Fatalf("method = %q, want tools/call", request.Method)
		}
		if r.Header.Get("Mcp-Session-Id") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":-32000,"message":"Bad Request: A new session can only be created by an initialize request. Include a valid Mcp-Session-Id header for non-initialize requests."},"id":"","jsonrpc":"2.0"}`))
			return
		}
		if r.Header.Get("Mcp-Session-Id") == "session-1" {
			sawSessionHeader = true
		}
		sawTool = request.Params.Name
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}],"isError":false}}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	route := testRoute("get_task", "den-core")
	route.Path = "/mcp"
	result, failure, err := client.Call(context.Background(), testBackend("den-core", server.URL), route, ToolCall{
		ToolName:  "get_task",
		Operation: "get_task",
		RequestID: json.RawMessage(`1`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if !sawInitialAccept {
		t.Fatal("initial request did not include streamable MCP Accept header")
	}
	if !sawSessionHeader {
		t.Fatal("retried tools/call did not include negotiated Mcp-Session-Id")
	}
	if sawTool != "get_task" {
		t.Fatalf("tool = %q, want get_task", sawTool)
	}
	if !strings.Contains(string(result.Value), `"text":"ok"`) {
		t.Fatalf("result = %s", result.Value)
	}
}

func TestClientCallsProjectsRESTCreateProject(t *testing.T) {
	var sawToken string
	var sawPath string
	var sawMethod string
	var sawBody createProjectBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawToken = r.Header.Get("Authorization")
		sawPath = r.URL.Path
		sawMethod = r.Method
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"project-a","name":"Project A","kind":"project","visibility":"normal","writable":true}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	backendConfig := testBackend("projects", server.URL)
	backendConfig.ServiceToken = "projects-token"
	result, failure, err := client.Call(context.Background(), backendConfig, projectsRoute("create_project", http.MethodPost, "/v1/projects"), ToolCall{
		ToolName:  "create_project",
		Operation: "create_project",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"id":"project-a","name":"Project A","root_path":"/tmp/project-a"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawToken != "Bearer projects-token" {
		t.Fatalf("Authorization = %q, want Bearer projects-token", sawToken)
	}
	if sawMethod != http.MethodPost || sawPath != "/v1/projects" {
		t.Fatalf("request = %s %s, want POST /v1/projects", sawMethod, sawPath)
	}
	if sawBody.ID != "project-a" || sawBody.Name != "Project A" || sawBody.RootPath != "/tmp/project-a" {
		t.Fatalf("body = %#v", sawBody)
	}
	if !strings.Contains(string(result.Value), `"structuredContent":{"id":"project-a"`) {
		t.Fatalf("result = %s", result.Value)
	}
}

func TestClientCallsProjectsRESTListSpacesWithQuery(t *testing.T) {
	var sawRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/spaces" {
			t.Fatalf("request = %s %s, want GET /v1/spaces", r.Method, r.URL.Path)
		}
		sawRawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"id":"assistant-a","name":"Assistant A","kind":"assistant","visibility":"hidden","writable":true}]`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	result, failure, err := client.Call(context.Background(), testBackend("projects", server.URL), projectsRoute("list_spaces", http.MethodGet, "/v1/spaces"), ToolCall{
		ToolName:  "list_spaces",
		Operation: "list_spaces",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"kind":"assistant","include_hidden":true,"include_archived":true}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	for _, want := range []string{"kind=assistant", "include_hidden=true", "include_archived=true"} {
		if !strings.Contains(sawRawQuery, want) {
			t.Fatalf("RawQuery = %q, missing %s", sawRawQuery, want)
		}
	}
	if !strings.Contains(string(result.Value), `"structuredContent":[{"id":"assistant-a"`) {
		t.Fatalf("result = %s", result.Value)
	}
}

func TestClientCallsProjectsRESTUpdateProjectPathParameter(t *testing.T) {
	var sawPath string
	var sawBody updateProjectBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"project/a","name":"Renamed","kind":"project","visibility":"normal","writable":true}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("projects", server.URL), projectsRoute("update_project", http.MethodPatch, "/v1/projects/{project_id}"), ToolCall{
		ToolName:  "update_project",
		Operation: "update_project",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"project/a","name":"Renamed"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/projects/project%2Fa" {
		t.Fatalf("path = %q, want escaped project id", sawPath)
	}
	if sawBody.Name == nil || *sawBody.Name != "Renamed" {
		t.Fatalf("body = %#v", sawBody)
	}
}

func TestClientCallsTasksRESTCreateTask(t *testing.T) {
	var sawPath string
	var sawBody createTaskBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.EscapedPath()
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":101,"project_id":"project/a","title":"Task","status":"planned","priority":3,"created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	result, failure, err := client.Call(context.Background(), testBackend("tasks", server.URL), tasksRouteForTest("create_task", http.MethodPost, "/v1/projects/{project_id}/tasks"), ToolCall{
		ToolName:  "create_task",
		Operation: "create_task",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"project/a","title":"Task","priority":3,"depends_on":"1, 2","tags":"[\"mcp\",\"smoke\"]"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/projects/project%2Fa/tasks" {
		t.Fatalf("path = %q, want escaped project id", sawPath)
	}
	if sawBody.Title != "Task" || len(sawBody.DependsOn) != 2 || sawBody.DependsOn[0] != 1 || len(sawBody.Tags) != 2 {
		t.Fatalf("body = %#v", sawBody)
	}
	if !strings.Contains(string(result.Value), `"structuredContent":{"id":101`) {
		t.Fatalf("result = %s", result.Value)
	}
}

func TestClientCallsTasksRESTListTasksWithFilters(t *testing.T) {
	var sawRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/projects/den-services/tasks" {
			t.Fatalf("request = %s %s, want GET /v1/projects/den-services/tasks", r.Method, r.URL.EscapedPath())
		}
		sawRawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("tasks", server.URL), tasksRouteForTest("list_tasks", http.MethodGet, "/v1/projects/{project_id}/tasks"), ToolCall{
		ToolName:  "list_tasks",
		Operation: "list_tasks",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"den-services","assigned_to":"codex","status":"planned,review","priority":2,"tags":"mcp,cutover"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	for _, want := range []string{"assigned_to=codex", "status=planned%2Creview", "priority=2", "tags=mcp%2Ccutover"} {
		if !strings.Contains(sawRawQuery, want) {
			t.Fatalf("RawQuery = %q, missing %s", sawRawQuery, want)
		}
	}
}

func TestClientCallsTasksRESTRemoveDependencyPath(t *testing.T) {
	var sawPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", r.Method)
		}
		_, _ = w.Write([]byte(`{"message":"Task dependency removed."}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("tasks", server.URL), tasksRouteForTest("remove_dependency", http.MethodDelete, "/v1/tasks/{task_id}/dependencies/{depends_on}"), ToolCall{
		ToolName:  "remove_dependency",
		Operation: "remove_dependency",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":42,"depends_on":41}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/tasks/42/dependencies/41" {
		t.Fatalf("path = %q, want /v1/tasks/42/dependencies/41", sawPath)
	}
}

func TestClientCallsMessagesRESTSendMessage(t *testing.T) {
	var sawToken string
	var sawPath string
	var sawBody sendMessageBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawToken = r.Header.Get("Authorization")
		sawPath = r.URL.EscapedPath()
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":501,"project_id":"project/a","sender":"codex","content":"hello"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	backendConfig := testBackend("messages", server.URL)
	backendConfig.ServiceToken = "messages-token"
	result, failure, err := client.Call(context.Background(), backendConfig, messagesRouteForTest("send_message", http.MethodPost, "/v1/projects/{project_id}/messages"), ToolCall{
		ToolName:  "send_message",
		Operation: "send_message",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"project/a","task_id":7,"sender":"codex","content":"hello","metadata":"{\"kind\":\"smoke\"}"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawToken != "Bearer messages-token" {
		t.Fatalf("Authorization = %q, want Bearer messages-token", sawToken)
	}
	if sawPath != "/v1/projects/project%2Fa/messages" {
		t.Fatalf("path = %q, want escaped project id", sawPath)
	}
	if sawBody.TaskID == nil || *sawBody.TaskID != 7 || sawBody.Metadata["kind"] != "smoke" {
		t.Fatalf("body = %#v", sawBody)
	}
	if !strings.Contains(string(result.Value), `"structuredContent":{"id":501`) {
		t.Fatalf("result = %s", result.Value)
	}
}

func TestClientCallsMessagesRESTMarkReadCSV(t *testing.T) {
	var sawBody markReadBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages/read" {
			t.Fatalf("request = %s %s, want POST /v1/messages/read", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_, _ = w.Write([]byte(`{"marked":2}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("messages", server.URL), messagesRouteForTest("mark_read", http.MethodPost, "/v1/messages/read"), ToolCall{
		ToolName:  "mark_read",
		Operation: "mark_read",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"agent":"reviewer","message_ids":"1, 2"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawBody.Agent != "reviewer" || len(sawBody.MessageIDs) != 2 || sawBody.MessageIDs[1] != 2 {
		t.Fatalf("body = %#v", sawBody)
	}
}

func TestClientCallsMessagesRESTGetUserNotificationsQuery(t *testing.T) {
	var sawRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/user-notifications" {
			t.Fatalf("request = %s %s, want GET /v1/user-notifications", r.Method, r.URL.Path)
		}
		sawRawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	isRead := false
	arguments, err := json.Marshal(map[string]any{
		"project_id":     "den-services",
		"task_id":        3726,
		"sender":         "codex",
		"metadata_type":  "review",
		"urgency":        "normal",
		"read_for_agent": "planner",
		"is_read":        isRead,
		"limit":          10,
		"offset":         5,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("messages", server.URL), messagesRouteForTest("get_user_notifications", http.MethodGet, "/v1/user-notifications"), ToolCall{
		ToolName:  "get_user_notifications",
		Operation: "get_user_notifications",
		RequestID: json.RawMessage(`1`),
		Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	for _, want := range []string{"project_id=den-services", "task_id=3726", "sender=codex", "metadata_type=review", "urgency=normal", "read_for_agent=planner", "is_read=false", "limit=10", "offset=5"} {
		if !strings.Contains(sawRawQuery, want) {
			t.Fatalf("RawQuery = %q, missing %s", sawRawQuery, want)
		}
	}
}

func TestClientCallsMessagesRESTRenderWorkerPromptPath(t *testing.T) {
	var sawPath string
	var sawRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.EscapedPath()
		sawRawQuery = r.URL.RawQuery
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		_, _ = w.Write([]byte(`{"prompt":"work carefully"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("messages", server.URL), messagesRouteForTest("render_worker_prompt", http.MethodGet, "/v1/projects/{project_id}/packets/{message_id}/worker-prompt"), ToolCall{
		ToolName:  "render_worker_prompt",
		Operation: "render_worker_prompt",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"project/a","packet_message_id":501,"completion_reporting_mode":"artifact_reconciled"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/projects/project%2Fa/packets/501/worker-prompt" {
		t.Fatalf("path = %q, want worker prompt path", sawPath)
	}
	if sawRawQuery != "completion_reporting_mode=artifact_reconciled" {
		t.Fatalf("RawQuery = %q, want completion_reporting_mode=artifact_reconciled", sawRawQuery)
	}
}

func TestClientCallsDocumentsRESTStoreDocument(t *testing.T) {
	var sawPath string
	var sawBody storeDocumentBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.EscapedPath()
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":801,"project_id":"project/a","slug":"doc","title":"Doc","content":"body","doc_type":"spec","visibility":"normal"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	result, failure, err := client.Call(context.Background(), testBackend("documents", server.URL), documentsRouteForTest("store_document", http.MethodPost, "/v1/projects/{project_id}/documents"), ToolCall{
		ToolName:  "store_document",
		Operation: "store_document",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"project/a","slug":"doc","title":"Doc","content":"body","doc_type":"spec","tags":"[\"mcp\",\"smoke\"]"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/projects/project%2Fa/documents" {
		t.Fatalf("path = %q, want escaped project id", sawPath)
	}
	if sawBody.Slug != "doc" || len(sawBody.Tags) != 2 || sawBody.Tags[1] != "smoke" {
		t.Fatalf("body = %#v", sawBody)
	}
	if !strings.Contains(string(result.Value), `"structuredContent":{"id":801`) {
		t.Fatalf("result = %s", result.Value)
	}
}

func TestClientCallsDocumentsRESTSearchDocumentsQuery(t *testing.T) {
	var sawRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/projects/den-services/documents/search" {
			t.Fatalf("request = %s %s, want GET /v1/projects/den-services/documents/search", r.Method, r.URL.EscapedPath())
		}
		sawRawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("documents", server.URL), documentsRouteForTest("search_documents", http.MethodGet, "/v1/documents/search"), ToolCall{
		ToolName:  "search_documents",
		Operation: "search_documents",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"den-services","query":"route flip"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	for _, want := range []string{"query=route+flip"} {
		if !strings.Contains(sawRawQuery, want) {
			t.Fatalf("RawQuery = %q, missing %s", sawRawQuery, want)
		}
	}
}

func TestClientCallsReviewRESTCreateRoundTaskScoped(t *testing.T) {
	var sawPath string
	var sawBody reviewRoundBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7,"project_id":"den-services","task_id":3726,"round_number":1}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("review", server.URL), reviewRouteForTest("create_review_round", http.MethodPost, "/v1/tasks/{task_id}/review/rounds"), ToolCall{
		ToolName:  "create_review_round",
		Operation: "create_review_round",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":3726,"requested_by":"codex","branch":"task/3726","base_branch":"main","base_commit":"base","head_commit":"head","tests_run":"[\"go test ./...\"]"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/tasks/3726/review/rounds" {
		t.Fatalf("path = %q, want task-scoped route", sawPath)
	}
	if sawBody.RequestedBy != "codex" || len(sawBody.TestsRun) != 1 || sawBody.TestsRun[0] != "go test ./..." {
		t.Fatalf("body = %#v", sawBody)
	}
}

func TestClientCallsReviewRESTListFindingsWithFilters(t *testing.T) {
	var sawRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/tasks/3726/review/findings" {
			t.Fatalf("request = %s %s, want GET /v1/tasks/3726/review/findings", r.Method, r.URL.Path)
		}
		sawRawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("review", server.URL), reviewRouteForTest("list_review_findings", http.MethodGet, "/v1/tasks/{task_id}/review/findings"), ToolCall{
		ToolName:  "list_review_findings",
		Operation: "list_review_findings",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"task_id":3726,"review_round_id":7,"status":"open,claimed_fixed","resolved":false}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	for _, want := range []string{"review_round_id=7", "status=open%2Cclaimed_fixed", "resolved=false"} {
		if !strings.Contains(sawRawQuery, want) {
			t.Fatalf("RawQuery = %q, missing %s", sawRawQuery, want)
		}
	}
}

func TestClientCallsReviewRESTFindingPathAndPayload(t *testing.T) {
	var sawPath string
	var sawBody respondReviewFindingBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_, _ = w.Write([]byte(`{"id":33,"status":"claimed_fixed"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("review", server.URL), reviewRouteForTest("respond_to_review_finding", http.MethodPost, "/v1/review/findings/{finding_id}/response"), ToolCall{
		ToolName:  "respond_to_review_finding",
		Operation: "respond_to_review_finding",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"review_finding_id":33,"responded_by":"codex","response_notes":"fixed","status":"claimed_fixed"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/review/findings/33/response" {
		t.Fatalf("path = %q, want finding id path", sawPath)
	}
	if sawBody.RespondedBy != "codex" || sawBody.Status != "claimed_fixed" {
		t.Fatalf("body = %#v", sawBody)
	}
}

func TestClientCallsReviewRESTSplitFindingsLists(t *testing.T) {
	var sawBody splitReviewFindingsBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/den-services/tasks/3726/review/findings/split-follow-up" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_, _ = w.Write([]byte(`{"follow_up_task_id":3900}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("review", server.URL), reviewRouteForTest("split_review_findings_to_follow_up", http.MethodPost, "/v1/projects/{project_id}/tasks/{task_id}/review/findings/split-follow-up"), ToolCall{
		ToolName:  "split_review_findings_to_follow_up",
		Operation: "split_review_findings_to_follow_up",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"project_id":"den-services","task_id":3726,"finding_ids":"[33,34]","split_by":"codex","follow_up_tags":"review,followup"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if len(sawBody.FindingIDs) != 2 || sawBody.FindingIDs[1] != 34 || len(sawBody.FollowUpTags) != 2 {
		t.Fatalf("body = %#v", sawBody)
	}
}

func TestClientCallsKnowledgeRESTStoreEntry(t *testing.T) {
	var sawPath string
	var sawBody knowledgeStoreBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":12,"slug":"mcp-routing","title":"MCP Routing","body_markdown":"body","kind":"reference","status":"reviewed","curation_state":"human_curated"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("knowledge", server.URL), knowledgeRouteForTest("den_knowledge_store", http.MethodPost, "/v1/knowledge/entries"), ToolCall{
		ToolName:  "den_knowledge_store",
		Operation: "den_knowledge_store",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"slug":"mcp-routing","title":"MCP Routing","body_markdown":"body","status":"reviewed","tags":["mcp","routing"],"audience":["agents"],"changed_by":"codex"}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/knowledge/entries" {
		t.Fatalf("path = %q, want knowledge entries", sawPath)
	}
	if sawBody.Slug != "mcp-routing" || len(sawBody.Tags) != 2 || sawBody.Audience[0] != "agents" {
		t.Fatalf("body = %#v", sawBody)
	}
}

func TestClientCallsKnowledgeRESTSearchAndGuide(t *testing.T) {
	var sawSearchBody knowledgeSearchBody
	var sawGuideBody knowledgeGuideBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/knowledge/search":
			if err := json.NewDecoder(r.Body).Decode(&sawSearchBody); err != nil {
				t.Fatalf("Decode() search error = %v", err)
			}
			_, _ = w.Write([]byte(`{"results":[],"count":0}`))
		case "/v1/knowledge/guide":
			if err := json.NewDecoder(r.Body).Decode(&sawGuideBody); err != nil {
				t.Fatalf("Decode() guide error = %v", err)
			}
			_, _ = w.Write([]byte(`{"answer":"none","citations":[],"follow_ups":[],"uncertainty":["missing"]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("knowledge", server.URL), knowledgeRouteForTest("den_knowledge_search", http.MethodPost, "/v1/knowledge/search"), ToolCall{
		ToolName:  "den_knowledge_search",
		Operation: "den_knowledge_search",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"query":"routing","required_tags":["mcp"],"any_tags":["go"],"include_unreviewed":true,"limit":5}`),
	})
	if err != nil {
		t.Fatalf("Call() search error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() search failure = %#v", failure)
	}
	includeFollowUps := false
	_, failure, err = client.Call(context.Background(), testBackend("knowledge", server.URL), knowledgeRouteForTest("den_knowledge_guide", http.MethodPost, "/v1/knowledge/guide"), ToolCall{
		ToolName:  "den_knowledge_guide",
		Operation: "den_knowledge_guide",
		RequestID: json.RawMessage(`2`),
		Arguments: json.RawMessage(`{"question":"How route?","required_tags":"mcp","context_budget":500,"include_follow_ups":false}`),
	})
	if err != nil {
		t.Fatalf("Call() guide error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() guide failure = %#v", failure)
	}
	if sawSearchBody.Query != "routing" || !sawSearchBody.IncludeUnreviewed || sawSearchBody.Limit != 5 || sawSearchBody.RequiredTags[0] != "mcp" {
		t.Fatalf("search body = %#v", sawSearchBody)
	}
	if sawGuideBody.Question != "How route?" || sawGuideBody.ContextBudget != 500 || sawGuideBody.IncludeFollowUps == nil || *sawGuideBody.IncludeFollowUps != includeFollowUps {
		t.Fatalf("guide body = %#v", sawGuideBody)
	}
}

func TestClientCallsKnowledgeRESTGetEscapesSlug(t *testing.T) {
	var sawPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.EscapedPath()
		if r.URL.Query().Get("include_archived") != "true" {
			t.Fatalf("query = %s, want include_archived=true", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"id":12,"slug":"topic/a","title":"Topic","body_markdown":"body","kind":"reference","status":"reviewed","curation_state":"human_curated"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	_, failure, err := client.Call(context.Background(), testBackend("knowledge", server.URL), knowledgeRouteForTest("den_knowledge_get", http.MethodGet, "/v1/knowledge/entries/{slug}"), ToolCall{
		ToolName:  "den_knowledge_get",
		Operation: "den_knowledge_get",
		RequestID: json.RawMessage(`1`),
		Arguments: json.RawMessage(`{"slug":"topic/a","include_archived":true}`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if sawPath != "/v1/knowledge/entries/topic%2Fa" {
		t.Fatalf("path = %q, want escaped slug", sawPath)
	}
}

func TestFailureTextIncludesToolCircuitAndStatus(t *testing.T) {
	statusCode := http.StatusBadGateway
	failure := Failure{
		Error:        "den_backend_unavailable",
		Retryable:    true,
		Backend:      "den-core",
		Operation:    "get_task",
		Tool:         "get_task",
		Message:      "bad gateway",
		StatusCode:   &statusCode,
		CircuitState: string(StateUnavailable),
	}

	text := failure.Text()
	for _, want := range []string{`"tool":"get_task"`, `"status_code":502`, `"circuit_state":"unavailable"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("Failure.Text() = %s, missing %s", text, want)
		}
	}
}

func projectsRoute(operation string, method string, path string) Route {
	return Route{
		Operation:       operation,
		Backend:         "projects",
		Method:          method,
		Path:            path,
		RequestAdapter:  RequestAdapterMCPProjectsREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}
}

func tasksRouteForTest(operation string, method string, path string) Route {
	return Route{
		Operation:       operation,
		Backend:         "tasks",
		Method:          method,
		Path:            path,
		RequestAdapter:  RequestAdapterMCPTasksREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}
}

func messagesRouteForTest(operation string, method string, path string) Route {
	return Route{
		Operation:       operation,
		Backend:         "messages",
		Method:          method,
		Path:            path,
		RequestAdapter:  RequestAdapterMCPMessagesREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}
}

func documentsRouteForTest(operation string, method string, path string) Route {
	return Route{
		Operation:       operation,
		Backend:         "documents",
		Method:          method,
		Path:            path,
		RequestAdapter:  RequestAdapterMCPDocumentsREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}
}

func reviewRouteForTest(operation string, method string, path string) Route {
	return Route{
		Operation:       operation,
		Backend:         "review",
		Method:          method,
		Path:            path,
		RequestAdapter:  RequestAdapterMCPReviewREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}
}

func knowledgeRouteForTest(operation string, method string, path string) Route {
	return Route{
		Operation:       operation,
		Backend:         "knowledge",
		Method:          method,
		Path:            path,
		RequestAdapter:  RequestAdapterMCPKnowledgeREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}
}
