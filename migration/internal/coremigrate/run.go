package coremigrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"den-services/migration"
	"den-services/shared/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "modernc.org/sqlite"
)

func Run(ctx context.Context, options Options) (*Report, error) {
	if strings.TrimSpace(options.SourceSQLitePath) == "" {
		return nil, errors.New("source sqlite path is required")
	}
	if strings.TrimSpace(options.DatabaseURL) == "" {
		return nil, postgres.ErrMissingDatabaseURL
	}
	if err := validateReadableFile(options.SourceSQLitePath); err != nil {
		return nil, err
	}

	sqliteDB, err := openSQLiteReadOnly(options.SourceSQLitePath)
	if err != nil {
		return nil, err
	}
	defer sqliteDB.Close()
	if err := sqliteDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("pinging sqlite source: %w", err)
	}

	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: options.DatabaseURL})
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	report := &Report{
		SourceSQLitePath: options.SourceSQLitePath,
		SearchParityPlaceholders: []string{
			"documents/knowledge FTS parity is placeholder-only until #3324 replaces SQLite FTS5 behavior",
		},
	}
	if options.ApplyMigrations {
		applied, err := applyMigrations(ctx, pool)
		if err != nil {
			return nil, err
		}
		for _, appliedMigration := range applied {
			report.AppliedMigrations = append(report.AppliedMigrations, fmt.Sprintf("%s/%03d", appliedMigration.Schema, appliedMigration.Version))
		}
	}

	sourceTables, excluded, err := loadSourceTables(ctx, sqliteDB)
	if err != nil {
		return nil, err
	}
	report.ExcludedSourceTables = excluded
	targetTables, err := loadTargetTables(ctx, pool)
	if err != nil {
		return nil, err
	}
	if len(targetTables) == 0 {
		return nil, errors.New("den_core schema has no target tables; run migrations first or pass --apply-migrations")
	}
	foreignKeys, err := loadForeignKeys(ctx, pool)
	if err != nil {
		return nil, err
	}

	report.UnexpectedSourceTables = unexpectedSourceTables(sourceTables, targetTables)
	report.MissingSourceTables = missingSourceTables(sourceTables, targetTables)
	tableOrder := sortTablesForImport(targetTables, foreignKeys)

	tableReports, err := preflightTables(ctx, sqliteDB, sourceTables, targetTables, foreignKeys, tableOrder)
	if err != nil {
		return nil, err
	}
	report.Tables = tableReports
	if preflightHasBlockingMismatch(report.Tables) {
		return report, nil
	}

	importedRows, err := importTables(ctx, sqliteDB, pool, sourceTables, targetTables, tableOrder, options.ResetTarget)
	if err != nil {
		return nil, err
	}
	for index := range report.Tables {
		report.Tables[index].ImportedRows = importedRows[report.Tables[index].Name]
	}

	if err := refreshTargetParity(ctx, sqliteDB, pool, sourceTables, targetTables, report); err != nil {
		return nil, err
	}
	return report, nil
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) ([]migration.AppliedMigration, error) {
	migrations, err := migration.Discover(migration.DefaultFS())
	if err != nil {
		return nil, err
	}
	var denCoreMigrations []migration.Migration
	for _, candidate := range migrations {
		if candidate.Schema == "den_core" {
			denCoreMigrations = append(denCoreMigrations, candidate)
		}
	}
	runner, err := migration.NewRunner(pool, denCoreMigrations)
	if err != nil {
		return nil, err
	}
	applied, err := runner.Up(ctx)
	if err != nil {
		return nil, err
	}
	return applied, nil
}

