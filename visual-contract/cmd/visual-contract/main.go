package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"den-services/shared/health"

	visualcontract "den-services/visual-contract/internal"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("visual-contract %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := visualcontract.LoadConfig()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	info, err := buildInfo()
	if err != nil {
		slog.Error("building version info", "error", err)
		os.Exit(1)
	}
	server, err := visualcontract.NewHTTPServer(cfg, info)
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("visual-contract listening", "bind_addr", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("visual-contract server", "error", err)
		os.Exit(1)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("visual-contract", version, commit, parsedBuiltAt)
}
