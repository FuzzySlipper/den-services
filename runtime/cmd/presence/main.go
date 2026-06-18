package main

import (
	"context"
	"log"
	"net/http"
	"time"

	runtime "den-services/runtime/internal"
	"den-services/shared/api"
	"den-services/shared/postgres"
)

func main() {
	cfg, err := runtime.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	pool := postgres.MustConnect(cfg.DatabaseURL)
	defer pool.Close()

	store := runtime.NewStore(pool)
	service := runtime.NewRuntimeService(store, time.Now, cfg.Heartbeat.StaleThreshold, cfg.Heartbeat.DeadThreshold)
	handler := runtime.NewHandler(service)

	go runSweepLoop(context.Background(), service, cfg.Heartbeat.SweepInterval)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	var root http.Handler = mux
	if cfg.ServiceToken != "" {
		auth, err := api.NewServiceTokenAuth(cfg.ServiceToken)
		if err != nil {
			log.Fatalf("configuring auth: %v", err)
		}
		root = auth.Middleware(root)
	}

	server := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           root,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}
	log.Printf("runtime presence listening on %s", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("runtime presence server: %v", err)
	}
}

func runSweepLoop(ctx context.Context, service *runtime.RuntimeService, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			staleCount, deadCount, err := service.Sweep(ctx)
			if err != nil {
				log.Printf("runtime sweep failed: %v", err)
				continue
			}
			if staleCount > 0 || deadCount > 0 {
				log.Printf("runtime sweep marked stale=%d dead=%d", staleCount, deadCount)
			}
		}
	}
}
