package gateway

import (
	"log/slog"
	"net/http"

	"den-services/shared/health"
)

func NewHTTPServer(cfg *Config, routes *RouteTable, buildInfo health.BuildInfo, logger *slog.Logger) (*http.Server, error) {
	healthHandler, err := health.HealthHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	versionHandler, err := health.VersionHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	routes.defaultAuth = CallerAuth{bearerToken: cfg.ServiceToken}
	routes.hasDefaultAuth = true

	mux := http.NewServeMux()
	mux.Handle("GET /health", healthHandler)
	mux.Handle("GET /version", versionHandler)
	mux.Handle("/", NewProxyHandler(routes, logger))

	return &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}, nil
}
