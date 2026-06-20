package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"den-services/shared/health"

	gateway "den-services/gateway/internal"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("gateway %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := gateway.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	routes, err := gateway.LoadRouteTable(cfg.RoutingConfigPath)
	if err != nil {
		log.Fatalf("loading routes: %v", err)
	}
	info, err := buildInfo()
	if err != nil {
		log.Fatalf("building version info: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server, err := gateway.NewHTTPServer(cfg, routes, info, logger)
	if err != nil {
		log.Fatalf("building server: %v", err)
	}

	logger.Info("gateway proxy listening", "addr", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("gateway proxy server: %v", err)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("gateway", version, commit, parsedBuiltAt)
}
