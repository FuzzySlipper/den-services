package migration

import (
	"strings"
	"testing"
)

func TestKnowledgeMigrationDiscovered(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	var found *Migration
	for i := range migrations {
		if migrations[i].Schema == "den_knowledge" && migrations[i].Version == 1 {
			found = &migrations[i]
			break
		}
	}
	if found == nil {
		t.Fatal("den_knowledge version 1 migration not discovered")
	}
	for _, fragment := range []string{
		"create table den_knowledge.knowledge_entries",
		"create table den_knowledge.knowledge_entry_tags",
		"create table den_knowledge.knowledge_entry_revisions",
		"create table den_knowledge.knowledge_entry_links",
		"grant select, insert, update, delete on den_knowledge.knowledge_entries to den_knowledge_app",
	} {
		if !strings.Contains(found.SQL, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
}
