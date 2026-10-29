package main

import (
	"context"
	"encoding/json"
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
	service := ops.NewService(store, time.Now, cfg.Defaults)
	backup := ops.NewBackup(store, service, cfg.Backup, cfg.DatabasePath, cfg.Defaults, time.Now)
	if len(os.Args) == 2 && os.Args[1] == "backup" {
		os.Exit(runBackupNow(backup, cfg.Backup))
	}
	if cfg.Backup.Enabled() {
		if err := backup.Register(context.Background()); err != nil {
			slog.Error("registering ledger backup job", "error", err)
		}
		go backup.Schedule(context.Background())
	}
	server, err := ops.NewHTTPServer(cfg, info, service)
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

// runBackupNow is "ops backup": one export, restore test and report, run
// beside the service (SQLite serializes the writes).
func runBackupNow(backup *ops.Backup, cfg ops.BackupConfig) int {
	if !cfg.Enabled() {
		slog.Error("backup is not configured: set backup.directory")
		return 2
	}
	ctx := context.Background()
	if err := backup.Register(ctx); err != nil {
		slog.Error("registering ledger backup job", "error", err)
		return 1
	}
	report, err := backup.Run(ctx)
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(encoded))
	if err != nil || report["state"] == "failed" {
		return 1
	}
	return 0
}
