package conversation

import (
	"net/http"
	"time"

	"den-services/shared/api"
	"den-services/shared/health"
	"den-services/shared/postgres"
)

func NewHTTPServer(cfg *Config, buildInfo health.BuildInfo) (*http.Server, error) {
	pool := postgres.MustConnect(cfg.DatabaseURL)
	store := NewStore(pool)
	server, err := NewHTTPServerWithStore(cfg, buildInfo, store)
	if err != nil {
		pool.Close()
		return nil, err
	}
	server.RegisterOnShutdown(pool.Close)
	return server, nil
}

func NewHTTPServerWithStore(cfg *Config, buildInfo health.BuildInfo, store ConversationStore) (*http.Server, error) {
	healthHandler, err := health.HealthHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	versionHandler, err := health.VersionHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	service := NewService(store, wakeTargetResolver(cfg), time.Now, cfg)
	handler := NewHandler(service)

	apiMux := http.NewServeMux()
	handler.RegisterRoutes(apiMux)

	auth, err := api.NewServiceTokenAuth(cfg.ServiceToken)
	if err != nil {
		return nil, err
	}
	root := http.NewServeMux()
	root.Handle("GET /health", healthHandler)
	root.Handle("GET /version", versionHandler)
	root.Handle("/", auth.Middleware(apiMux))

	return &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           root,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}, nil
}

func wakeTargetResolver(cfg *Config) WakeTargetResolver {
	if cfg == nil || !cfg.WakeTargets.Enabled {
		return NoopWakeTargetResolver{}
	}
	return NewRuntimeWakeTargetResolver(
		cfg.WakeTargets.RuntimeBaseURL,
		cfg.WakeTargets.RuntimeServiceToken,
		cfg.WakeTargets.Timeout,
	)
}
