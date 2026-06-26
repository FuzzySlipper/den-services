package server

import (
	"net/http"

	"den-services/shared/api"
	"den-services/shared/health"

	"den-services/mcp/internal/config"
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
	protectedMCP, err := protectedMCPHandler(cfg, mcpHandler)
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

func protectedMCPHandler(cfg *config.Config, handler MCPHandler) (http.Handler, error) {
	if handler == nil {
		handler = PlaceholderMCPHandler{}
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

type PlaceholderMCPHandler struct{}

type PlaceholderResponse struct {
	Error     string `json:"error"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message"`
}

func (PlaceholderMCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "MCP endpoint placeholder accepts POST only")
		return
	}
	api.WriteJSON(w, http.StatusNotImplemented, PlaceholderResponse{
		Error:     "mcp_tool_registry_not_implemented",
		Retryable: false,
		Message:   "den-services/mcp shell is healthy, but MCP tool routing is not implemented yet",
	})
}
