package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	handoff "den-services/handoff/internal"
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
		fmt.Printf("handoff %s %s %s\n", version, commit, builtAt)
		return
	}
	cfg, err := handoff.LoadConfig()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		slog.Error("parsing build timestamp", "error", err)
		os.Exit(1)
	}
	info, err := health.NewBuildInfo("handoff", version, commit, parsedBuiltAt)
	if err != nil {
		slog.Error("building version info", "error", err)
		os.Exit(1)
	}
	pool, err := postgres.Connect(context.Background(), postgres.PoolConfig{DatabaseURL: cfg.DatabaseURL})
	if err != nil {
		slog.Error("connecting postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	server, err := handoff.NewHTTPServer(cfg, info, handoff.NewService(handoff.NewStore(pool), time.Now))
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("handoff listening", "bind_addr", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("handoff server", "error", err)
		os.Exit(1)
	}
}
