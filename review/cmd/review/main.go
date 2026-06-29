package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	review "den-services/review/internal"
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
		fmt.Printf("review %s %s %s\n", version, commit, builtAt)
		return
	}
	cfg, err := review.LoadConfig()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	info, err := buildInfo()
	if err != nil {
		slog.Error("building version info", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: cfg.DatabaseURL})
	if err != nil {
		slog.Error("connecting postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	service := review.NewService(
		review.NewStore(pool),
		review.NewProjectScopeClient(cfg.ProjectsBaseURL, cfg.ProjectsToken),
		review.NewTaskClient(cfg.TasksBaseURL, cfg.TasksToken),
		review.NewMessageClient(cfg.MessagesBaseURL, cfg.MessagesToken),
		time.Now,
	)
	server, err := review.NewHTTPServer(cfg, info, service)
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("review listening", "bind_addr", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("review server", "error", err)
		os.Exit(1)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("review", version, commit, parsedBuiltAt)
}
