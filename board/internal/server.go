package board

import (
	"net/http"
	"strings"

	"den-services/shared/api"
	"den-services/shared/health"
)

func NewHTTPServer(cfg *Config, buildInfo health.BuildInfo, service BoardUseCases) (*http.Server, error) {
	healthHandler, err := health.HealthHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	versionHandler, err := health.VersionHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	httpConfig := cfg.HTTP
	if httpConfig.MaxRequestBodyBytes == 0 {
		httpConfig.MaxRequestBodyBytes = DefaultMaxRequestBodyBytes
	}
	adapterIdentity := strings.TrimSpace(cfg.AdapterIdentity)
	if adapterIdentity == "" {
		adapterIdentity = DefaultAdapterIdentity
	}
	apiMux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(apiMux)
	boundedAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, httpConfig.MaxRequestBodyBytes)
		}
		ctx := WithAuthenticatedAdapterIdentity(r.Context(), adapterIdentity)
		apiMux.ServeHTTP(w, r.WithContext(ctx))
	})
	auth, err := api.NewServiceTokenAuth(cfg.ServiceToken)
	if err != nil {
		return nil, err
	}
	root := http.NewServeMux()
	root.Handle("GET /health", healthHandler)
	root.Handle("GET /version", versionHandler)
	root.Handle("/", auth.Middleware(boundedAPI))
	return &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           root,
		ReadHeaderTimeout: httpConfig.ReadHeaderTimeout,
	}, nil
}
