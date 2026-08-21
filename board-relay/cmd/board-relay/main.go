package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	boardrelay "den-services/board-relay/internal"
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
		fmt.Printf("board-relay %s %s %s\n", version, commit, builtAt)
		return
	}
	cfg, err := boardrelay.LoadConfig()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		slog.Error("parsing build timestamp", "error", err)
		os.Exit(1)
	}
	info, err := health.NewBuildInfo("board-relay", version, commit, parsedBuiltAt)
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
	service, err := boardrelay.NewService(
		boardrelay.NewStore(pool),
		boardrelay.NewHTTPBoardClient(cfg.BoardBaseURL, cfg.BoardToken, cfg.GitHub.RequestTimeout),
		boardrelay.NewHTTPGitHubClient(cfg.GitHub.APIBaseURL, cfg.GitHub.Token, cfg.GitHub.RequestTimeout),
		cfg.GitHub.Repository,
		time.Now,
		cfg.Sync.MaxReceiptItemURLs,
	)
	if err != nil {
		slog.Error("building relay service", "error", err)
		os.Exit(1)
	}
	server, err := boardrelay.NewHTTPServer(cfg, info, service)
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("board relay listening", "bind_addr", cfg.BindAddr, "repository", cfg.GitHub.Repository)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("board relay server", "error", err)
		os.Exit(1)
	}
}
