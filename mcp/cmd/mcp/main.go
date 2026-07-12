package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"den-services/shared/health"

	"den-services/mcp/internal/config"
	"den-services/mcp/internal/server"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("mcp %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	info, err := buildInfo()
	if err != nil {
		slog.Error("building version info", "error", err)
		os.Exit(1)
	}
	httpServer, err := server.NewHTTPServer(cfg, info, nil)
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("mcp facade listening", "bind_addr", cfg.Server.ListenAddr, "mcp_endpoint_path", cfg.Server.MCPEndpointPath)
	if err := httpServer.ListenAndServe(); err != nil {
		slog.Error("mcp facade server", "error", err)
		os.Exit(1)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("mcp", version, commit, parsedBuiltAt)
}
