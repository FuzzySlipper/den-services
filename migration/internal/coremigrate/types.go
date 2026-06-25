package coremigrate

import (
	"fmt"
	"strings"
)

type Options struct {
	SourceSQLitePath string
	DatabaseURL      string
	ApplyMigrations  bool
	ResetTarget      bool
}

type Report struct {
	SourceSQLitePath         string
	AppliedMigrations        []string
	ExcludedSourceTables     []string
	UnexpectedSourceTables   []string
	MissingSourceTables      []string
	Tables                   []TableReport
	SearchParityPlaceholders []string
}

type TableReport struct {
	Name                   string
	ImportedRows           int64
	SourceCount            int64
	TargetCount            int64
	SourceIDRange          IDRange
	TargetIDRange          IDRange
	SourceChecksum         string
	TargetChecksum         string
	JSONChecks             []ColumnCheck
	TimestampChecks        []ColumnCheck
	FKAnomalies            []FKAnomaly
	MissingRequiredColumns []string
	SourceOnlyColumns      []string
	TargetOnlyColumns      []string
	Status                 string
}

type IDRange struct {
	HasID bool
	Count int64
	Min   *int64
	Max   *int64
}

type ColumnCheck struct {
	Column  string
	Checked int64
	Invalid int64
	Sample  string
}

type FKAnomaly struct {
	Constraint    string
	ChildTable    string
	ChildColumns  []string
	ParentTable   string
	ParentColumns []string
	Count         int64
}

func (r *Report) HasMismatch() bool {
	if len(r.UnexpectedSourceTables) > 0 || len(r.MissingSourceTables) > 0 {
		return true
	}
	for _, table := range r.Tables {
		if table.Status != "ok" {
			return true
		}
	}
	return false
}

func (r *Report) FormatText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "den_core import parity report\n")
	fmt.Fprintf(&b, "source_sqlite=%s\n", r.SourceSQLitePath)
	if len(r.AppliedMigrations) > 0 {
		fmt.Fprintf(&b, "applied_migrations=%s\n", strings.Join(r.AppliedMigrations, ", "))
	}
	if len(r.ExcludedSourceTables) > 0 {
		fmt.Fprintf(&b, "known_source_exclusions=%s\n", strings.Join(r.ExcludedSourceTables, ", "))
	}
	if len(r.UnexpectedSourceTables) > 0 {
		fmt.Fprintf(&b, "unexpected_source_tables=%s\n", strings.Join(r.UnexpectedSourceTables, ", "))
	}
	if len(r.MissingSourceTables) > 0 {
		fmt.Fprintf(&b, "missing_source_tables=%s\n", strings.Join(r.MissingSourceTables, ", "))
	}
	if len(r.SearchParityPlaceholders) > 0 {
		fmt.Fprintf(&b, "search_parity_placeholders=%s\n", strings.Join(r.SearchParityPlaceholders, "; "))
	}
	fmt.Fprintf(&b, "\n%-34s %10s %10s %10s %-8s %s\n", "table", "source", "imported", "target", "status", "notes")
	for _, table := range r.Tables {
		notes := tableNotes(table)
		fmt.Fprintf(&b, "%-34s %10d %10d %10d %-8s %s\n", table.Name, table.SourceCount, table.ImportedRows, table.TargetCount, table.Status, notes)
	}
	return strings.TrimRight(b.String(), "\n")
}

func tableNotes(table TableReport) string {
	var notes []string
	if table.SourceIDRange.HasID {
		notes = append(notes, fmt.Sprintf("id=%s", formatRange(table.SourceIDRange)))
	}
	if table.SourceChecksum != "" && table.TargetChecksum != "" {
		if table.SourceChecksum == table.TargetChecksum {
			notes = append(notes, "checksum=ok")
		} else {
			notes = append(notes, "checksum=mismatch")
		}
	}
	for _, check := range append(table.JSONChecks, table.TimestampChecks...) {
		if check.Invalid > 0 {
			notes = append(notes, fmt.Sprintf("%s_invalid=%d", check.Column, check.Invalid))
		}
	}
	for _, anomaly := range table.FKAnomalies {
		if anomaly.Count > 0 {
			notes = append(notes, fmt.Sprintf("%s_fk_anomalies=%d", anomaly.Constraint, anomaly.Count))
		}
	}
	if len(table.MissingRequiredColumns) > 0 {
		notes = append(notes, "missing_required="+strings.Join(table.MissingRequiredColumns, ","))
	}
	if len(table.SourceOnlyColumns) > 0 {
		notes = append(notes, "source_only_columns="+strings.Join(table.SourceOnlyColumns, ","))
	}
	if len(table.TargetOnlyColumns) > 0 {
		notes = append(notes, "target_only_columns="+strings.Join(table.TargetOnlyColumns, ","))
	}
	return strings.Join(notes, "; ")
}

func formatRange(r IDRange) string {
	if r.Count == 0 || r.Min == nil || r.Max == nil {
		return "empty"
	}
	return fmt.Sprintf("%d..%d", *r.Min, *r.Max)
}
