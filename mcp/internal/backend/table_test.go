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
		if route.Backend != "den-core" {
			t.Fatalf("route %s backend = %q, want den-core", tool.Name, route.Backend)
		}
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
