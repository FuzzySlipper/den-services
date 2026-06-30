package documents

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestScanThreadAllowsNullableTargetAnchor(t *testing.T) {
	now := time.Date(2026, 6, 30, 1, 0, 0, 0, time.UTC)
	row := fakeRow{
		values: []any{
			int64(42),
			TargetTypeDocument,
			"den-services",
			nil,
			"architecture-guidelines",
			nil,
			"default",
			"architecture-guidelines discussion",
			ThreadStatusOpen,
			"smoke-3725",
			nil,
			nil,
			nil,
			nil,
			now,
			now,
		},
	}

	thread, err := scanThread(row)
	if err != nil {
		t.Fatalf("scanThread() error = %v", err)
	}
	if thread.TargetAnchor != "" {
		t.Fatalf("TargetAnchor = %q, want empty string", thread.TargetAnchor)
	}
	if thread.Summary != "" || thread.ResolutionSummary != "" {
		t.Fatalf("nullable summaries = %q/%q, want empty strings", thread.Summary, thread.ResolutionSummary)
	}
	if thread.TargetSlug != "architecture-guidelines" || thread.TargetProjectID != "den-services" {
		t.Fatalf("target = %#v", thread)
	}
}

func TestScanDocumentAllowsNullableSummary(t *testing.T) {
	now := time.Date(2026, 6, 30, 1, 0, 0, 0, time.UTC)
	row := fakeRow{
		values: []any{
			int64(7),
			"den-services",
			"doc",
			"Doc",
			"Body",
			DocTypeNote,
			VisibilityNormal,
			[]byte(`["smoke-3834"]`),
			nil,
			now,
			now,
		},
	}

	doc, err := scanDocument(row)
	if err != nil {
		t.Fatalf("scanDocument() error = %v", err)
	}
	if doc.Summary() != "" {
		t.Fatalf("Summary = %q, want empty string", doc.Summary())
	}
	if tags := doc.Tags(); len(tags) != 1 || tags[0] != "smoke-3834" {
		t.Fatalf("Tags = %#v", tags)
	}
}

func TestScanDocumentSummaryAllowsNullableSummary(t *testing.T) {
	now := time.Date(2026, 6, 30, 1, 0, 0, 0, time.UTC)
	row := fakeRow{
		values: []any{
			int64(7),
			"den-services",
			"doc",
			"Doc",
			DocTypeNote,
			VisibilityNormal,
			[]byte(`["smoke-3834"]`),
			nil,
			now,
		},
	}

	summary, err := scanDocumentSummary(row)
	if err != nil {
		t.Fatalf("scanDocumentSummary() error = %v", err)
	}
	if summary.Summary != "" {
		t.Fatalf("Summary = %q, want empty string", summary.Summary)
	}
	if len(summary.Tags) != 1 || summary.Tags[0] != "smoke-3834" {
		t.Fatalf("Tags = %#v", summary.Tags)
	}
}

type fakeRow struct {
	values []any
}

func (r fakeRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("dest len = %d, values len = %d", len(dest), len(r.values))
	}
	for i := range dest {
		if err := assignFakeScanValue(dest[i], r.values[i]); err != nil {
			return fmt.Errorf("assigning column %d: %w", i, err)
		}
	}
	return nil
}

func assignFakeScanValue(dest any, value any) error {
	switch target := dest.(type) {
	case *int64:
		typed, ok := value.(int64)
		if !ok {
			return fmt.Errorf("want int64, got %T", value)
		}
		*target = typed
	case **int64:
		if value == nil {
			*target = nil
			return nil
		}
		typed, ok := value.(int64)
		if !ok {
			return fmt.Errorf("want int64 pointer value, got %T", value)
		}
		*target = &typed
	case *string:
		typed, ok := value.(string)
		if !ok {
			return fmt.Errorf("want string, got %T", value)
		}
		*target = typed
	case *sql.NullString:
		if value == nil {
			*target = sql.NullString{}
			return nil
		}
		typed, ok := value.(string)
		if !ok {
			return fmt.Errorf("want nullable string, got %T", value)
		}
		*target = sql.NullString{String: typed, Valid: true}
	case *[]byte:
		if value == nil {
			*target = nil
			return nil
		}
		typed, ok := value.([]byte)
		if !ok {
			return fmt.Errorf("want bytes, got %T", value)
		}
		*target = typed
	case **time.Time:
		if value == nil {
			*target = nil
			return nil
		}
		typed, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("want time pointer value, got %T", value)
		}
		*target = &typed
	case *time.Time:
		typed, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("want time, got %T", value)
		}
		*target = typed
	default:
		return fmt.Errorf("unsupported destination %T", dest)
	}
	return nil
}
