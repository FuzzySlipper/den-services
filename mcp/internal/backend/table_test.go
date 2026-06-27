package backend

import (
	"errors"
	"testing"
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
