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
		"den_artifacts":   2,
		"den_channels":    6,
		"den_core":        2,
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

func TestDenCorePhase0AlignmentMigrationTracksCurrentCoreTables(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	var alignment Migration
	for _, migration := range migrations {
		if migration.Schema == "den_core" && migration.Version == 2 {
			alignment = migration
			break
		}
	}
	if alignment.Path == "" {
		t.Fatal("den_core version 2 migration was not discovered")
	}
	for _, want := range []string{
		"create table den_core.capability_invocations",
		"invocation_id text not null unique",
		"create table den_core.collaboration_turns",
		"turn_order integer not null",
		"create table den_core.worker_checkpoints",
		"checkpoint_type text not null",
		"create table den_core.orchestrator_leases",
		"lease_kind text not null default 'project_orchestrator'",
		"create table den_core.pricing_snapshots",
		"snapshot_label text not null",
	} {
		if !strings.Contains(alignment.SQL, want) {
			t.Fatalf("den_core alignment SQL missing %q", want)
		}
	}
}

func TestDenCorePhase0MigrationDocumentsTemporaryCoreOwnedSchema(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	var phase0 Migration
	for _, migration := range migrations {
		if migration.Schema == "den_core" && migration.Version == 1 {
			phase0 = migration
			break
		}
	}
	if phase0.Path == "" {
		t.Fatal("den_core version 1 migration was not discovered")
	}
	for _, want := range []string{
		"Temporary Phase 0 Core-owned schema",
		"create table den_core.projects",
		"create table den_core.tasks",
		"create table den_core.messages",
		"create table den_core.documents",
		"search_vector tsvector generated always as",
		"create table den_core.knowledge_entries",
		"create table den_core.worker_pool_members",
		"create table den_core.capability_definitions",
		"create or replace function den_core.reset_identity_sequences_after_import()",
		"grant select, insert, update, delete on all tables in schema den_core to den_core_app",
	} {
		if !strings.Contains(phase0.SQL, want) {
			t.Fatalf("den_core phase 0 SQL missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"CREATE VIRTUAL TABLE",
		"USING fts5",
		"AUTOINCREMENT",
		"datetime('now')",
	} {
		if strings.Contains(phase0.SQL, forbidden) {
			t.Fatalf("den_core phase 0 SQL should not contain SQLite-only construct %q", forbidden)
		}
	}
}

func TestConversationLegacyProjectLinkMigrationDefinesMappingTable(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	var legacyProjectLink Migration
	for _, migration := range migrations {
		if migration.Schema == "den_channels" && migration.Version == 5 {
			legacyProjectLink = migration
			break
		}
	}
	if legacyProjectLink.Path == "" {
		t.Fatal("den_channels version 5 migration was not discovered")
	}
	for _, want := range []string{
		"create table den_channels.legacy_import_project_links",
		"legacy_is_primary boolean not null default false",
		"legacy_settings jsonb not null default '{}'::jsonb",
		"grant select, insert, update on den_channels.legacy_import_project_links to den_channels_app",
	} {
		if !strings.Contains(legacyProjectLink.SQL, want) {
			t.Fatalf("legacy project link SQL missing %q", want)
		}
	}
}

func TestConversationLegacyImportMigrationDefinesMappingTables(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	var legacyImport Migration
	for _, migration := range migrations {
		if migration.Schema == "den_channels" && migration.Version == 4 {
			legacyImport = migration
			break
		}
	}
	if legacyImport.Path == "" {
		t.Fatal("den_channels version 4 migration was not discovered")
	}
	for _, want := range []string{
		"create table den_channels.legacy_import_channels",
		"create table den_channels.legacy_import_messages",
		"create table den_channels.legacy_import_memberships",
		"create table den_channels.legacy_import_reactions",
		"create table den_channels.legacy_import_read_cursors",
		"m.metadata->>'legacy_source_kind' = 'wake_event'",
		"grant select on den_channels.chat_history to den_observation_app",
	} {
		if !strings.Contains(legacyImport.SQL, want) {
			t.Fatalf("legacy import SQL missing %q", want)
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
