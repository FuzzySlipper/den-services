package observation

import (
	"net/http"
	"time"

	"den-services/shared/api"
	"den-services/shared/health"
	"den-services/shared/postgres"
)

func NewHTTPServer(cfg *Config, buildInfo health.BuildInfo) (*http.Server, error) {
	healthHandler, err := health.HealthHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	versionHandler, err := health.VersionHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	pool := postgres.MustConnect(cfg.DatabaseURL)
	store := NewStore(pool)
	service := NewObservationService(store, time.Now, cfg.DefaultLimit, cfg.MaxLimit)
	handler := NewHandler(service)

	apiMux := http.NewServeMux()
	handler.RegisterRoutes(apiMux)

	var apiRoot http.Handler = apiMux
	if cfg.ServiceToken != "" {
		auth, err := api.NewServiceTokenAuth(cfg.ServiceToken)
		if err != nil {
			pool.Close()
			return nil, err
		}
		apiRoot = auth.Middleware(apiRoot)
	}
	root := http.NewServeMux()
	root.Handle("GET /health", healthHandler)
	root.Handle("GET /version", versionHandler)
	root.Handle("/", apiRoot)

	server := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           root,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}
	server.RegisterOnShutdown(pool.Close)
	return server, nil
}
