package webedge

import (
	"log/slog"
	"net/http"
	"time"

	"den-services/shared/health"
)

func NewHTTPServer(cfg *Config, buildInfo health.BuildInfo, logger *slog.Logger) (*http.Server, error) {
	static, err := newStaticHandler(cfg.Static)
	if err != nil {
		return nil, err
	}
	healthHandler, err := health.HealthHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	versionHandler, err := health.VersionHandler(buildInfo)
	if err != nil {
		return nil, err
	}
	gatewayProxy := newGatewayProxy(cfg.Gateway, logger)

	mux := http.NewServeMux()
	mux.Handle("GET /health", healthHandler)
	mux.Handle("GET /version", versionHandler)
	mux.Handle("/api/v1/", gatewayProxy)
	mux.HandleFunc("/api/v1", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1" {
			static.ServeHTTP(w, r)
			return
		}
		gatewayProxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/", retiredAPIHandler)
	mux.HandleFunc("/api", retiredAPIHandler)
	for _, prefix := range []string{"/den-core-api", "/den-host-api", "/den-gateway-api"} {
		mux.HandleFunc(prefix+"/", retiredLegacyHandler)
		mux.HandleFunc(prefix, retiredLegacyHandler)
	}
	mux.Handle("/", static)

	return &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           requestLogger(mux, logger),
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}, nil
}

func retiredAPIHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, http.StatusGone, "api_route_retired", "Only /api/v1 routes are supported")
}

func retiredLegacyHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, http.StatusNotFound, "legacy_route_retired", "This legacy Den Web route is retired")
}

func requestLogger(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	})
}