func preflightTables(
	ctx context.Context,
	sqliteDB *sql.DB,
	sourceTables map[string]sourceTable,
	targetTables map[string]targetTable,
	foreignKeys []foreignKey,
	tableOrder []string,
) ([]TableReport, error) {
	fksByTable := map[string][]foreignKey{}
	for _, key := range foreignKeys {
		fksByTable[key.ChildTable] = append(fksByTable[key.ChildTable], key)
	}

	var reports []TableReport
	for _, tableName := range tableOrder {
		sourceTable, ok := sourceTables[tableName]
		if !ok {
			reports = append(reports, TableReport{Name: tableName, Status: "source_missing"})
			continue
		}
		targetTable := targetTables[tableName]
		pairs, missingRequired := importColumnPairs(sourceTable, targetTable)
		tableReport := TableReport{
			Name:                   tableName,
			MissingRequiredColumns: missingRequired,
			Status:                 "ok",
		}
		tableReport.SourceOnlyColumns, tableReport.TargetOnlyColumns = columnGaps(sourceTable, targetTable)
		count, err := sqliteCount(ctx, sqliteDB, tableName)
		if err != nil {
			return nil, err
		}
		tableReport.SourceCount = count
		idRange, err := sqliteIDRange(ctx, sqliteDB, tableName, sourceTable, targetTable)
		if err != nil {
			return nil, err
		}
		tableReport.SourceIDRange = idRange
		tableReport.JSONChecks, err = checkJSONColumns(ctx, sqliteDB, tableName, pairs)
		if err != nil {
			return nil, err
		}
		tableReport.TimestampChecks, err = checkTimestampColumns(ctx, sqliteDB, tableName, pairs)
		if err != nil {
			return nil, err
		}
		tableReport.FKAnomalies, err = checkFKAnomalies(ctx, sqliteDB, sourceTables, fksByTable[tableName])
		if err != nil {
			return nil, err
		}
		tableReport.SourceChecksum, err = checksumSQLiteTable(ctx, sqliteDB, tableName, pairs)
		if err != nil {
			return nil, err
		}
		tableReport.Status = preflightStatus(tableReport)
		reports = append(reports, tableReport)
	}
	return reports, nil
}

func preflightStatus(report TableReport) string {
	if report.SourceCount > 0 && len(report.MissingRequiredColumns) > 0 {
		return "schema_gap"
	}
	if len(report.SourceOnlyColumns) > 0 {
		return "schema_gap"
	}
	for _, check := range append(report.JSONChecks, report.TimestampChecks...) {
		if check.Invalid > 0 {
			return "parse_fail"
		}
	}
	for _, anomaly := range report.FKAnomalies {
		if anomaly.Count > 0 {
			return "fk_anomaly"
		}
	}
	return "ok"
}

func preflightHasBlockingMismatch(reports []TableReport) bool {
	for _, report := range reports {
		if report.Status != "ok" {
			return true
		}
	}
	return false
}

func importTables(
	ctx context.Context,
	sqliteDB *sql.DB,
	pool *pgxpool.Pool,
	sourceTables map[string]sourceTable,
	targetTables map[string]targetTable,
	tableOrder []string,
	resetTarget bool,
) (map[string]int64, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning den_core import: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if resetTarget {
		if _, err := tx.Exec(ctx, truncateSQL(targetTables)); err != nil {
			return nil, fmt.Errorf("resetting den_core target tables: %w", err)
		}
	}

	importedRows := map[string]int64{}
	for _, tableName := range tableOrder {
		sourceTable, ok := sourceTables[tableName]
		if !ok {
			continue
		}
		targetTable := targetTables[tableName]
		pairs, _ := importColumnPairs(sourceTable, targetTable)
		count, err := copyTable(ctx, sqliteDB, tx, tableName, pairs)
		if err != nil {
			return nil, err
		}
		importedRows[tableName] = count
	}

	if _, err := tx.Exec(ctx, "select den_core.reset_identity_sequences_after_import()"); err != nil {
		return nil, fmt.Errorf("resetting den_core identity sequences after import: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing den_core import: %w", err)
	}
	return importedRows, nil
}

func copyTable(ctx context.Context, sqliteDB *sql.DB, tx pgx.Tx, tableName string, pairs []columnPair) (int64, error) {
	if len(pairs) == 0 {
		return 0, nil
	}
	columnNames := make([]string, len(pairs))
	for index, pair := range pairs {
		columnNames[index] = pair.Target.Name
	}
	source, err := newSQLiteCopySource(ctx, sqliteDB, tableName, pairs)
	if err != nil {
		return 0, err
	}
	defer source.Close()
	count, err := tx.CopyFrom(ctx, pgx.Identifier{"den_core", tableName}, columnNames, source)
	if err != nil {
		return 0, fmt.Errorf("copying %s into den_core: %w", tableName, err)
	}
	return count, nil
}

