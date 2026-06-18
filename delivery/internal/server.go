package delivery

import (
	"net/http"
	"time"

	"den-services/shared/api"
	"den-services/shared/postgres"
)

func NewHTTPServer(cfg *Config) (*http.Server, error) {
	pool := postgres.MustConnect(cfg.DatabaseURL)
	store := NewStore(pool)
	runtimeClient := NewRuntimeClient(cfg.RuntimeServiceURL, cfg.RuntimeHTTP.Timeout)
	service := NewIntentService(store, runtimeClient, time.Now, cfg.DefaultTTL, cfg.MaxTTL, cfg.PendingTTL, cfg.RunningTTL)
	handler := NewHandler(service)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	var root http.Handler = mux
	if cfg.ServiceToken != "" {
		auth, err := api.NewServiceTokenAuth(cfg.ServiceToken)
		if err != nil {
			pool.Close()
			return nil, err
		}
		root = auth.Middleware(root)
	}

	server := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           root,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}
	server.RegisterOnShutdown(pool.Close)
	return server, nil
}
