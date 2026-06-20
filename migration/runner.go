package migration

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Runner struct {
	pool       *pgxpool.Pool
	migrations []Migration
}

type AppliedMigration struct {
	Schema    string
	Version   int
	Name      string
	AppliedAt time.Time
}

type SchemaStatus struct {
	Schema         string
	CurrentVersion int
	PendingCount   int
}

func NewRunner(pool *pgxpool.Pool, migrations []Migration) (*Runner, error) {
	if pool == nil {
		return nil, ErrMissingPool
	}
	if err := validateMigrations(migrations); err != nil {
		return nil, err
	}
	return &Runner{
		pool:       pool,
		migrations: append([]Migration(nil), migrations...),
	}, nil
}

func NewDefaultRunner(pool *pgxpool.Pool) (*Runner, error) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		return nil, err
	}
	return NewRunner(pool, migrations)
}

func (r *Runner) Up(ctx context.Context) ([]AppliedMigration, error) {
	grouped := groupBySchema(r.migrations)
	schemas := sortedSchemaNames(grouped)
	var applied []AppliedMigration
	for _, schema := range schemas {
		if err := r.ensureSchema(ctx, schema); err != nil {
			return nil, err
		}
		if err := r.ensureSchemaMigrationTable(ctx, schema); err != nil {
			return nil, err
		}
		currentVersion, err := r.currentVersion(ctx, schema)
		if err != nil {
			return nil, err
		}
		for _, migration := range grouped[schema] {
			if migration.Version <= currentVersion {
				continue
			}
			appliedMigration, err := r.apply(ctx, migration)
			if err != nil {
				return nil, err
			}
			applied = append(applied, appliedMigration)
		}
	}
	return applied, nil
}

func (r *Runner) Status(ctx context.Context) ([]SchemaStatus, error) {
	grouped := groupBySchema(r.migrations)
	schemas := sortedSchemaNames(grouped)
	statuses := make([]SchemaStatus, 0, len(schemas))
	for _, schema := range schemas {
		if err := r.ensureSchema(ctx, schema); err != nil {
			return nil, err
		}
		if err := r.ensureSchemaMigrationTable(ctx, schema); err != nil {
			return nil, err
		}
		currentVersion, err := r.currentVersion(ctx, schema)
		if err != nil {
			return nil, err
		}
		pendingCount := 0
		for _, migration := range grouped[schema] {
			if migration.Version > currentVersion {
				pendingCount++
			}
		}
		statuses = append(statuses, SchemaStatus{
			Schema:         schema,
			CurrentVersion: currentVersion,
			PendingCount:   pendingCount,
		})
	}
	return statuses, nil
}

func (r *Runner) ensureSchema(ctx context.Context, schema string) error {
	_, err := r.pool.Exec(ctx, "create schema if not exists "+pgx.Identifier{schema}.Sanitize())
	if err != nil {
		return fmt.Errorf("creating schema %s: %w", schema, err)
	}
	return nil
}

func (r *Runner) ensureSchemaMigrationTable(ctx context.Context, schema string) error {
	_, err := r.pool.Exec(ctx, fmt.Sprintf(createSchemaMigrationsSQL, pgx.Identifier{schema}.Sanitize()))
	if err != nil {
		return fmt.Errorf("creating %s.schema_migrations: %w", schema, err)
	}
	return nil
}

func (r *Runner) currentVersion(ctx context.Context, schema string) (int, error) {
	query := fmt.Sprintf(currentVersionSQL, pgx.Identifier{schema}.Sanitize())
	var version int
	if err := r.pool.QueryRow(ctx, query).Scan(&version); err != nil {
		return 0, fmt.Errorf("reading %s current migration version: %w", schema, err)
	}
	return version, nil
}

func (r *Runner) apply(ctx context.Context, migration Migration) (AppliedMigration, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AppliedMigration{}, fmt.Errorf("beginning migration %s: %w", migration.Path, err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, migration.SQL); err != nil {
		return AppliedMigration{}, fmt.Errorf("applying migration %s: %w", migration.Path, err)
	}

	appliedAt := time.Now().UTC()
	query := fmt.Sprintf(recordMigrationSQL, pgx.Identifier{migration.Schema}.Sanitize())
	if _, err := tx.Exec(ctx, query, migration.Version, migration.Name, appliedAt); err != nil {
		return AppliedMigration{}, fmt.Errorf("recording migration %s: %w", migration.Path, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return AppliedMigration{}, fmt.Errorf("committing migration %s: %w", migration.Path, err)
	}
	return AppliedMigration{
		Schema:    migration.Schema,
		Version:   migration.Version,
		Name:      migration.Name,
		AppliedAt: appliedAt,
	}, nil
}

func sortedSchemaNames(grouped map[string][]Migration) []string {
	schemas := make([]string, 0, len(grouped))
	for schema := range grouped {
		schemas = append(schemas, schema)
	}
	sort.Strings(schemas)
	return schemas
}

const createSchemaMigrationsSQL = `
create table if not exists %s.schema_migrations (
	version integer primary key,
	name text not null,
	applied_at timestamptz not null
)`

const currentVersionSQL = `
select coalesce(max(version), 0)
from %s.schema_migrations`

const recordMigrationSQL = `
insert into %s.schema_migrations (version, name, applied_at)
values ($1, $2, $3)`
