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