func refreshTargetParity(
	ctx context.Context,
	sqliteDB *sql.DB,
	pool *pgxpool.Pool,
	sourceTables map[string]sourceTable,
	targetTables map[string]targetTable,
	report *Report,
) error {
	for index := range report.Tables {
		table := &report.Tables[index]
		if table.Status == "source_missing" {
			continue
		}
		targetTable := targetTables[table.Name]
		sourceTable := sourceTables[table.Name]
		pairs, _ := importColumnPairs(sourceTable, targetTable)
		targetCount, err := postgresCount(ctx, pool, table.Name)
		if err != nil {
			return err
		}
		table.TargetCount = targetCount
		targetIDRange, err := postgresIDRange(ctx, pool, table.Name, targetTable)
		if err != nil {
			return err
		}
		table.TargetIDRange = targetIDRange
		targetChecksum, err := checksumPostgresTable(ctx, pool, table.Name, pairs)
		if err != nil {
			return err
		}
		table.TargetChecksum = targetChecksum
		if table.Status == "ok" && table.SourceCount != table.TargetCount {
			table.Status = "count_mismatch"
		}
		if table.Status == "ok" && table.SourceChecksum != table.TargetChecksum {
			table.Status = "checksum_mismatch"
		}
	}
	return nil
}

type sqliteCopySource struct {
	rows   *sql.Rows
	pairs  []columnPair
	values []any
	err    error
}

func newSQLiteCopySource(ctx context.Context, db *sql.DB, table string, pairs []columnPair) (*sqliteCopySource, error) {
	columnNames := make([]string, len(pairs))
	for index, pair := range pairs {
		columnNames[index] = quoteSQLiteIdent(pair.Source.Name)
	}
	query := "select " + strings.Join(columnNames, ", ") + " from " + quoteSQLiteIdent(table)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying sqlite table %s for import: %w", table, err)
	}
	return &sqliteCopySource{
		rows:   rows,
		pairs:  pairs,
		values: make([]any, len(pairs)),
	}, nil
}

func (s *sqliteCopySource) Next() bool {
	if !s.rows.Next() {
		return false
	}
	rawValues := make([]any, len(s.pairs))
	if err := s.rows.Scan(scanValuePointers(rawValues)...); err != nil {
		s.err = err
		return false
	}
	for index, value := range rawValues {
		normalized, err := normalizeForPostgres(value, s.pairs[index].Target)
		if err != nil {
			s.err = fmt.Errorf("normalizing %s: %w", s.pairs[index].Target.Name, err)
			return false
		}
		s.values[index] = normalized
	}
	return true
}

func (s *sqliteCopySource) Values() ([]any, error) {
	return s.values, nil
}

func (s *sqliteCopySource) Err() error {
	if s.err != nil {
		return s.err
	}
	if err := s.rows.Err(); err != nil {
		return err
	}
	return nil
}

func (s *sqliteCopySource) Close() {
	_ = s.rows.Close()
}

func validateReadableFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("reading source sqlite path: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("source sqlite path %s is a directory", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening source sqlite path read-only: %w", err)
	}
	return file.Close()
}

func openSQLiteReadOnly(path string) (*sql.DB, error) {
	uri := url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Set("mode", "ro")
	query.Set("cache", "private")
	query.Set("_pragma", "foreign_keys(1)")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, fmt.Errorf("opening sqlite source read-only: %w", err)
	}
	if _, err := db.Exec("pragma query_only = on"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enabling sqlite query_only: %w", err)
	}
	return db, nil
}

func unexpectedSourceTables(source map[string]sourceTable, target map[string]targetTable) []string {
	var unexpected []string
	for name := range source {
		if _, ok := target[name]; !ok {
			unexpected = append(unexpected, name)
		}
	}
	sort.Strings(unexpected)
	return unexpected
}

