package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
	_ "time/tzdata" // Job schedules name IANA zones; do not depend on host tzdata.

	ops "den-services/ops/internal"
	"den-services/shared/health"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("ops %s %s %s\n", version, commit, builtAt)
		return
	}
	cfg, err := ops.LoadConfig()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		slog.Error("parsing build timestamp", "error", err)
		os.Exit(1)
	}
	info, err := health.NewBuildInfo("ops", version, commit, parsedBuiltAt)
	if err != nil {
		slog.Error("building version info", "error", err)
		os.Exit(1)
	}
	store, err := ops.OpenStore(context.Background(), cfg.DatabasePath)
	if err != nil {
		slog.Error("opening ops ledger", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	server, err := ops.NewHTTPServer(cfg, info, ops.NewService(store, time.Now, cfg.Defaults))
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("ops listening", "bind_addr", cfg.BindAddr, "database_path", cfg.DatabasePath)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("ops server", "error", err)
		os.Exit(1)
	}
}
