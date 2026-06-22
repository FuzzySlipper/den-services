package docpublish

import (
	"net/http"
	"time"

	"den-services/shared/api"
	"den-services/shared/health"
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
	store := NewFilePublicationStore(cfg.Records.Path)
	fetcher := NewHTTPDocumentFetcher(cfg.Source.DocumentsBaseURL, cfg.SourceToken, cfg.Source.RequestTimeout)
	publisher := NewGitPublisher(cfg.Blog, cfg.Git.CommandTimeout)
	service, err := NewService(cfg.Blog, store, fetcher, publisher, nowUTC)
	if err != nil {
		return nil, err
	}
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

func nowUTC() time.Time {
	return time.Now().UTC()
}
