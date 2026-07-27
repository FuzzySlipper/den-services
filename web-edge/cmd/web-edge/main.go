package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"den-services/shared/health"
	"den-services/shared/logging"
	webedge "den-services/web-edge/internal"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("web-edge %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := webedge.Load()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	info, err := buildInfo()
	if err != nil {
		slog.Error("building version info", "error", err)
		os.Exit(1)
	}
	logger := logging.NewLogger(os.Stdout, logging.Config{
		Service: "web-edge",
		Version: version,
	})
	server, err := webedge.NewHTTPServer(cfg, info, logger)
	if err != nil {
		logger.Error("building server", "error", err)
		os.Exit(1)
	}
	logger.Info("web-edge listening", "bind_addr", cfg.Server.ListenAddr, "static_root", cfg.Static.Root)
	if err := server.ListenAndServe(); err != nil {
		logger.Error("web-edge server", "error", err)
		os.Exit(1)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("web-edge", version, commit, parsedBuiltAt)
}
