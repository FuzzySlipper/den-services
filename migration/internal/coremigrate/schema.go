package coremigrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sourceTable struct {
	Name    string
	SQL     string
	Columns map[string]sourceColumn
}

type sourceColumn struct {
	Name    string
	Type    string
	NotNull bool
}

type targetTable struct {
	Name         string
	Columns      []targetColumn
	ColumnByName map[string]targetColumn
}

type targetColumn struct {
	Name       string
	DataType   string
	UDTName    string
	Nullable   bool
	HasDefault bool
	IsIdentity bool
}

type columnPair struct {
	Source sourceColumn
	Target targetColumn
}

type foreignKey struct {
	Constraint    string
	ChildTable    string
	ChildColumns  []string
	ParentTable   string
	ParentColumns []string
}

func loadSourceTables(ctx context.Context, db *sql.DB) (map[string]sourceTable, []string, error) {
	rows, err := db.QueryContext(ctx, `
select name, sql
from sqlite_master
where type = 'table'
  and name not like 'sqlite_%'
order by name`)
	if err != nil {
		return nil, nil, fmt.Errorf("listing sqlite tables: %w", err)
	}
	defer rows.Close()

	tables := map[string]sourceTable{}
	var excluded []string
	for rows.Next() {
		var name string
		var createSQL sql.NullString
		if err := rows.Scan(&name, &createSQL); err != nil {
			return nil, nil, fmt.Errorf("scanning sqlite table: %w", err)
		}
		if isKnownSQLiteExclusion(name, createSQL.String) {
			excluded = append(excluded, name)
			continue
		}
		columns, err := loadSourceColumns(ctx, db, name)
		if err != nil {
			return nil, nil, err
		}
		tables[name] = sourceTable{Name: name, SQL: createSQL.String, Columns: columns}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("reading sqlite tables: %w", err)
	}
	sort.Strings(excluded)
	return tables, excluded, nil
}

func loadSourceColumns(ctx context.Context, db *sql.DB, table string) (map[string]sourceColumn, error) {
	rows, err := db.QueryContext(ctx, "pragma table_info("+quoteSQLiteIdent(table)+")")
	if err != nil {
		return nil, fmt.Errorf("listing sqlite columns for %s: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]sourceColumn{}
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("scanning sqlite columns for %s: %w", table, err)
		}
		columns[name] = sourceColumn{Name: name, Type: dataType, NotNull: notNull != 0}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading sqlite columns for %s: %w", table, err)
	}
	return columns, nil
}

