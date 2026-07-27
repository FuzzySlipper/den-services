package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"den-services/migration"
	"den-services/shared/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	databaseURL := flags.String("database-url", migrationDatabaseURLFromEnv(), "Postgres URL for the migration role")
	timeout := flags.Duration("timeout", 30*time.Second, "Migration command timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	command := "status"
	if flags.NArg() > 0 {
		command = flags.Arg(0)
	}
	if *databaseURL == "" {
		return postgres.ErrMissingDatabaseURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: *databaseURL})
	if err != nil {
		return err
	}
	defer pool.Close()

	runner, err := migration.NewDefaultRunner(pool)
	if err != nil {
		return err
	}

	switch command {
	case "up":
		applied, err := runner.Up(ctx)
		if err != nil {
			return err
		}
		for _, migration := range applied {
			fmt.Printf("applied %s %03d %s\n", migration.Schema, migration.Version, migration.Name)
		}
		if len(applied) == 0 {
			fmt.Println("no pending migrations")
		}
	case "status":
		statuses, err := runner.Status(ctx)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			fmt.Printf("%s version=%03d pending=%d\n", status.Schema, status.CurrentVersion, status.PendingCount)
		}
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	return nil
}

func migrationDatabaseURLFromEnv() string {
	if value := os.Getenv("DEN_MIGRATION_DATABASE_URL"); value != "" {
		return value
	}
	return os.Getenv("DEN_SERVICES_MIGRATION_DATABASE_URL")
}
