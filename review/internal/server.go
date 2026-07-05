package review

import (
	"net/http"

	"den-services/shared/api"
	"den-services/shared/health"
)

func NewHTTPServer(cfg *Config, info health.BuildInfo, service ReviewUseCases) (*http.Server, error) {
	healthHandler, err := health.HealthHandler(info)
	if err != nil {
		return nil, err
	}
	versionHandler, err := health.VersionHandler(info)
	if err != nil {
		return nil, err
	}
	apiMux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(apiMux)
	root := http.NewServeMux()
	root.Handle("GET /health", healthHandler)
	root.Handle("GET /version", versionHandler)
	if cfg.AllowUnauthenticatedLocalDev {
		root.Handle("/", apiMux)
		return &http.Server{
			Addr:              cfg.BindAddr,
			Handler:           root,
			ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		}, nil
	}
	auth, err := api.NewServiceTokenAuth(cfg.ServiceToken)
	if err != nil {
		return nil, err
	}
	root.Handle("/", auth.Middleware(apiMux))
	return &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           root,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}, nil
}
