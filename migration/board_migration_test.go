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

func TestBoardIdempotencyMigration(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("discover migrations: %v", err)
	}
	for _, migration := range migrations {
		if migration.Schema != "den_board" || migration.Version != 3 {
			continue
		}
		for _, fragment := range []string{
			"create table den_board.idempotency_keys",
			"request_key text primary key",
			"operation text not null",
		} {
			if !strings.Contains(migration.SQL, fragment) {
				t.Fatalf("board idempotency migration missing %q", fragment)
			}
		}
		return
	}
	t.Fatal("den_board version 3 migration not discovered")
}
