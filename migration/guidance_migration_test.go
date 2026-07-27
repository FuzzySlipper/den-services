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
