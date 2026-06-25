package coremigrate

import (
	"strings"
	"testing"
	"time"
)

func TestSortTablesForImportOrdersParentsBeforeChildren(t *testing.T) {
	tables := map[string]targetTable{
		"tasks":    {Name: "tasks"},
		"projects": {Name: "projects"},
		"messages": {Name: "messages"},
	}
	keys := []foreignKey{
		{ChildTable: "tasks", ParentTable: "projects"},
		{ChildTable: "messages", ParentTable: "tasks"},
	}

	got := sortTablesForImport(tables, keys)
	want := []string{"projects", "tasks", "messages"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sortTablesForImport() = %v, want %v", got, want)
	}
}

func TestNormalizeForPostgresConvertsSQLiteScalarTypes(t *testing.T) {
	boolValue, err := normalizeForPostgres(int64(1), targetColumn{DataType: "boolean"})
	if err != nil {
		t.Fatalf("normalizing bool: %v", err)
	}
	if boolValue != true {
		t.Fatalf("boolValue = %v, want true", boolValue)
	}

	timeValue, err := normalizeForPostgres("2026-06-25 01:02:03", targetColumn{DataType: "timestamp with time zone"})
	if err != nil {
		t.Fatalf("normalizing timestamp: %v", err)
	}
	if timeValue.(time.Time).Location() != time.UTC {
		t.Fatalf("timestamp location = %v, want UTC", timeValue.(time.Time).Location())
	}

	jsonValue, err := normalizeForPostgres([]byte(`{"b":2,"a":1}`), targetColumn{DataType: "jsonb"})
	if err != nil {
		t.Fatalf("normalizing json: %v", err)
	}
	if jsonValue != `{"a":1,"b":2}` {
		t.Fatalf("jsonValue = %s, want canonical object", jsonValue)
	}
}

func TestReportMismatchIncludesSchemaAndParityFailures(t *testing.T) {
	report := &Report{
		Tables: []TableReport{
			{Name: "projects", Status: "ok"},
			{Name: "tasks", Status: "checksum_mismatch"},
		},
	}
	if !report.HasMismatch() {
		t.Fatal("HasMismatch() = false, want true")
	}
	text := report.FormatText()
	if !strings.Contains(text, "tasks") || !strings.Contains(text, "checksum_mismatch") {
		t.Fatalf("FormatText() missing mismatch details:\n%s", text)
	}
}

func TestKnownSQLiteExclusionsRecognizeFTSTables(t *testing.T) {
	if !isKnownSQLiteExclusion("documents_fts", "CREATE VIRTUAL TABLE documents_fts USING fts5(title)") {
		t.Fatal("documents_fts was not excluded")
	}
	if !isKnownSQLiteExclusion("documents_fts_data", "") {
		t.Fatal("documents_fts_data was not excluded")
	}
	if isKnownSQLiteExclusion("documents", "CREATE TABLE documents (id integer)") {
		t.Fatal("documents should not be excluded")
	}
}
