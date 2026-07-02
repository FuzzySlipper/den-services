package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	librarian "den-services/librarian/internal"
	"den-services/shared/health"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("librarian %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := librarian.LoadConfig()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}
	info, err := buildInfo()
	if err != nil {
		slog.Error("building version info", "error", err)
		os.Exit(1)
	}
	clients := librarian.NewHTTPSourceClients(librarian.SourceClientConfig{
		ProjectsBaseURL:  cfg.ProjectsBaseURL,
		ProjectsToken:    cfg.ProjectsToken,
		TasksBaseURL:     cfg.TasksBaseURL,
		TasksToken:       cfg.TasksToken,
		MessagesBaseURL:  cfg.MessagesBaseURL,
		MessagesToken:    cfg.MessagesToken,
		DocumentsBaseURL: cfg.DocumentsBaseURL,
		DocumentsToken:   cfg.DocumentsToken,
		KnowledgeBaseURL: cfg.KnowledgeBaseURL,
		KnowledgeToken:   cfg.KnowledgeToken,
		RequestTimeout:   cfg.Upstreams.RequestTimeout,
	})
	service := librarian.NewService(clients, cfg.DefaultBudget)
	server, err := librarian.NewHTTPServer(cfg, info, service)
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("librarian listening", "bind_addr", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("librarian server", "error", err)
		os.Exit(1)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("librarian", version, commit, parsedBuiltAt)
}
