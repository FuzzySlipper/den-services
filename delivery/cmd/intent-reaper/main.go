package main

import (
	"context"
	"log"
	"time"

	delivery "den-services/delivery/internal"
	"den-services/shared/postgres"
)

func main() {
	cfg, err := delivery.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	pool := postgres.MustConnect(cfg.DatabaseURL)
	defer pool.Close()

	store := delivery.NewStore(pool)
	runtimeClient := delivery.NewRuntimeClient(cfg.RuntimeServiceURL, cfg.RuntimeHTTP.Timeout)
	service := delivery.NewIntentService(store, runtimeClient, time.Now, cfg.DefaultTTL, cfg.MaxTTL, cfg.PendingTTL, cfg.RunningTTL)

	log.Printf("delivery intent-reaper running every %s", cfg.SweepInterval)
	runReaperLoop(context.Background(), service, cfg.SweepInterval)
}

func runReaperLoop(ctx context.Context, service *delivery.IntentService, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expired, failed, err := service.Reap(ctx)
			if err != nil {
				log.Printf("delivery reaper failed: %v", err)
				continue
			}
			if expired > 0 || failed > 0 {
				log.Printf("delivery reaper transitioned expired=%d failed=%d", expired, failed)
			}
		}
	}
}
