package backend

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"den-services/mcp/internal/config"
	"den-services/mcp/internal/registry"
)

type Locator struct {
	backends map[string]config.BackendConfig
	client   *Client
	mu       sync.RWMutex
	table    *RouteTable
	states   map[string]State
}

func NewLocator(backends []config.BackendConfig, table *RouteTable, httpClient *http.Client) (*Locator, error) {
	backendMap, err := backendMap(backends)
	if err != nil {
		return nil, err
	}
	if err := validateRouteBackends(table, backendMap); err != nil {
		return nil, err
	}
	states := make(map[string]State, len(backendMap))
	for name := range backendMap {
		states[name] = StateReady
	}
	return &Locator{
		backends: backendMap,
		client:   NewClient(httpClient),
		table:    table,
		states:   states,
	}, nil
}

func NewLocatorFromPath(backends []config.BackendConfig, routeTablePath string, httpClient *http.Client) (*Locator, error) {
	table, err := LoadRouteTable(routeTablePath)
	if err != nil {
		return nil, err
	}
	return NewLocator(backends, table, httpClient)
}

func (l *Locator) Resolve(operation string) (Route, config.BackendConfig, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	route, err := l.table.Resolve(operation)
	if err != nil {
		return Route{}, config.BackendConfig{}, err
	}
	backend, ok := l.backends[route.Backend]
	if !ok {
		return Route{}, config.BackendConfig{}, fmt.Errorf("%w: %s", ErrBackendNotFound, route.Backend)
	}
	return route, backend, nil
}

func (l *Locator) Call(ctx context.Context, call ToolCall) (Result, *Failure, error) {
	route, backend, backends, err := l.resolveForCall(call.Operation)
	if err != nil {
		return Result{}, nil, err
	}
	var result Result
	var failure *Failure
	if registry.TaskDerivesProject(call.Operation) {
		call, failure, err = l.client.withCanonicalTaskProject(ctx, backends, call)
		if failure != nil || err != nil {
			return result, failure, err
		}
	}
	switch route.RequestAdapter {
	case RequestAdapterMCPProjectSummaryCompose:
		result, failure, err = l.client.callProjectSummaryCompose(ctx, backends, route, call)
	case RequestAdapterMCPTaskWorkflowSummaryCompose:
		result, failure, err = l.client.callTaskWorkflowSummaryCompose(ctx, backends, route, call)
	case RequestAdapterMCPTaskContextCompose:
		result, failure, err = l.client.callTaskContextCompose(ctx, backends, route, call)
	default:
		result, failure, err = l.client.Call(ctx, backend, route, call)
	}
	if failure != nil {
		if failure.Retryable {
			l.markState(failure.Backend, StateUnavailable)
			failure.CircuitState = string(StateUnavailable)
		}
		return result, failure, err
	}
	if err != nil {
		return result, failure, err
	}
	l.markState(backend.Name, StateReady)
	return result, nil, nil
}

func (l *Locator) resolveForCall(operation string) (Route, config.BackendConfig, map[string]config.BackendConfig, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	route, err := l.table.Resolve(operation)
	if err != nil {
		return Route{}, config.BackendConfig{}, nil, err
	}
	backend, ok := l.backends[route.Backend]
	if !ok {
		return Route{}, config.BackendConfig{}, nil, fmt.Errorf("%w: %s", ErrBackendNotFound, route.Backend)
	}
	backends := make(map[string]config.BackendConfig, len(l.backends))
	for name, backendConfig := range l.backends {
		backends[name] = backendConfig
	}
	return route, backend, backends, nil
}

func (l *Locator) BackendState(name string) (State, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	state, ok := l.states[name]
	return state, ok
}

func (l *Locator) CheckReadiness(ctx context.Context, name string) *Failure {
	l.mu.RLock()
	backend, ok := l.backends[name]
	l.mu.RUnlock()
	if !ok {
		return &Failure{
			Error:     "den_backend_config_error",
			Retryable: false,
			Backend:   name,
			Message:   ErrBackendNotFound.Error(),
		}
	}
	failure := l.client.CheckReady(ctx, backend)
	if failure != nil {
		l.markState(name, StateUnavailable)
		return failure
	}
	l.markState(name, StateReady)
	return nil
}

func (l *Locator) ReloadRoutes(table *RouteTable) error {
	if err := validateRouteBackends(table, l.backends); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.table = table
	return nil
}

func (l *Locator) ReloadRoutesFromPath(path string) error {
	table, err := LoadRouteTable(path)
	if err != nil {
		return err
	}
	return l.ReloadRoutes(table)
}

func (l *Locator) markState(name string, state State) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.states[name] = state
}

func backendMap(backends []config.BackendConfig) (map[string]config.BackendConfig, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("%w: no backends configured", ErrBackendNotFound)
	}
	result := make(map[string]config.BackendConfig, len(backends))
	for _, backend := range backends {
		if _, exists := result[backend.Name]; exists {
			return nil, fmt.Errorf("duplicate backend %q", backend.Name)
		}
		result[backend.Name] = backend
	}
	return result, nil
}

func validateRouteBackends(table *RouteTable, backends map[string]config.BackendConfig) error {
	for _, route := range table.routes {
		if _, ok := backends[route.Backend]; !ok {
			return fmt.Errorf("%w: %s", ErrBackendNotFound, route.Backend)
		}
	}
	return nil
}
