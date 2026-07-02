package backend

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type RouteTable struct {
	routes map[string]Route
}

type routeTableFile struct {
	Routes []routeFile `yaml:"routes"`
}

type routeFile struct {
	Operation       string `yaml:"operation"`
	Backend         string `yaml:"backend"`
	Method          string `yaml:"method"`
	Path            string `yaml:"path"`
	RequestAdapter  string `yaml:"request_adapter"`
	ResponseAdapter string `yaml:"response_adapter"`
}

func LoadRouteTable(path string) (*RouteTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading mcp route table %s: %w", path, err)
	}
	var file routeTableFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing mcp route table %s: %w", path, err)
	}
	return NewRouteTable(file.toRoutes())
}

func NewRouteTable(routes []Route) (*RouteTable, error) {
	if len(routes) == 0 {
		return nil, errors.New("at least one route is required")
	}
	table := &RouteTable{routes: make(map[string]Route, len(routes))}
	for _, route := range routes {
		normalized := normalizeRoute(route)
		if err := validateRoute(normalized); err != nil {
			return nil, err
		}
		if _, exists := table.routes[normalized.Operation]; exists {
			return nil, fmt.Errorf("duplicate route for operation %q", normalized.Operation)
		}
		table.routes[normalized.Operation] = normalized
	}
	return table, nil
}

func (t *RouteTable) Resolve(operation string) (Route, error) {
	route, ok := t.routes[operation]
	if !ok {
		return Route{}, fmt.Errorf("%w: %s", ErrRouteNotFound, operation)
	}
	return route, nil
}

func (f routeTableFile) toRoutes() []Route {
	routes := make([]Route, 0, len(f.Routes))
	for _, route := range f.Routes {
		routes = append(routes, Route{
			Operation:       route.Operation,
			Backend:         route.Backend,
			Method:          route.Method,
			Path:            route.Path,
			RequestAdapter:  route.RequestAdapter,
			ResponseAdapter: route.ResponseAdapter,
		})
	}
	return routes
}

func normalizeRoute(route Route) Route {
	route.Operation = strings.TrimSpace(route.Operation)
	route.Backend = strings.TrimSpace(route.Backend)
	route.Method = strings.ToUpper(strings.TrimSpace(route.Method))
	route.Path = cleanPath(route.Path)
	route.RequestAdapter = strings.TrimSpace(route.RequestAdapter)
	route.ResponseAdapter = strings.TrimSpace(route.ResponseAdapter)
	return route
}

func validateRoute(route Route) error {
	if route.Operation == "" {
		return errors.New("route operation is required")
	}
	if route.Backend == "" {
		return fmt.Errorf("route %s backend is required", route.Operation)
	}
	if route.Method == "" {
		return fmt.Errorf("route %s method is required", route.Operation)
	}
	if !supportedMethod(route.Method) {
		return fmt.Errorf("route %s method %s is not supported", route.Operation, route.Method)
	}
	if route.Path == "" {
		return fmt.Errorf("route %s path is required", route.Operation)
	}
	if !supportedAdapterPair(route.RequestAdapter, route.ResponseAdapter) {
		return fmt.Errorf("%w: %s/%s", ErrUnsupportedAdapter, route.RequestAdapter, route.ResponseAdapter)
	}
	return nil
}

func supportedMethod(method string) bool {
	switch method {
	case http.MethodDelete, http.MethodGet, http.MethodPatch, http.MethodPost:
		return true
	default:
		return false
	}
}

func supportedAdapterPair(requestAdapter string, responseAdapter string) bool {
	switch {
	case requestAdapter == RequestAdapterMCPToolsCall && responseAdapter == ResponseAdapterMCPJSONRPC:
		return true
	case requestAdapter == RequestAdapterMCPProjectsREST && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPProjectSummaryCompose && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPTaskWorkflowSummaryCompose && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPTasksREST && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPMessagesREST && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPDocumentsREST && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPReviewREST && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPKnowledgeREST && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPGuidanceREST && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	case requestAdapter == RequestAdapterMCPLibrarianREST && responseAdapter == ResponseAdapterMCPToolResultJSON:
		return true
	default:
		return false
	}
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}
