package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging projects store: %w", err)
	}
	return nil
}

func (s *Store) CreateScope(ctx context.Context, scope *Scope) (*Scope, error) {
	created, err := scanScope(s.pool.QueryRow(ctx, createScopeSQL,
		scope.ID(),
		scope.Name(),
		scope.Kind(),
		scope.Visibility(),
		emptyToNil(scope.Owner()),
		emptyToNil(scope.RootPath()),
		emptyToNil(scope.Description()),
		jsonOrNil(scope.SettingsJSON()),
		scope.CreatedAt(),
		scope.UpdatedAt(),
	))
	if isUniqueViolation(err) {
		return nil, conflict(fmt.Errorf("%w: %s", ErrDuplicateScope, scope.ID()), "scope_already_exists")
	}
	if err != nil {
		return nil, fmt.Errorf("creating scope %s: %w", scope.ID(), err)
	}
	return created, nil
}

func (s *Store) GetScope(ctx context.Context, id string) (*Scope, error) {
	scope, err := scanScope(s.pool.QueryRow(ctx, getScopeSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting scope %s: %w", id, err)
	}
	return scope, nil
}

func (s *Store) ListScopes(ctx context.Context, query ListScopesQuery) ([]*Scope, error) {
	rows, err := s.pool.Query(ctx, listScopesSQL, emptyToNil(query.Kind), query.IncludeHidden, query.IncludeArchived)
	if err != nil {
		return nil, fmt.Errorf("listing scopes: %w", err)
	}
	defer rows.Close()
	return scanScopes(rows)
}

func (s *Store) UpdateScope(ctx context.Context, id string, patch ScopePatch, updatedAt time.Time) (*Scope, error) {
	scope, err := scanScope(s.pool.QueryRow(ctx, updateScopeSQL,
		id,
		patch.Name,
		patch.RootPath,
		patch.Description,
		patch.Owner,
		patch.HasSettings,
		jsonOrNil(patch.SettingsJSON),
		updatedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("updating scope %s: %w", id, err)
	}
	return scope, nil
}

func (s *Store) UpdateVisibility(ctx context.Context, id string, visibility string, updatedAt time.Time) (*Scope, error) {
	scope, err := scanScope(s.pool.QueryRow(ctx, updateVisibilitySQL, id, visibility, updatedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("updating scope visibility %s: %w", id, err)
	}
	return scope, nil
}

func (s *Store) DeleteScope(ctx context.Context, id string) (*Scope, error) {
	scope, err := scanScope(s.pool.QueryRow(ctx, deleteScopeSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("deleting scope %s: %w", id, err)
	}
	return scope, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanScopes(rows pgx.Rows) ([]*Scope, error) {
	var scopes []*Scope
	for rows.Next() {
		scope, err := scanScope(rows)
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading scopes: %w", err)
	}
	return scopes, nil
}

func scanScope(row rowScanner) (*Scope, error) {
	var id string
	var name string
	var kind string
	var visibility string
	var owner *string
	var rootPath *string
	var description *string
	var settingsJSON []byte
	var createdAt time.Time
	var updatedAt time.Time
	if err := row.Scan(
		&id,
		&name,
		&kind,
		&visibility,
		&owner,
		&rootPath,
		&description,
		&settingsJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	return NewScope(NewScopeParams{
		ID:           id,
		Name:         name,
		Kind:         kind,
		Visibility:   visibility,
		Owner:        nilToString(owner),
		RootPath:     nilToString(rootPath),
		Description:  nilToString(description),
		SettingsJSON: settingsJSON,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	})
}

func emptyToNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nilToString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func jsonOrNil(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return json.RawMessage(value)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

const scopeColumns = `
id, name, kind, visibility, owner, root_path, description, settings_json,
created_at, updated_at`

const createScopeSQL = `
insert into den_projects.projects (
	id, name, kind, visibility, owner, root_path, description, settings_json, created_at, updated_at
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
returning ` + scopeColumns

const getScopeSQL = `
select ` + scopeColumns + `
from den_projects.projects
where id = $1`

const listScopesSQL = `
select ` + scopeColumns + `
from den_projects.projects
where ($1::text is null or kind = $1)
  and ($2::boolean or visibility <> 'hidden')
  and ($3::boolean or visibility <> 'archived')
order by id`

const updateScopeSQL = `
update den_projects.projects
set name = coalesce($2, name),
    root_path = case when $3::text is null then root_path when $3::text = '' then null else $3::text end,
    description = case when $4::text is null then description when $4::text = '' then null else $4::text end,
    owner = case when $5::text is null then owner when $5::text = '' then null else $5::text end,
    settings_json = case when $6::boolean then $7::jsonb else settings_json end,
    updated_at = $8
where id = $1
returning ` + scopeColumns

const updateVisibilitySQL = `
update den_projects.projects
set visibility = $2,
    updated_at = $3
where id = $1
returning ` + scopeColumns

const deleteScopeSQL = `
delete from den_projects.projects
where id = $1
returning ` + scopeColumns
