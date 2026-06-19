package main

import (
	"fmt"
	"log"
	"os"
	"time"

	delivery "den-services/delivery/internal"
	"den-services/shared/health"
)

var (
	version = "dev"                  //nolint:gochecknoglobals
	commit  = "unknown"              //nolint:gochecknoglobals
	builtAt = "1970-01-01T00:00:00Z" //nolint:gochecknoglobals
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("delivery %s %s %s\n", version, commit, builtAt)
		return
	}

	cfg, err := delivery.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	info, err := buildInfo()
	if err != nil {
		log.Fatalf("building version info: %v", err)
	}
	server, err := delivery.NewHTTPServer(cfg, info)
	if err != nil {
		log.Fatalf("building server: %v", err)
	}
	log.Printf("delivery intent-lifecycle listening on %s", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("delivery intent-lifecycle server: %v", err)
	}
}

func buildInfo() (health.BuildInfo, error) {
	parsedBuiltAt, err := time.Parse(time.RFC3339, builtAt)
	if err != nil {
		return health.BuildInfo{}, fmt.Errorf("parsing builtAt: %w", err)
	}
	return health.NewBuildInfo("delivery", version, commit, parsedBuiltAt)
}
