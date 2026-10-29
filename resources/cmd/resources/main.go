package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	resources "den-services/resources/internal"
	"den-services/shared/health"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("resources %s %s %s\n", version, commit, builtAt)
		return
	}
	cfg, err := resources.LoadConfig()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		slog.Error("parsing build timestamp", "error", err)
		os.Exit(1)
	}
	info, err := health.NewBuildInfo("resources", version, commit, parsedBuiltAt)
	if err != nil {
		slog.Error("building version info", "error", err)
		os.Exit(1)
	}
	store, err := resources.OpenStore(context.Background(), cfg.DatabasePath)
	if err != nil {
		slog.Error("opening resources database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	// "resources import-board FILE" copies the asset-pipeline Resource Board's
	// SQLite file once, beside the running service.
	if len(os.Args) == 3 && os.Args[1] == "import-board" {
		summary, err := resources.ImportBoard(context.Background(), store, os.Args[2], time.Now())
		if err != nil {
			slog.Error("importing board", "error", err)
			os.Exit(1)
		}
		encoded, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Println(string(encoded))
		return
	}
	service := resources.NewService(store, time.Now, cfg.StaleAfter)
	server, err := resources.NewHTTPServer(cfg, info, service)
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("resources listening", "bind_addr", cfg.BindAddr, "database_path", cfg.DatabasePath)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("resources server", "error", err)
		os.Exit(1)
	}
}
