package integration

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestNewPostgresFixtureRequiresAdminDatabaseURL(t *testing.T) {
	_, err := NewPostgresFixture(context.Background(), "")
	if !errors.Is(err, ErrMissingAdminDatabaseURL) {
		t.Fatalf("NewPostgresFixture() error = %v, want %v", err, ErrMissingAdminDatabaseURL)
	}
}

func TestPostgresFixtureCreateMigrateTearDown(t *testing.T) {
	adminURL := os.Getenv("DEN_SERVICES_TEST_ADMIN_DATABASE_URL")
	if adminURL == "" {
		t.Skip("DEN_SERVICES_TEST_ADMIN_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fixture, err := NewPostgresFixture(ctx, adminURL)
	if err != nil {
		t.Fatalf("NewPostgresFixture() error = %v", err)
	}
	defer func() {
		if err := fixture.TearDown(ctx); err != nil {
			t.Fatalf("TearDown() error = %v", err)
		}
	}()

	if err := fixture.RunMigrations(ctx); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
}
