package migration

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed postgres/*/*.sql
var defaultMigrations embed.FS

var migrationFilePattern = regexp.MustCompile(`^([0-9]+)_[a-z0-9_]+\.sql$`) //nolint:gochecknoglobals

type Migration struct {
	Schema  string
	Version int
	Name    string
	Path    string
	SQL     string
}

func DefaultFS() fs.FS {
	return defaultMigrations
}

func Discover(fsys fs.FS) ([]Migration, error) {
	var migrations []Migration
	err := fs.WalkDir(fsys, "postgres", func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if path.Ext(filePath) != ".sql" {
			return nil
		}
		migration, err := parseMigration(fsys, filePath)
		if err != nil {
			return err
		}
		migrations = append(migrations, migration)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discovering migrations: %w", err)
	}
	if err := validateMigrations(migrations); err != nil {
		return nil, err
	}
	sort.Slice(migrations, func(left int, right int) bool {
		if migrations[left].Schema == migrations[right].Schema {
			return migrations[left].Version < migrations[right].Version
		}
		return migrations[left].Schema < migrations[right].Schema
	})
	return migrations, nil
}

func parseMigration(fsys fs.FS, filePath string) (Migration, error) {
	parts := strings.Split(filePath, "/")
	if len(parts) != 3 {
		return Migration{}, fmt.Errorf("%w: %s", ErrInvalidMigrationPath, filePath)
	}
	schema := parts[1]
	fileName := parts[2]
	matches := migrationFilePattern.FindStringSubmatch(fileName)
	if matches == nil {
		return Migration{}, fmt.Errorf("%w: %s", ErrInvalidMigrationName, filePath)
	}
	version, err := strconv.Atoi(matches[1])
	if err != nil {
		return Migration{}, fmt.Errorf("%w: %s: %w", ErrInvalidMigrationName, filePath, err)
	}
	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return Migration{}, fmt.Errorf("reading migration %s: %w", filePath, err)
	}
	name := strings.TrimSuffix(fileName, ".sql")
	return Migration{
		Schema:  schema,
		Version: version,
		Name:    name,
		Path:    filePath,
		SQL:     string(data),
	}, nil
}

func validateMigrations(migrations []Migration) error {
	seen := make(map[string]string)
	for _, migration := range migrations {
		key := fmt.Sprintf("%s/%d", migration.Schema, migration.Version)
		if existing, ok := seen[key]; ok {
			return fmt.Errorf("%w: %s and %s", ErrDuplicateMigrationVersion, existing, migration.Path)
		}
		seen[key] = migration.Path
	}
	return nil
}

func groupBySchema(migrations []Migration) map[string][]Migration {
	grouped := make(map[string][]Migration)
	for _, migration := range migrations {
		grouped[migration.Schema] = append(grouped[migration.Schema], migration)
	}
	return grouped
}

var (
	ErrInvalidMigrationPath      = errors.New("invalid migration path")      //nolint:gochecknoglobals
	ErrInvalidMigrationName      = errors.New("invalid migration name")      //nolint:gochecknoglobals
	ErrDuplicateMigrationVersion = errors.New("duplicate migration version") //nolint:gochecknoglobals
)
