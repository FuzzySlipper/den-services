package migration

import (
	"strings"
	"testing"
)

func TestDocumentsMigrationDiscovered(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	var found *Migration
	for i := range migrations {
		if migrations[i].Schema == "den_documents" && migrations[i].Version == 1 {
			found = &migrations[i]
			break
		}
	}
	if found == nil {
		t.Fatal("den_documents version 1 migration not discovered")
	}
	for _, want := range []string{
		"create table den_documents.documents",
		"create table den_documents.discussion_threads",
		"create table den_documents.discussion_comments",
		"documents_search_vector_idx",
		"grant select, insert, update, delete on den_documents.documents to den_documents_app",
	} {
		if !strings.Contains(found.SQL, want) {
			t.Fatalf("documents migration missing %q", want)
		}
	}
}