func loadTargetTables(ctx context.Context, pool *pgxpool.Pool) (map[string]targetTable, error) {
	rows, err := pool.Query(ctx, `
select table_name, column_name, data_type, udt_name, is_nullable, column_default, is_identity, is_generated
from information_schema.columns
where table_schema = 'den_core'
order by table_name, ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("listing den_core columns: %w", err)
	}
	defer rows.Close()

	tables := map[string]targetTable{}
	for rows.Next() {
		var tableName string
		var column targetColumn
		var nullable string
		var defaultValue *string
		var isIdentity string
		var isGenerated string
		if err := rows.Scan(&tableName, &column.Name, &column.DataType, &column.UDTName, &nullable, &defaultValue, &isIdentity, &isGenerated); err != nil {
			return nil, fmt.Errorf("scanning den_core column: %w", err)
		}
		if tableName == "schema_migrations" || isGenerated != "NEVER" {
			continue
		}
		column.Nullable = nullable == "YES"
		column.HasDefault = defaultValue != nil
		column.IsIdentity = isIdentity == "YES"
		table := tables[tableName]
		if table.Name == "" {
			table.Name = tableName
			table.ColumnByName = map[string]targetColumn{}
		}
		table.Columns = append(table.Columns, column)
		table.ColumnByName[column.Name] = column
		tables[tableName] = table
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading den_core columns: %w", err)
	}
	return tables, nil
}

func loadForeignKeys(ctx context.Context, pool *pgxpool.Pool) ([]foreignKey, error) {
	rows, err := pool.Query(ctx, `
select
    c.conname,
    child.relname as child_table,
    array_agg(child_att.attname order by keys.ord) as child_columns,
    parent.relname as parent_table,
    array_agg(parent_att.attname order by keys.ord) as parent_columns
from pg_constraint c
join pg_namespace n on n.oid = c.connamespace
join pg_class child on child.oid = c.conrelid
join pg_class parent on parent.oid = c.confrelid
join unnest(c.conkey, c.confkey) with ordinality as keys(child_attnum, parent_attnum, ord) on true
join pg_attribute child_att on child_att.attrelid = c.conrelid and child_att.attnum = keys.child_attnum
join pg_attribute parent_att on parent_att.attrelid = c.confrelid and parent_att.attnum = keys.parent_attnum
where n.nspname = 'den_core'
  and c.contype = 'f'
group by c.conname, child.relname, parent.relname
order by child.relname, c.conname`)
	if err != nil {
		return nil, fmt.Errorf("listing den_core foreign keys: %w", err)
	}
	defer rows.Close()

	var keys []foreignKey
	for rows.Next() {
		var key foreignKey
		if err := rows.Scan(&key.Constraint, &key.ChildTable, &key.ChildColumns, &key.ParentTable, &key.ParentColumns); err != nil {
			return nil, fmt.Errorf("scanning den_core foreign key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading den_core foreign keys: %w", err)
	}
	return keys, nil
}

func tableNames[T sourceTable | targetTable](tables map[string]T) []string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func importColumnPairs(source sourceTable, target targetTable) ([]columnPair, []string) {
	var pairs []columnPair
	var missingRequired []string
	for _, targetColumn := range target.Columns {
		sourceColumn, ok := source.Columns[targetColumn.Name]
		if ok {
			pairs = append(pairs, columnPair{Source: sourceColumn, Target: targetColumn})
			continue
		}
		if !targetColumn.Nullable && !targetColumn.HasDefault && !targetColumn.IsIdentity {
			missingRequired = append(missingRequired, targetColumn.Name)
		}
	}
	return pairs, missingRequired
}

func columnGaps(source sourceTable, target targetTable) ([]string, []string) {
	var sourceOnly []string
	var targetOnly []string
	for name := range source.Columns {
		if _, ok := target.ColumnByName[name]; !ok {
			sourceOnly = append(sourceOnly, name)
		}
	}
	for _, column := range target.Columns {
		if _, ok := source.Columns[column.Name]; !ok {
			targetOnly = append(targetOnly, column.Name)
		}
	}
	sort.Strings(sourceOnly)
	sort.Strings(targetOnly)
	return sourceOnly, targetOnly
}

func sortTablesForImport(tables map[string]targetTable, keys []foreignKey) []string {
	dependencies := map[string]map[string]bool{}
	for table := range tables {
		dependencies[table] = map[string]bool{}
	}
	for _, key := range keys {
		if key.ChildTable == key.ParentTable {
			continue
		}
		if _, ok := tables[key.ChildTable]; !ok {
			continue
		}
		if _, ok := tables[key.ParentTable]; !ok {
			continue
		}
		dependencies[key.ChildTable][key.ParentTable] = true
	}

	var sorted []string
	remaining := map[string]bool{}
	for table := range tables {
		remaining[table] = true
	}
	for len(remaining) > 0 {
		var ready []string
		for table := range remaining {
			hasRemainingDependency := false
			for dependency := range dependencies[table] {
				if remaining[dependency] {
					hasRemainingDependency = true
					break
				}
			}
			if !hasRemainingDependency {
				ready = append(ready, table)
			}
		}
		if len(ready) == 0 {
			for table := range remaining {
				ready = append(ready, table)
			}
		}
		sort.Strings(ready)
		for _, table := range ready {
			sorted = append(sorted, table)
			delete(remaining, table)
		}
	}
	return sorted
}

func truncateSQL(tables map[string]targetTable) string {
	names := tableNames(tables)
	qualified := make([]string, 0, len(names))
	for _, name := range names {
		qualified = append(qualified, pgx.Identifier{"den_core", name}.Sanitize())
	}
	return "truncate table " + strings.Join(qualified, ", ") + " restart identity cascade"
}

func isKnownSQLiteExclusion(name string, createSQL string) bool {
	lowerName := strings.ToLower(name)
	lowerSQL := strings.ToLower(createSQL)
	if strings.Contains(lowerSQL, "create virtual table") && strings.Contains(lowerSQL, "using fts5") {
		return true
	}
	return strings.Contains(lowerName, "_fts_") || strings.HasSuffix(lowerName, "_fts")
}

func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
