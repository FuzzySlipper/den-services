package gateway

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const migratedFunctionsHeader = "X-Den-Migrated-Functions"

type RouteTable struct {
	routes []Route
}

type Route struct {
	name         string
	pathPattern  string
	legacyURL    *url.URL
	successorURL *url.URL
}

type routeConfigFile struct {
	Routes []routeFile `yaml:"routes"`
}

type routeFile struct {
	Name                 string `yaml:"name"`
	PathPattern          string `yaml:"path_pattern"`
	LegacyUpstreamURL    string `yaml:"legacy_upstream_url"`
	SuccessorUpstreamURL string `yaml:"successor_upstream_url"`
}

func LoadRouteTable(path string) (*RouteTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading gateway routes %s: %w", path, err)
	}
	var file routeConfigFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing gateway routes %s: %w", path, err)
	}
	return NewRouteTable(file.Routes)
}

func NewRouteTable(files []routeFile) (*RouteTable, error) {
	if len(files) == 0 {
		return nil, errors.New("at least one route is required")
	}
	routes := make([]Route, 0, len(files))
	for _, file := range files {
		route, err := newRoute(file)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	sort.SliceStable(routes, func(i int, j int) bool {
		return len(routes[i].pathPattern) > len(routes[j].pathPattern)
	})
	return &RouteTable{routes: routes}, nil
}

func newRoute(file routeFile) (Route, error) {
	if strings.TrimSpace(file.Name) == "" {
		return Route{}, errors.New("route name is required")
	}
	pattern := strings.TrimSpace(file.PathPattern)
	if !strings.HasPrefix(pattern, "/") {
		return Route{}, fmt.Errorf("route %s path_pattern must start with /", file.Name)
	}
	legacyURL, err := parseUpstreamURL("legacy_upstream_url", file.LegacyUpstreamURL)
	if err != nil {
		return Route{}, fmt.Errorf("route %s: %w", file.Name, err)
	}
	var successorURL *url.URL
	if strings.TrimSpace(file.SuccessorUpstreamURL) != "" {
		successorURL, err = parseUpstreamURL("successor_upstream_url", file.SuccessorUpstreamURL)
		if err != nil {
			return Route{}, fmt.Errorf("route %s: %w", file.Name, err)
		}
	}
	return Route{
		name:         file.Name,
		pathPattern:  pattern,
		legacyURL:    legacyURL,
		successorURL: successorURL,
	}, nil
}

func parseUpstreamURL(name string, raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s must use http or https", name)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%s host is required", name)
	}
	return parsed, nil
}

func (t *RouteTable) Match(path string, preferSuccessor bool) (*url.URL, bool) {
	for _, route := range t.routes {
		if !route.matches(path) {
			continue
		}
		if preferSuccessor && route.successorURL != nil {
			return cloneURL(route.successorURL), true
		}
		return cloneURL(route.legacyURL), true
	}
	return nil, false
}

func (r Route) matches(path string) bool {
	if r.pathPattern == "/" {
		return true
	}
	return path == r.pathPattern || strings.HasPrefix(path, strings.TrimRight(r.pathPattern, "/")+"/")
}

func cloneURL(value *url.URL) *url.URL {
	cloned := *value
	return &cloned
}
