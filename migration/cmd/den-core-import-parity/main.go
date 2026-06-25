package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"den-services/migration/internal/coremigrate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("den-core import parity failed: %v", err)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("den-core-import-parity", flag.ContinueOnError)
	sourceSQLite := flags.String("source-sqlite", "", "Path to a read-only backup/copy of the den-core SQLite database")
	databaseURL := flags.String("postgres-url", databaseURLFromEnv(), "Postgres URL for the migration/staging target")
	applyMigrations := flags.Bool("apply-migrations", false, "Apply embedded migrations before import")
	resetTarget := flags.Bool("reset-target", false, "TRUNCATE den_core target tables before import")
	timeout := flags.Duration("timeout", 10*time.Minute, "Dry-run timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	report, err := coremigrate.Run(ctx, coremigrate.Options{
		SourceSQLitePath: *sourceSQLite,
		DatabaseURL:      *databaseURL,
		ApplyMigrations:  *applyMigrations,
		ResetTarget:      *resetTarget,
	})
	if err != nil {
		return err
	}
	fmt.Println(report.FormatText())
	if report.HasMismatch() {
		return fmt.Errorf("parity mismatch detected")
	}
	return nil
}

func databaseURLFromEnv() string {
	for _, name := range []string{
		"DEN_CORE_MIGRATION_DATABASE_URL",
		"DEN_MIGRATION_DATABASE_URL",
		"DEN_SERVICES_MIGRATION_DATABASE_URL",
	} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
