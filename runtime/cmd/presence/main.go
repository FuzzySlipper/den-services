package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	runtime "den-services/runtime/internal"
	"den-services/shared/api"
	"den-services/shared/health"
	"den-services/shared/postgres"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("runtime %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := runtime.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	info, err := buildInfo()
	if err != nil {
		log.Fatalf("building version info: %v", err)
	}
	healthHandler, err := health.HealthHandler(info)
	if err != nil {
		log.Fatalf("building health handler: %v", err)
	}
	versionHandler, err := health.VersionHandler(info)
	if err != nil {
		log.Fatalf("building version handler: %v", err)
	}

	pool := postgres.MustConnect(cfg.DatabaseURL)
	defer pool.Close()

	store := runtime.NewStore(pool)
	service := runtime.NewRuntimeService(store, time.Now, cfg.Heartbeat.StaleThreshold, cfg.Heartbeat.DeadThreshold)
	handler := runtime.NewHandler(service)

	go runSweepLoop(context.Background(), service, cfg.Heartbeat.SweepInterval)

	apiMux := http.NewServeMux()
	handler.RegisterRoutes(apiMux)

	var apiRoot http.Handler = apiMux
	if cfg.ServiceToken != "" {
		auth, err := api.NewServiceTokenAuth(cfg.ServiceToken)
		if err != nil {
			log.Fatalf("configuring auth: %v", err)
		}
		apiRoot = auth.Middleware(apiRoot)
	}
	root := http.NewServeMux()
	root.Handle("GET /health", healthHandler)
	root.Handle("GET /version", versionHandler)
	root.Handle("/", apiRoot)

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

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("runtime", version, commit, parsedBuiltAt)
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
