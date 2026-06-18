package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"den-services/migration"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresFixture struct {
	AdminDatabaseURL string
	DatabaseName     string
	DatabaseURL      string
}

func NewPostgresFixture(ctx context.Context, adminDatabaseURL string) (*PostgresFixture, error) {
	if strings.TrimSpace(adminDatabaseURL) == "" {
		return nil, ErrMissingAdminDatabaseURL
	}
	databaseName, err := generateDatabaseName()
	if err != nil {
		return nil, err
	}
	databaseURL, err := databaseURLForName(adminDatabaseURL, databaseName)
	if err != nil {
		return nil, err
	}
	fixture := &PostgresFixture{
		AdminDatabaseURL: adminDatabaseURL,
		DatabaseName:     databaseName,
		DatabaseURL:      databaseURL,
	}
	if err := fixture.create(ctx); err != nil {
		return nil, err
	}
	return fixture, nil
}

func (f *PostgresFixture) RunMigrations(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, f.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting test database: %w", err)
	}
	defer pool.Close()

	runner, err := migration.NewDefaultRunner(pool)
	if err != nil {
		return err
	}
	if _, err := runner.Up(ctx); err != nil {
		return err
	}
	return nil
}

func (f *PostgresFixture) TearDown(ctx context.Context) error {
	adminPool, err := pgxpool.New(ctx, f.AdminDatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting admin database: %w", err)
	}
	defer adminPool.Close()

	identifier := pgx.Identifier{f.DatabaseName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "drop database if exists "+identifier+" with (force)"); err != nil {
		return fmt.Errorf("dropping test database %s: %w", f.DatabaseName, err)
	}
	return nil
}

func (f *PostgresFixture) create(ctx context.Context) error {
	adminPool, err := pgxpool.New(ctx, f.AdminDatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting admin database: %w", err)
	}
	defer adminPool.Close()

	identifier := pgx.Identifier{f.DatabaseName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "create database "+identifier); err != nil {
		return fmt.Errorf("creating test database %s: %w", f.DatabaseName, err)
	}
	return nil
}

func generateDatabaseName() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating test database suffix: %w", err)
	}
	return "den_services_test_" + hex.EncodeToString(bytes), nil
}

func databaseURLForName(rawURL string, databaseName string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing admin database url: %w", err)
	}
	parsed.Path = "/" + databaseName
	return parsed.String(), nil
}
