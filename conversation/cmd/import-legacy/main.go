package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"den-services/conversation/internal/importer"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	sourcePath := flag.String("source", "", "path to legacy den-channels SQLite database")
	databaseURL := flag.String("database-url", os.Getenv("DEN_CHANNELS_DATABASE_URL"), "Postgres application database URL")
	sourceName := flag.String("source-name", importer.SourceLegacyDenChannels, "stable import source name")
	apply := flag.Bool("apply", false, "write imported rows; omit for dry-run")
	limit := flag.Int("limit", 0, "optional per-table source row limit for tests or sampling")
	timeout := flag.Duration("timeout", 10*time.Minute, "maximum import runtime")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	options := importer.Options{
		SourcePath:  *sourcePath,
		DatabaseURL: *databaseURL,
		SourceName:  *sourceName,
		DryRun:      !*apply,
		Limit:       *limit,
	}

	var destination importer.Destination
	if *apply {
		if options.DatabaseURL == "" {
			log.Fatal("DEN_CHANNELS_DATABASE_URL or --database-url is required with --apply")
		}
		pool, err := pgxpool.New(ctx, options.DatabaseURL)
		if err != nil {
			log.Fatalf("connecting to Postgres: %v", err)
		}
		defer pool.Close()
		postgresDestination, err := importer.NewPostgresDestination(pool)
		if err != nil {
			log.Fatalf("creating Postgres destination: %v", err)
		}
		destination = postgresDestination
	}

	report, err := importer.Run(ctx, options, destination)
	if err != nil {
		log.Fatalf("import failed: %v", err)
	}
	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatalf("encoding report: %v", err)
	}
	fmt.Println(string(output))
}
