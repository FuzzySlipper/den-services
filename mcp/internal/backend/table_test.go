package backend

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"den-services/mcp/internal/registry"
)

func TestRouteTableResolvesOperation(t *testing.T) {
	table, err := NewRouteTable([]Route{testRoute("get_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	route, err := table.Resolve("get_task")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if route.Backend != "den-core" {
		t.Fatalf("Backend = %q, want den-core", route.Backend)
	}
	if route.Path != "/mcp" {
		t.Fatalf("Path = %q, want /mcp", route.Path)
	}
}

func TestRoutesExampleUsesNarrowGitHubWaitTimeout(t *testing.T) {
	table, err := LoadRouteTable(filepath.Join("..", "..", "routes.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	route, err := table.Resolve("wait_for_github_checks")
	if err != nil {
		t.Fatal(err)
	}
	if route.Timeout != 55*time.Second {
		t.Fatalf("route timeout = %s", route.Timeout)
	}
}

func TestRouteTableRejectsUnsupportedAdapter(t *testing.T) {
	route := testRoute("get_task", "den-core")
	route.RequestAdapter = "unknown"

	_, err := NewRouteTable([]Route{route})
	if !errors.Is(err, ErrUnsupportedAdapter) {
		t.Fatalf("NewRouteTable() error = %v, want %v", err, ErrUnsupportedAdapter)
	}
}

func TestRouteTableReportsMissingRoute(t *testing.T) {
	table, err := NewRouteTable([]Route{testRoute("get_task", "den-core")})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	_, err = table.Resolve("create_task")
	if !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrRouteNotFound)
	}
}

func TestRoutesExampleCoversDefaultRegistry(t *testing.T) {
	reg, err := registry.DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	table, err := LoadRouteTable(filepath.Join("..", "..", "routes.example.yaml"))
	if err != nil {
		t.Fatalf("LoadRouteTable() error = %v", err)
	}
	for _, tool := range reg.Tools() {
		route, err := table.Resolve(tool.Name)
		if err != nil {
			t.Fatalf("Resolve(%s) error = %v", tool.Name, err)
		}
		if route.Operation != tool.Name {
			t.Fatalf("route %s operation = %q, want %q", tool.Name, route.Operation, tool.Name)
		}
		wantBackend := "den-core"
		if projectsSafeSubsetRoute(tool.Name) {
			wantBackend = "projects"
		}
		if projectSummaryComposeRoute(tool.Name) {
			wantBackend = "projects"
		}
		if taskWorkflowSummaryComposeRoute(tool.Name) {
			wantBackend = "tasks"
		}
		if taskContextComposeRoute(tool.Name) {
			wantBackend = "tasks"
		}
		if tasksRoute(tool.Name) {
			wantBackend = "tasks"
		}
		if messagesRoute(tool.Name) {
			wantBackend = "messages"
		}
		if documentsRoute(tool.Name) {
			wantBackend = "documents"
		}
		if reviewRoute(tool.Name) {
			wantBackend = "review"
		}
		if knowledgeRoute(tool.Name) {
			wantBackend = "knowledge"
		}
		if guidanceRoute(tool.Name) {
			wantBackend = "guidance"
		}
		if librarianRoute(tool.Name) {
			wantBackend = "librarian"
		}
		if route.Backend != wantBackend {
			t.Fatalf("route %s backend = %q, want %s", tool.Name, route.Backend, wantBackend)
		}
	}
}

func TestRoutesExampleRoutesHiddenAdminDeleteSpaceToProjects(t *testing.T) {
	table, err := LoadRouteTable(filepath.Join("..", "..", "routes.example.yaml"))
	if err != nil {
		t.Fatalf("LoadRouteTable() error = %v", err)
	}
	route, err := table.Resolve("delete_space")
	if err != nil {
		t.Fatalf("Resolve(delete_space) error = %v", err)
	}
	if route.Backend != "projects" || route.Path != "/v1/admin/spaces/{space_id}/delete" || route.RequestAdapter != RequestAdapterMCPProjectsREST {
		t.Fatalf("delete_space route = %#v", route)
	}
}

func TestRouteTableAllowsTasksRESTAdapter(t *testing.T) {
	route := Route{
		Operation:       "remove_dependency",
		Backend:         "tasks",
		Method:          "DELETE",
		Path:            "/v1/tasks/{task_id}/dependencies/{depends_on}",
		RequestAdapter:  RequestAdapterMCPTasksREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("remove_dependency")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Method != "DELETE" {
		t.Fatalf("Method = %q, want DELETE", resolved.Method)
	}
}

func TestRouteTableAllowsProjectsRESTAdapter(t *testing.T) {
	route := Route{
		Operation:       "list_projects",
		Backend:         "projects",
		Method:          "GET",
		Path:            "/v1/projects",
		RequestAdapter:  RequestAdapterMCPProjectsREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("list_projects")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Method != "GET" {
		t.Fatalf("Method = %q, want GET", resolved.Method)
	}
}

func TestRouteTableAllowsGuidanceRESTAdapter(t *testing.T) {
	route := Route{
		Operation:       "get_agent_guidance",
		Backend:         "guidance",
		Method:          "GET",
		Path:            "/v1/projects/{project_id}/agent-guidance",
		RequestAdapter:  RequestAdapterMCPGuidanceREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("get_agent_guidance")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Backend != "guidance" {
		t.Fatalf("Backend = %q, want guidance", resolved.Backend)
	}
}

func TestRouteTableAllowsLibrarianRESTAdapter(t *testing.T) {
	route := Route{
		Operation:       "query_librarian",
		Backend:         "librarian",
		Method:          "POST",
		Path:            "/v1/projects/{project_id}/librarian/query",
		RequestAdapter:  RequestAdapterMCPLibrarianREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("query_librarian")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Backend != "librarian" {
		t.Fatalf("Backend = %q, want librarian", resolved.Backend)
	}
}

func TestRouteTableAllowsProjectSummaryComposeAdapter(t *testing.T) {
	route := Route{
		Operation:       "get_project",
		Backend:         "projects",
		Method:          "GET",
		Path:            "/v1/projects/{project_id}/summary",
		RequestAdapter:  RequestAdapterMCPProjectSummaryCompose,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("get_project")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Method != "GET" {
		t.Fatalf("Method = %q, want GET", resolved.Method)
	}
}

func TestRouteTableAllowsTaskWorkflowSummaryComposeAdapter(t *testing.T) {
	route := Route{
		Operation:       "get_task_workflow_summary",
		Backend:         "tasks",
		Method:          "GET",
		Path:            "/v1/tasks/{task_id}/workflow-summary",
		RequestAdapter:  RequestAdapterMCPTaskWorkflowSummaryCompose,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("get_task_workflow_summary")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Backend != "tasks" {
		t.Fatalf("Backend = %q, want tasks", resolved.Backend)
	}
}

func TestRouteTableAllowsTaskContextComposeAdapter(t *testing.T) {
	route := Route{
		Operation:       "get_task_context",
		Backend:         "tasks",
		Method:          http.MethodGet,
		Path:            "/v1/tasks/{task_id}/context",
		RequestAdapter:  RequestAdapterMCPTaskContextCompose,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}
	if _, err := NewRouteTable([]Route{route}); err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
}

func TestRouteTableAllowsMessagesRESTAdapter(t *testing.T) {
	route := Route{
		Operation:       "send_message",
		Backend:         "messages",
		Method:          "POST",
		Path:            "/v1/projects/{project_id}/messages",
		RequestAdapter:  RequestAdapterMCPMessagesREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("send_message")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Method != "POST" {
		t.Fatalf("Method = %q, want POST", resolved.Method)
	}
}

func TestRouteTableAllowsDocumentsRESTAdapter(t *testing.T) {
	route := Route{
		Operation:       "store_document",
		Backend:         "documents",
		Method:          "POST",
		Path:            "/v1/projects/{project_id}/documents",
		RequestAdapter:  RequestAdapterMCPDocumentsREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("store_document")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Method != "POST" {
		t.Fatalf("Method = %q, want POST", resolved.Method)
	}
}

func TestRouteTableAllowsReviewRESTAdapter(t *testing.T) {
	route := Route{
		Operation:       "create_review_round",
		Backend:         "review",
		Method:          "POST",
		Path:            "/v1/tasks/{task_id}/review/rounds",
		RequestAdapter:  RequestAdapterMCPReviewREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("create_review_round")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Method != "POST" {
		t.Fatalf("Method = %q, want POST", resolved.Method)
	}
}

func TestRouteTableAllowsKnowledgeRESTAdapter(t *testing.T) {
	route := Route{
		Operation:       "den_knowledge_search",
		Backend:         "knowledge",
		Method:          "POST",
		Path:            "/v1/knowledge/search",
		RequestAdapter:  RequestAdapterMCPKnowledgeREST,
		ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}

	table, err := NewRouteTable([]Route{route})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	resolved, err := table.Resolve("den_knowledge_search")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Method != "POST" {
		t.Fatalf("Method = %q, want POST", resolved.Method)
	}
}

func projectsSafeSubsetRoute(operation string) bool {
	switch operation {
	case "create_project",
		"list_projects",
		"update_project",
		"create_space",
		"list_spaces",
		"update_space_visibility",
		"archive_space":
		return true
	default:
		return false
	}
}

func projectSummaryComposeRoute(operation string) bool {
	switch operation {
	case "get_project", "get_space":
		return true
	default:
		return false
	}
}

func taskWorkflowSummaryComposeRoute(operation string) bool {
	switch operation {
	case "get_task_workflow_summary":
		return true
	default:
		return false
	}
}

func taskContextComposeRoute(operation string) bool {
	return operation == "get_task_context"
}

func tasksRoute(operation string) bool {
	switch operation {
	case "create_task",
		"list_tasks",
		"get_task",
		"update_task",
		"next_task",
		"add_dependency",
		"remove_dependency":
		return true
	default:
		return false
	}
}

func messagesRoute(operation string) bool {
	switch operation {
	case "send_message",
		"get_messages",
		"wait_for_messages",
		"get_thread",
		"mark_read",
		"send_user_notification",
		"get_user_notifications",
		"mark_notifications_read",
		"get_latest_task_packet",
		"render_worker_prompt":
		return true
	default:
		return false
	}
}

func documentsRoute(operation string) bool {
	switch operation {
	case "store_document",
		"get_document",
		"list_documents",
		"search_documents",
		"delete_document",
		"update_document_visibility",
		"archive_document_preflight",
		"query_archived_documents",
		"get_document_discussion",
		"comment_on_document",
		"list_discussion_threads",
		"get_discussion_thread",
		"create_discussion_comment",
		"update_discussion_thread":
		return true
	default:
		return false
	}
}

func reviewRoute(operation string) bool {
	switch operation {
	case "create_review_round",
		"list_review_rounds",
		"list_review_findings",
		"request_review",
		"post_review_findings",
		"split_review_findings_to_follow_up",
		"create_review_finding",
		"set_review_verdict",
		"respond_to_review_finding",
		"set_review_finding_status",
		"await_github_checks",
		"watch_github_checks",
		"get_github_check_gate",
		"wait_for_github_checks":
		return true
	default:
		return false
	}
}

func knowledgeRoute(operation string) bool {
	switch operation {
	case "den_knowledge_search",
		"den_knowledge_get",
		"den_knowledge_guide",
		"den_knowledge_store":
		return true
	default:
		return false
	}
}

func guidanceRoute(operation string) bool {
	switch operation {
	case "get_agent_guidance",
		"list_agent_guidance_entries",
		"add_agent_guidance_entry",
		"delete_agent_guidance_entry":
		return true
	default:
		return false
	}
}

func librarianRoute(operation string) bool {
	switch operation {
	case "query_librarian":
		return true
	default:
		return false
	}
}

func testRoute(operation string, backend string) Route {
	return Route{
		Operation:       operation,
		Backend:         backend,
		Method:          "POST",
		Path:            "mcp",
		RequestAdapter:  RequestAdapterMCPToolsCall,
		ResponseAdapter: ResponseAdapterMCPJSONRPC,
	}
}
