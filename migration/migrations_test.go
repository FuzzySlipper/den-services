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
		"den_channels":    7,
		"den_delivery":    2,
		"den_observation": 3,
		"den_runtime":     3,
	}
	for schema, want := range wantVersions {
		if versionsBySchema[schema] != want {
			t.Fatalf("%s current version = %d, want %d", schema, versionsBySchema[schema], want)
		}
	}
}

func TestObservationRustyChannelReferenceReconciliationMigration(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	var reconcile Migration
	for _, migration := range migrations {
		if migration.Schema == "den_observation" && migration.Version == 3 {
			reconcile = migration
			break
		}
	}
	if reconcile.Path == "" {
		t.Fatal("den_observation version 3 migration was not discovered")
	}
	for _, want := range []string{
		"update den_observation.activity_events",
		"jsonb_set(payload, '{work_ref,channel_id}', to_jsonb(7593), false)",
		"payload #>> '{work_ref,channel_id}' = '43'",
		"create or replace view den_observation.rusty_channel_activity_split_refs",
		"grant select on den_observation.rusty_channel_activity_split_refs to den_observation_app",
	} {
		if !strings.Contains(reconcile.SQL, want) {
			t.Fatalf("observation rusty reconciliation SQL missing %q", want)
		}
	}
	if strings.Contains(reconcile.SQL, "payload #>> '{work_ref,project_id}' = 'rusty-crew'") {
		t.Fatal("observation rusty reconciliation should include channel-only activity refs")
	}
}

func TestRuntimeRustyChannelSubscriptionReconciliationMigration(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	var reconcile Migration
	for _, migration := range migrations {
		if migration.Schema == "den_runtime" && migration.Version == 3 {
			reconcile = migration
			break
		}
	}
	if reconcile.Path == "" {
		t.Fatal("den_runtime version 3 migration was not discovered")
	}
	for _, want := range []string{
		"superseded_channel_id constant bigint := 43",
		"canonical_channel_id constant bigint := 7593",
		"update den_runtime.channel_subscriptions target",
		"update den_runtime.channel_subscription_cursors target_cursor",
		"delete from den_runtime.channel_subscription_cursors source_cursor",
		"update den_runtime.channel_subscription_cursors cursor_row",
		"delete from den_runtime.channel_subscriptions source_subscription",
		"update den_runtime.channel_subscriptions",
		"create or replace view den_runtime.rusty_channel_subscription_split_refs",
		"grant select on den_runtime.rusty_channel_subscription_split_refs to den_runtime_app",
	} {
		if !strings.Contains(reconcile.SQL, want) {
			t.Fatalf("runtime rusty reconciliation SQL missing %q", want)
		}
	}
}

func TestConversationRustyProjectDefaultReconciliationMigration(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover(DefaultFS()) error = %v", err)
	}
	var reconcile Migration
	for _, migration := range migrations {
		if migration.Schema == "den_channels" && migration.Version == 7 {
			reconcile = migration
			break
		}
	}
	if reconcile.Path == "" {
		t.Fatal("den_channels version 7 migration was not discovered")
	}
	for _, want := range []string{
		"canonical_channel_id constant bigint := 7593",
		"rusty_project_id constant text := 'rusty-crew'",
		"update den_channels.channel_messages",
		"update den_channels.channel_memberships",
		"update den_channels.channel_reactions",
		"update den_channels.legacy_import_memberships lim",
		"delete from den_channels.channel_memberships source",
		"update den_channels.channel_read_cursors",
		"delete from den_channels.channel_read_cursors source",
		"update den_channels.channel_project_links",
		"update den_channels.legacy_import_project_links lipl",
		"delete from den_channels.channel_project_links source",
		"update den_channels.legacy_import_messages lim",
		"update den_channels.legacy_import_read_cursors lirc",
		"create or replace view den_channels.project_default_channel_id_splits",
		"grant select on den_channels.project_default_channel_id_splits to den_channels_app",
	} {
		if !strings.Contains(reconcile.SQL, want) {
			t.Fatalf("rusty reconciliation SQL missing %q", want)
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
