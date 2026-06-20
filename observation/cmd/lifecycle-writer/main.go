package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"den-services/shared/health"

	observation "den-services/observation/internal"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("observation %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := observation.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	info, err := buildInfo()
	if err != nil {
		log.Fatalf("building version info: %v", err)
	}
	server, err := observation.NewHTTPServer(cfg, info)
	if err != nil {
		log.Fatalf("building server: %v", err)
	}
	log.Printf("observation lifecycle-writer listening on %s", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("observation lifecycle-writer server: %v", err)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("observation", version, commit, parsedBuiltAt)
}
