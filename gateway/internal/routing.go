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
	routes         []Route
	defaultAuth    CallerAuth
	hasDefaultAuth bool
}

type Route struct {
	name                string
	pathPattern         string
	methods             map[string]struct{}
	legacyURL           *url.URL
	successorURL        *url.URL
	successorMode       SuccessorMode
	successorAuth       UpstreamAuth
	callerAuth          CallerAuth
	legacyCallerAuth    CallerAuth
	successorCallerAuth CallerAuth
	identityTranslation IdentityTranslation
}

type RouteMatch struct {
	Target              *url.URL
	Auth                UpstreamAuth
	CallerAuth          CallerAuth
	IdentityTranslation IdentityTranslation
	UsesSuccessor       bool
}

type UpstreamAuth struct {
	bearerToken string
}

type SuccessorMode string

const (
	SuccessorModeHeader SuccessorMode = "header"
	SuccessorModeAlways SuccessorMode = "always"
)

type routeConfigFile struct {
	Routes []routeFile `yaml:"routes"`
}

type routeFile struct {
	Name                 string                  `yaml:"name"`
	PathPattern          string                  `yaml:"path_pattern"`
	Methods              []string                `yaml:"methods"`
	LegacyUpstreamURL    string                  `yaml:"legacy_upstream_url"`
	SuccessorUpstreamURL string                  `yaml:"successor_upstream_url"`
	SuccessorMode        string                  `yaml:"successor_mode"`
	SuccessorAuth        upstreamAuthFile        `yaml:"successor_auth"`
	CallerAuth           callerAuthFile          `yaml:"caller_auth"`
	LegacyCallerAuth     callerAuthFile          `yaml:"legacy_caller_auth"`
	SuccessorCallerAuth  callerAuthFile          `yaml:"successor_caller_auth"`
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
	return NewRouteTableWithValuesAndDefaultAuth(files, values, CallerAuth{})
}

func NewRouteTableWithValuesAndDefaultAuth(files []routeFile, values sharedconfig.Values, defaultAuth CallerAuth) (*RouteTable, error) {
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
	if err := validateUniqueRouteCoverage(routes); err != nil {
		return nil, err
	}
	sort.SliceStable(routes, func(i int, j int) bool {
		if len(routes[i].pathPattern) != len(routes[j].pathPattern) {
			return len(routes[i].pathPattern) > len(routes[j].pathPattern)
		}
		return len(routes[i].methods) > len(routes[j].methods)
	})
	return &RouteTable{routes: routes, defaultAuth: defaultAuth, hasDefaultAuth: defaultAuth.Enabled()}, nil
}

func validateUniqueRouteCoverage(routes []Route) error {
	names := make(map[string]struct{}, len(routes))
	for i, route := range routes {
		if _, exists := names[route.name]; exists {
			return fmt.Errorf("duplicate route name %s", route.name)
		}
		names[route.name] = struct{}{}
		for _, existing := range routes[:i] {
			if route.pathPattern == existing.pathPattern && methodsOverlap(route.methods, existing.methods) {
				return fmt.Errorf("route %s overlaps route %s for path %s", route.name, existing.name, route.pathPattern)
			}
		}
	}
	return nil
}

func methodsOverlap(left map[string]struct{}, right map[string]struct{}) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	for method := range left {
		if _, exists := right[method]; exists {
			return true
		}
	}
	return false
}

