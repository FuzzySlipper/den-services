package artifacts

import (
	"net/http"

	"den-services/shared/api"
	"den-services/shared/health"
)

func NewHTTPServer(cfg *Config, buildInfo health.BuildInfo, service ArtifactUseCases) (*http.Server, error) {
	healthHandler, err := health.HealthHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	versionHandler, err := health.VersionHandler(buildInfo)
	if err != nil {
		return nil, err
	}

	apiMux := http.NewServeMux()
	NewHandler(service, cfg).RegisterRoutes(apiMux)
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
