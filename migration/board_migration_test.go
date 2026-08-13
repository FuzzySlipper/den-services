package migration

import (
	"strings"
	"testing"
)

func TestBoardFullTextSearchMigration(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("discover migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.Schema != "den_board" || migration.Version != 2 {
			continue
		}
		for _, fragment := range []string{
			"generated always as",
			"to_tsvector('english'",
			"using gin(search_vector)",
			"where status = 'active'",
		} {
			if !strings.Contains(migration.SQL, fragment) {
				t.Fatalf("board FTS migration missing %q", fragment)
			}
		}
		return
	}
	t.Fatal("den_board version 2 migration not discovered")
}
