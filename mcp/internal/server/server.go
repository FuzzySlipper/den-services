package server

import (
	"net/http"

	"den-services/shared/api"
	"den-services/shared/health"

	"den-services/mcp/internal/backend"
	"den-services/mcp/internal/config"
	"den-services/mcp/internal/registry"
)

type MCPHandler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func NewHTTPServer(cfg *config.Config, buildInfo health.BuildInfo, mcpHandler MCPHandler) (*http.Server, error) {
	healthHandler, err := health.HealthHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	versionHandler, err := health.VersionHandler(buildInfo)
	if err != nil {
		return nil, err
	}

	root := http.NewServeMux()
	root.Handle("GET /health", healthHandler)
	root.Handle("GET /version", versionHandler)
	protectedMCP, err := protectedMCPHandler(cfg, buildInfo, mcpHandler)
	if err != nil {
		return nil, err
	}
	root.Handle(cfg.Server.MCPEndpointPath, protectedMCP)

	return &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           root,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
	}, nil
}

func protectedMCPHandler(cfg *config.Config, buildInfo health.BuildInfo, handler MCPHandler) (http.Handler, error) {
	if handler == nil {
		defaultRegistry, err := registry.DefaultRegistry()
		if err != nil {
			return nil, err
		}
		locator, err := backend.NewLocatorFromPath(cfg.Backends, cfg.Routes.TablePath, nil)
		if err != nil {
			return nil, err
		}
		handler = NewMCPHandler(defaultRegistry, buildInfo, locator)
	}
	if cfg.Security.AllowUnauthenticatedLocalDev {
		return handler, nil
	}
	auth, err := api.NewServiceTokenAuth(cfg.Security.ServiceToken)
	if err != nil {
		return nil, err
	}
	return auth.Middleware(handler), nil
}
