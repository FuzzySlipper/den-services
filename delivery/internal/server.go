package delivery

import (
	"net/http"
	"time"

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
	runtimeClient := NewRuntimeClient(cfg.RuntimeServiceURL, cfg.RuntimeHTTP.Timeout)
	service := NewIntentService(store, runtimeClient, time.Now, cfg.DefaultTTL, cfg.MaxTTL, cfg.PendingTTL, cfg.RunningTTL)
	handler := NewHandler(service)

	apiMux := http.NewServeMux()
	handler.RegisterRoutes(apiMux)

	root := http.NewServeMux()
	root.Handle("GET /health", healthHandler)
	root.Handle("GET /version", versionHandler)
	root.Handle("/", apiMux)

	server := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           root,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}
	server.RegisterOnShutdown(pool.Close)
	return server, nil
}
