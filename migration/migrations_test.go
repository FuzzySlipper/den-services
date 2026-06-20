package migration

import (
	"errors"
	"strings"
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
	versionsBySchema := make(map[string]int)
	for _, migration := range migrations {
		if migration.Version > versionsBySchema[migration.Schema] {
			versionsBySchema[migration.Schema] = migration.Version
		}
	}
	wantVersions := map[string]int{
		"den_channels":    3,
		"den_delivery":    2,
		"den_observation": 2,
		"den_runtime":     2,
	}
	for schema, want := range wantVersions {
		if versionsBySchema[schema] != want {
			t.Fatalf("%s current version = %d, want %d", schema, versionsBySchema[schema], want)
		}
	}
}

func TestConversationPilotMigrationDefinesViewContract(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	var pilot Migration
	for _, migration := range migrations {
		if migration.Schema == "den_channels" && migration.Version == 3 {
			pilot = migration
			break
		}
	}
	if pilot.Path == "" {
		t.Fatal("den_channels version 3 migration was not discovered")
	}
	for _, want := range []string{
		"create table den_channels.channels",
		"create table den_channels.channel_messages",
		"create table den_channels.channel_memberships",
		"create table den_channels.channel_reactions",
		"create table den_channels.channel_read_cursors",
		"create or replace view den_channels.chat_history",
		"grant select on den_channels.chat_history to den_observation_app",
	} {
		if !strings.Contains(pilot.SQL, want) {
			t.Fatalf("pilot SQL missing %q", want)
		}
	}
}
