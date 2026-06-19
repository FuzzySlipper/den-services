package gateway

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	sharedconfig "den-services/shared/config"

	"gopkg.in/yaml.v3"
)

const migratedFunctionsHeader = "X-Den-Migrated-Functions"

type RouteTable struct {
	routes []Route
}

type Route struct {
	name                string
	pathPattern         string
	legacyURL           *url.URL
	successorURL        *url.URL
	successorAuth       UpstreamAuth
	identityTranslation IdentityTranslation
}

type RouteMatch struct {
	Target              *url.URL
	Auth                UpstreamAuth
	IdentityTranslation IdentityTranslation
	UsesSuccessor       bool
}

type UpstreamAuth struct {
	bearerToken string
}

type routeConfigFile struct {
	Routes []routeFile `yaml:"routes"`
}

type routeFile struct {
	Name                 string                  `yaml:"name"`
	PathPattern          string                  `yaml:"path_pattern"`
	LegacyUpstreamURL    string                  `yaml:"legacy_upstream_url"`
	SuccessorUpstreamURL string                  `yaml:"successor_upstream_url"`
	SuccessorAuth        upstreamAuthFile        `yaml:"successor_auth"`
	IdentityTranslation  identityTranslationFile `yaml:"identity_translation"`
}

type upstreamAuthFile struct {
	BearerToken string `yaml:"bearer_token"`
}

func LoadRouteTable(path string) (*RouteTable, error) {
	values, err := sharedconfig.Load()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading gateway routes %s: %w", path, err)
	}
	var file routeConfigFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing gateway routes %s: %w", path, err)
	}
	return NewRouteTableWithValues(file.Routes, values)
}

func NewRouteTable(files []routeFile) (*RouteTable, error) {
	return NewRouteTableWithValues(files, sharedconfig.FromMap(nil))
}

func NewRouteTableWithValues(files []routeFile, values sharedconfig.Values) (*RouteTable, error) {
	if len(files) == 0 {
		return nil, errors.New("at least one route is required")
	}
	routes := make([]Route, 0, len(files))
	for _, file := range files {
		route, err := newRoute(file, values)
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

func newRoute(file routeFile, values sharedconfig.Values) (Route, error) {
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
	successorAuth, err := newUpstreamAuth(file.SuccessorAuth, values)
	if err != nil {
		return Route{}, fmt.Errorf("route %s: %w", file.Name, err)
	}
	if successorURL != nil && !successorAuth.Enabled() {
		return Route{}, fmt.Errorf("route %s successor_auth.bearer_token is required when successor_upstream_url is set", file.Name)
	}
	if successorURL == nil && successorAuth.Enabled() {
		return Route{}, fmt.Errorf("route %s successor_auth requires successor_upstream_url", file.Name)
	}
	translation, err := newIdentityTranslation(file.IdentityTranslation)
	if err != nil {
		return Route{}, fmt.Errorf("route %s: %w", file.Name, err)
	}
	if translation.Enabled() && successorURL == nil {
		return Route{}, fmt.Errorf("route %s identity_translation requires successor_upstream_url", file.Name)
	}
	return Route{
		name:                file.Name,
		pathPattern:         pattern,
		legacyURL:           legacyURL,
		successorURL:        successorURL,
		successorAuth:       successorAuth,
		identityTranslation: translation,
	}, nil
}

func newUpstreamAuth(file upstreamAuthFile, values sharedconfig.Values) (UpstreamAuth, error) {
	rawToken := strings.TrimSpace(file.BearerToken)
	if rawToken == "" {
		return UpstreamAuth{}, nil
	}
	token, err := values.Expand(rawToken)
	if err != nil {
		return UpstreamAuth{}, fmt.Errorf("expanding successor_auth.bearer_token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return UpstreamAuth{}, errors.New("successor_auth.bearer_token must not be empty")
	}
	return UpstreamAuth{bearerToken: token}, nil
}

func (a UpstreamAuth) Enabled() bool {
	return a.bearerToken != ""
}

func (a UpstreamAuth) Apply(headers httpHeader) {
	if !a.Enabled() {
		return
	}
	headers.Set("Authorization", "Bearer "+a.bearerToken)
}

type httpHeader interface {
	Set(key string, value string)
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

func (t *RouteTable) Match(path string, preferSuccessor bool) (RouteMatch, bool) {
	for _, route := range t.routes {
		if !route.matches(path) {
			continue
		}
		if preferSuccessor && route.successorURL != nil {
			return RouteMatch{
				Target:              cloneURL(route.successorURL),
				Auth:                route.successorAuth,
				IdentityTranslation: route.identityTranslation,
				UsesSuccessor:       true,
			}, true
		}
		return RouteMatch{Target: cloneURL(route.legacyURL)}, true
	}
	return RouteMatch{}, false
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