func newRoute(file routeFile, values sharedconfig.Values) (Route, error) {
	if strings.TrimSpace(file.Name) == "" {
		return Route{}, errors.New("route name is required")
	}
	pattern := strings.TrimSpace(file.PathPattern)
	if !strings.HasPrefix(pattern, "/") {
		return Route{}, fmt.Errorf("route %s path_pattern must start with /", file.Name)
	}
	methods, err := parseMethods(file.Name, file.Methods)
	if err != nil {
		return Route{}, err
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
	successorMode, err := parseSuccessorMode(file.Name, file.SuccessorMode, successorURL != nil)
	if err != nil {
		return Route{}, err
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
	callerAuth, err := newCallerAuth(file.CallerAuth, values)
	if err != nil {
		return Route{}, fmt.Errorf("route %s: %w", file.Name, err)
	}
	legacyCallerAuth, err := newCallerAuth(file.LegacyCallerAuth, values)
	if err != nil {
		return Route{}, fmt.Errorf("route %s: %w", file.Name, err)
	}
	successorCallerAuth, err := newCallerAuth(file.SuccessorCallerAuth, values)
	if err != nil {
		return Route{}, fmt.Errorf("route %s: %w", file.Name, err)
	}
	if successorCallerAuth.Enabled() && successorURL == nil {
		return Route{}, fmt.Errorf("route %s successor_caller_auth requires successor_upstream_url", file.Name)
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
		methods:             methods,
		legacyURL:           legacyURL,
		successorURL:        successorURL,
		successorMode:       successorMode,
		successorAuth:       successorAuth,
		callerAuth:          callerAuth,
		legacyCallerAuth:    legacyCallerAuth,
		successorCallerAuth: successorCallerAuth,
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

func parseMethods(routeName string, rawMethods []string) (map[string]struct{}, error) {
	if len(rawMethods) == 0 {
		return nil, nil
	}
	methods := make(map[string]struct{}, len(rawMethods))
	for _, raw := range rawMethods {
		method := strings.ToUpper(strings.TrimSpace(raw))
		if method == "" {
			return nil, fmt.Errorf("route %s methods must not contain empty values", routeName)
		}
		if _, exists := methods[method]; exists {
			return nil, fmt.Errorf("route %s duplicate method %s", routeName, method)
		}
		methods[method] = struct{}{}
	}
	return methods, nil
}

func parseSuccessorMode(routeName string, raw string, hasSuccessor bool) (SuccessorMode, error) {
	mode := SuccessorMode(strings.ToLower(strings.TrimSpace(raw)))
	if mode == "" {
		mode = SuccessorModeHeader
	}
	switch mode {
	case SuccessorModeHeader:
		return mode, nil
	case SuccessorModeAlways:
		if !hasSuccessor {
			return "", fmt.Errorf("route %s successor_mode always requires successor_upstream_url", routeName)
		}
		return mode, nil
	default:
		return "", fmt.Errorf("route %s successor_mode must be header or always", routeName)
	}
}

func (t *RouteTable) Match(method string, path string, preferSuccessor bool) (RouteMatch, bool) {
	for _, route := range t.routes {
		if !route.matches(method, path) {
			continue
		}
		if route.usesSuccessor(preferSuccessor) {
			return RouteMatch{
				Target:              cloneURL(route.successorURL),
				Auth:                route.successorAuth,
				CallerAuth:          t.authForRoute(route, true),
				IdentityTranslation: route.identityTranslation,
				UsesSuccessor:       true,
			}, true
		}
		return RouteMatch{Target: cloneURL(route.legacyURL), CallerAuth: t.authForRoute(route, false)}, true
	}
	return RouteMatch{}, false
}

func (t *RouteTable) authForRoute(route Route, usesSuccessor bool) CallerAuth {
	if usesSuccessor && route.successorCallerAuth.Enabled() {
		return route.successorCallerAuth
	}
	if !usesSuccessor && route.legacyCallerAuth.Enabled() {
		return route.legacyCallerAuth
	}
	if route.callerAuth.Enabled() {
		return route.callerAuth
	}
	if t.hasDefaultAuth {
		return t.defaultAuth
	}
	return CallerAuth{}
}

func (r Route) matches(method string, path string) bool {
	if len(r.methods) > 0 {
		if _, ok := r.methods[strings.ToUpper(strings.TrimSpace(method))]; !ok {
			return false
		}
	}
	if strings.Contains(r.pathPattern, "{") {
		return pathTemplateMatches(r.pathPattern, path)
	}
	if r.pathPattern == "/" {
		return true
	}
	return path == r.pathPattern || strings.HasPrefix(path, strings.TrimRight(r.pathPattern, "/")+"/")
}

func pathTemplateMatches(pattern string, path string) bool {
	pattern = strings.TrimRight(pattern, "/")
	path = strings.TrimRight(path, "/")
	if pattern == "" {
		pattern = "/"
	}
	if path == "" {
		path = "/"
	}
	patternSegments := splitPathSegments(pattern)
	pathSegments := splitPathSegments(path)
	if len(pathSegments) < len(patternSegments) {
		return false
	}
	for i, patternSegment := range patternSegments {
		if isPathVariable(patternSegment) {
			if pathSegments[i] == "" {
				return false
			}
			continue
		}
		if pathSegments[i] != patternSegment {
			return false
		}
	}
	return true
}

func splitPathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func isPathVariable(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && len(segment) > 2
}

func (r Route) usesSuccessor(preferSuccessor bool) bool {
	if r.successorURL == nil {
		return false
	}
	return r.successorMode == SuccessorModeAlways || preferSuccessor
}

func cloneURL(value *url.URL) *url.URL {
	cloned := *value
	return &cloned
}
