package backend

import (
	"errors"
	"path/filepath"
	"testing"

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
		if tasksRoute(tool.Name) {
			wantBackend = "tasks"
		}
		if route.Backend != wantBackend {
			t.Fatalf("route %s backend = %q, want %s", tool.Name, route.Backend, wantBackend)
		}
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
