package server

import (
	"net/http"

	"den-services/shared/api"
	"den-services/shared/health"

	"den-services/visual-inspect/internal/config"
)

type RouteRegistrar interface {
	RegisterRoutes(mux *http.ServeMux)
}

func NewHTTPServer(cfg *config.Config, buildInfo health.BuildInfo, registrars ...RouteRegistrar) (*http.Server, error) {
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
	apiHandler, err := protectedAPIHandler(cfg, registrars)
	if err != nil {
		return nil, err
	}
	root.Handle("/", apiHandler)

	return &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           root,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
	}, nil
}

func apiMux(registrars []RouteRegistrar) http.Handler {
	mux := http.NewServeMux()
	for _, registrar := range registrars {
		registrar.RegisterRoutes(mux)
	}
	return mux
}

func protectedAPIHandler(cfg *config.Config, registrars []RouteRegistrar) (http.Handler, error) {
	mux := apiMux(registrars)
	if len(registrars) == 0 {
		return mux, nil
	}
	auth, err := api.NewServiceTokenAuth(cfg.Security.ServiceToken)
	if err != nil {
		return nil, err
	}
	return auth.Middleware(mux), nil
}