func missingSourceTables(source map[string]sourceTable, target map[string]targetTable) []string {
	var missing []string
	for name := range target {
		if _, ok := source[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func sqliteCount(ctx context.Context, db *sql.DB, table string) (int64, error) {
	var count int64
	query := "select count(*) from " + quoteSQLiteIdent(table)
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting sqlite table %s: %w", table, err)
	}
	return count, nil
}

func postgresCount(ctx context.Context, pool *pgxpool.Pool, table string) (int64, error) {
	var count int64
	query := "select count(*) from " + pgx.Identifier{"den_core", table}.Sanitize()
	if err := pool.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting den_core.%s: %w", table, err)
	}
	return count, nil
}

func sqliteIDRange(ctx context.Context, db *sql.DB, table string, source sourceTable, target targetTable) (IDRange, error) {
	targetID, ok := target.ColumnByName["id"]
	if !ok || !isIntegerTargetColumn(targetID) {
		return IDRange{}, nil
	}
	sourceID, ok := source.Columns["id"]
	if !ok || !isIntegerSourceColumn(sourceID) {
		return IDRange{}, nil
	}
	var count int64
	var minValue sql.NullInt64
	var maxValue sql.NullInt64
	query := "select count(id), min(id), max(id) from " + quoteSQLiteIdent(table)
	if err := db.QueryRowContext(ctx, query).Scan(&count, &minValue, &maxValue); err != nil {
		return IDRange{}, fmt.Errorf("reading sqlite id range for %s: %w", table, err)
	}
	return IDRange{HasID: true, Count: count, Min: nullableInt64Ptr(minValue), Max: nullableInt64Ptr(maxValue)}, nil
}

func postgresIDRange(ctx context.Context, pool *pgxpool.Pool, table string, target targetTable) (IDRange, error) {
	targetID, ok := target.ColumnByName["id"]
	if !ok || !isIntegerTargetColumn(targetID) {
		return IDRange{}, nil
	}
	var count int64
	var minValue *int64
	var maxValue *int64
	query := "select count(id), min(id), max(id) from " + pgx.Identifier{"den_core", table}.Sanitize()
	if err := pool.QueryRow(ctx, query).Scan(&count, &minValue, &maxValue); err != nil {
		return IDRange{}, fmt.Errorf("reading den_core id range for %s: %w", table, err)
	}
	return IDRange{HasID: true, Count: count, Min: minValue, Max: maxValue}, nil
}

func isIntegerTargetColumn(column targetColumn) bool {
	return column.DataType == "bigint" || column.DataType == "integer"
}

func isIntegerSourceColumn(column sourceColumn) bool {
	upperType := strings.ToUpper(column.Type)
	return strings.Contains(upperType, "INT")
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func checksumSQLiteTable(ctx context.Context, db *sql.DB, table string, pairs []columnPair) (string, error) {
	if len(pairs) == 0 {
		return "", nil
	}
	columnNames := make([]string, len(pairs))
	for index, pair := range pairs {
		columnNames[index] = quoteSQLiteIdent(pair.Source.Name)
	}
	query := "select " + strings.Join(columnNames, ", ") + " from " + quoteSQLiteIdent(table)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("querying sqlite table %s for checksum: %w", table, err)
	}
	defer rows.Close()

	var rowHashes []string
	for rows.Next() {
		rawValues := make([]any, len(pairs))
		if err := rows.Scan(scanValuePointers(rawValues)...); err != nil {
			return "", fmt.Errorf("scanning sqlite checksum row for %s: %w", table, err)
		}
		hash, err := hashCanonicalRow(rawValues, pairs)
		if err != nil {
			return "", fmt.Errorf("hashing sqlite row for %s: %w", table, err)
		}
		rowHashes = append(rowHashes, hash)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("reading sqlite checksum rows for %s: %w", table, err)
	}
	return aggregateRowHashes(rowHashes), nil
}

func checksumPostgresTable(ctx context.Context, pool *pgxpool.Pool, table string, pairs []columnPair) (string, error) {
	if len(pairs) == 0 {
		return "", nil
	}
	expressions := make([]string, len(pairs))
	for index, pair := range pairs {
		identifier := pgx.Identifier{pair.Target.Name}.Sanitize()
		expressions[index] = identifier + "::text"
	}
	query := "select " + strings.Join(expressions, ", ") + " from " + pgx.Identifier{"den_core", table}.Sanitize()
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return "", fmt.Errorf("querying den_core.%s for checksum: %w", table, err)
	}
	defer rows.Close()

	var rowHashes []string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return "", fmt.Errorf("reading den_core checksum row for %s: %w", table, err)
		}
		hash, err := hashCanonicalRow(values, pairs)
		if err != nil {
			return "", fmt.Errorf("hashing den_core row for %s: %w", table, err)
		}
		rowHashes = append(rowHashes, hash)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("reading den_core checksum rows for %s: %w", table, err)
	}
	return aggregateRowHashes(rowHashes), nil
}

func hashCanonicalRow(values []any, pairs []columnPair) (string, error) {
	parts := make([]string, len(values))
	for index, value := range values {
		canonical, err := canonicalValue(value, pairs[index].Target)
		if err != nil {
			return "", err
		}
		parts[index] = canonical
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:]), nil
}

func aggregateRowHashes(rowHashes []string) string {
	sort.Strings(rowHashes)
	sum := sha256.Sum256([]byte(strings.Join(rowHashes, "\n")))
	return hex.EncodeToString(sum[:])
}
