package migration

import (
	"strings"
	"testing"
)

func TestGuidanceMigrationNormalizesLegacyTimestampTypes(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("discover migrations: %v", err)
	}

	for i := range migrations {
		if migrations[i].Schema != "den_guidance" || migrations[i].Version != 1 {
			continue
		}
		for _, fragment := range []string{
			"nullif(created_at::text, '')::timestamptz",
			"nullif(updated_at::text, '')::timestamptz",
		} {
			if !strings.Contains(migrations[i].SQL, fragment) {
				t.Fatalf("guidance migration missing %q", fragment)
			}
		}
		return
	}

	t.Fatal("den_guidance version 1 migration not discovered")
}

func TestGuidanceAudienceNormalizationMigrationIsSafeAndForwardOnly(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("discover migrations: %v", err)
	}

	for i := range migrations {
		if migrations[i].Schema != "den_guidance" || migrations[i].Version != 2 {
			continue
		}
		for _, fragment := range []string{
			"jsonb_array_length(audience) = 1",
			"parsed_audience := candidate.raw_audience::jsonb",
			"jsonb_typeof(parsed_audience) = 'array'",
			"jsonb_typeof(value) <> 'string'",
			"btrim(value #>> '{}') = ''",
			"set audience = nullif(parsed_audience, '[]'::jsonb)",
			"exception",
			"when others then",
		} {
			if !strings.Contains(migrations[i].SQL, fragment) {
				t.Fatalf("guidance audience migration missing %q", fragment)
			}
		}
		return
	}

	t.Fatal("den_guidance version 2 migration not discovered")
}

func TestKnowledgeBindingsMigrationKeepsCanonicalKnowledgeOutOfGuidance(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("discover migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.Schema != "den_guidance" || migration.Version != 3 {
			continue
		}
		for _, fragment := range []string{
			"create table den_guidance.knowledge_bindings", "target_kind = 'knowledge'", "target_ref", "scope_kind in ('global', 'project', 'task', 'agent_profile', 'capability')", "read_policy in ('inline', 'must_read', 'on_demand', 'latent')", "unique (target_kind, target_ref, scope_kind, scope_ref)", "shadows_global",
		} {
			if !strings.Contains(migration.SQL, fragment) {
				t.Fatalf("knowledge binding migration missing %q", fragment)
			}
		}
		for _, forbidden := range []string{"body_markdown", "summary", "tags", "provenance", "curation_state", "replacement_slug", "revision"} {
			if strings.Contains(migration.SQL, forbidden) {
				t.Fatalf("knowledge binding migration must not duplicate canonical knowledge %q", forbidden)
			}
		}
		return
	}
	t.Fatal("den_guidance version 3 migration not discovered")
}
