package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	delivery "den-services/delivery/internal"
	"den-services/shared/postgres"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("delivery %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := delivery.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	pool := postgres.MustConnect(cfg.DatabaseURL)
	defer pool.Close()

	store := delivery.NewStore(pool)
	runtimeClient := delivery.NewRuntimeClient(cfg.RuntimeServiceURL, cfg.RuntimeServiceToken, cfg.RuntimeHTTP.Timeout)
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
