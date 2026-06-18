package migration

import (
	"errors"
	"testing"
	"testing/fstest"
)

func TestDiscoverParsesMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"postgres/den_delivery/001_initial.sql": {Data: []byte("create table example();")},
		"postgres/den_delivery/002_second.sql":  {Data: []byte("select 1;")},
		"postgres/den_runtime/001_initial.sql":  {Data: []byte("select 1;")},
	}

	migrations, err := Discover(fsys)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(migrations) != 3 {
		t.Fatalf("len(migrations) = %d, want 3", len(migrations))
	}
	if migrations[0].Schema != "den_delivery" || migrations[0].Version != 1 || migrations[0].Name != "001_initial" {
		t.Fatalf("first migration = %#v", migrations[0])
	}
}

func TestDiscoverRejectsInvalidName(t *testing.T) {
	fsys := fstest.MapFS{
		"postgres/den_delivery/initial.sql": {Data: []byte("select 1;")},
	}

	_, err := Discover(fsys)
	if !errors.Is(err, ErrInvalidMigrationName) {
		t.Fatalf("Discover() error = %v, want %v", err, ErrInvalidMigrationName)
	}
}

func TestDiscoverRejectsDuplicateVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"postgres/den_delivery/001_initial.sql": {Data: []byte("select 1;")},
		"postgres/den_delivery/001_second.sql":  {Data: []byte("select 1;")},
	}

	_, err := Discover(fsys)
	if !errors.Is(err, ErrDuplicateMigrationVersion) {
		t.Fatalf("Discover() error = %v, want %v", err, ErrDuplicateMigrationVersion)
	}
}

func TestDefaultMigrationsDiscover(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	if len(migrations) != 4 {
		t.Fatalf("len(migrations) = %d, want 4", len(migrations))
	}
}
