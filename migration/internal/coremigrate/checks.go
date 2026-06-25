package coremigrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func checkJSONColumns(ctx context.Context, db *sql.DB, table string, pairs []columnPair) ([]ColumnCheck, error) {
	var checks []ColumnCheck
	for _, pair := range pairs {
		if pair.Target.DataType != "jsonb" && pair.Target.DataType != "json" {
			continue
		}
		check, err := checkColumnParse(ctx, db, table, pair, func(value any) error {
			_, err := normalizeJSON(value)
			return err
		})
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func checkTimestampColumns(ctx context.Context, db *sql.DB, table string, pairs []columnPair) ([]ColumnCheck, error) {
	var checks []ColumnCheck
	for _, pair := range pairs {
		if pair.Target.DataType != "timestamp with time zone" {
			continue
		}
		check, err := checkColumnParse(ctx, db, table, pair, func(value any) error {
			_, err := normalizeTime(value)
			return err
		})
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func checkColumnParse(ctx context.Context, db *sql.DB, table string, pair columnPair, parse func(any) error) (ColumnCheck, error) {
	query := "select " + quoteSQLiteIdent(pair.Source.Name) + " from " + quoteSQLiteIdent(table) + " where " + quoteSQLiteIdent(pair.Source.Name) + " is not null"
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return ColumnCheck{}, fmt.Errorf("checking %s.%s parseability: %w", table, pair.Source.Name, err)
	}
	defer rows.Close()

	check := ColumnCheck{Column: pair.Source.Name}
	for rows.Next() {
		var value any
		if err := rows.Scan(&value); err != nil {
			return ColumnCheck{}, fmt.Errorf("scanning %s.%s parse check: %w", table, pair.Source.Name, err)
		}
		check.Checked++
		if err := parse(value); err != nil {
			check.Invalid++
			if check.Sample == "" {
				check.Sample = nullableString(value).String
			}
		}
	}
	if err := rows.Err(); err != nil {
		return ColumnCheck{}, fmt.Errorf("reading %s.%s parse checks: %w", table, pair.Source.Name, err)
	}
	return check, nil
}

func checkFKAnomalies(ctx context.Context, db *sql.DB, sourceTables map[string]sourceTable, keys []foreignKey) ([]FKAnomaly, error) {
	var anomalies []FKAnomaly
	for _, key := range keys {
		if !sourceHasFKColumns(sourceTables, key) {
			continue
		}
		count, err := countFKAnomaly(ctx, db, key)
		if err != nil {
			return nil, err
		}
		anomalies = append(anomalies, FKAnomaly{
			Constraint:    key.Constraint,
			ChildTable:    key.ChildTable,
			ChildColumns:  key.ChildColumns,
			ParentTable:   key.ParentTable,
			ParentColumns: key.ParentColumns,
			Count:         count,
		})
	}
	return anomalies, nil
}

func sourceHasFKColumns(sourceTables map[string]sourceTable, key foreignKey) bool {
	child, ok := sourceTables[key.ChildTable]
	if !ok {
		return false
	}
	parent, ok := sourceTables[key.ParentTable]
	if !ok {
		return false
	}
	for _, column := range key.ChildColumns {
		if _, ok := child.Columns[column]; !ok {
			return false
		}
	}
	for _, column := range key.ParentColumns {
		if _, ok := parent.Columns[column]; !ok {
			return false
		}
	}
	return len(key.ChildColumns) == len(key.ParentColumns)
}

func countFKAnomaly(ctx context.Context, db *sql.DB, key foreignKey) (int64, error) {
	joinParts := make([]string, len(key.ChildColumns))
	notNullParts := make([]string, len(key.ChildColumns))
	parentNullParts := make([]string, len(key.ParentColumns))
	for index := range key.ChildColumns {
		childColumn := quoteSQLiteIdent(key.ChildColumns[index])
		parentColumn := quoteSQLiteIdent(key.ParentColumns[index])
		joinParts[index] = "c." + childColumn + " = p." + parentColumn
		notNullParts[index] = "c." + childColumn + " is not null"
		parentNullParts[index] = "p." + parentColumn + " is null"
	}
	query := fmt.Sprintf(
		"select count(*) from %s c left join %s p on %s where %s and %s",
		quoteSQLiteIdent(key.ChildTable),
		quoteSQLiteIdent(key.ParentTable),
		strings.Join(joinParts, " and "),
		strings.Join(notNullParts, " and "),
		strings.Join(parentNullParts, " and "),
	)
	var count int64
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("checking fk %s anomalies: %w", key.Constraint, err)
	}
	return count, nil
}
