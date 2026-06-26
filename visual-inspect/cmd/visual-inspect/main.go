package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"den-services/shared/health"

	"den-services/visual-inspect/internal/artifacts"
	"den-services/visual-inspect/internal/config"
	"den-services/visual-inspect/internal/evaluator"
	"den-services/visual-inspect/internal/handler"
	"den-services/visual-inspect/internal/server"
	"den-services/visual-inspect/internal/service"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("visual-inspect %s %s %s\n", version, commit, builtAt)
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
	fetcher := artifacts.NewFetcher(cfg.Artifacts, nil)
	eval := evaluator.NewVisionEvaluator(evaluator.Config{
		Provider:        cfg.LLM.Provider,
		BaseURL:         cfg.LLM.BaseURL,
		APIKey:          cfg.LLM.APIKey,
		Model:           cfg.LLM.Model,
		Temperature:     cfg.LLM.Temperature,
		Timeout:         cfg.LLM.Timeout,
		MaxOutputTokens: cfg.LLM.MaxOutputTokens,
		MaxRetries:      cfg.LLM.MaxRetries,
		DefaultProfile:  cfg.Prompts.DefaultProfile,
		Profiles:        evaluatorProfiles(cfg.Prompts.Profiles),
	}, evaluator.NewOpenAIClient(evaluator.OpenAIClientConfig{
		BaseURL:    cfg.LLM.BaseURL,
		APIKey:     cfg.LLM.APIKey,
		MaxRetries: cfg.LLM.MaxRetries,
	}, &http.Client{Timeout: cfg.LLM.Timeout}))
	evaluateService := service.NewService(cfg, fetcher, eval, slog.Default())
	routeHandler := handler.New(evaluateService)
	httpServer, err := server.NewHTTPServer(cfg, info, routeHandler)
	if err != nil {
		slog.Error("building server", "error", err)
		os.Exit(1)
	}
	slog.Info("visual-inspect listening", "bind_addr", cfg.Server.ListenAddr)
	if err := httpServer.ListenAndServe(); err != nil {
		slog.Error("visual-inspect server", "error", err)
		os.Exit(1)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("visual-inspect", version, commit, parsedBuiltAt)
}

func evaluatorProfiles(profiles map[string]config.PromptProfile) map[string]evaluator.PromptProfile {
	result := make(map[string]evaluator.PromptProfile, len(profiles))
	for name, profile := range profiles {
		result[name] = evaluator.PromptProfile{
			Name:                 name,
			SystemPromptFile:     profile.SystemPromptFile,
			DeveloperPromptFile:  profile.DeveloperPromptFile,
			ResponseSchemaFile:   profile.ResponseSchemaFile,
			MinConfidenceForPass: profile.MinConfidenceForPass,
			MinConfidenceForFail: profile.MinConfidenceForFail,
		}
	}
	return result
}
