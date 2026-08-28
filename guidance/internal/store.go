package guidance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
		return fmt.Errorf("pinging guidance store: %w", err)
	}
	return nil
}

func (s *Store) UpsertEntry(ctx context.Context, entry *Entry) (*Entry, error) {
	created, err := scanEntry(s.pool.QueryRow(ctx, upsertEntrySQL,
		entry.ProjectID,
		entry.DocumentProjectID,
		entry.DocumentSlug,
		entry.Importance,
		jsonOrNil(entry.Audience),
		entry.SortOrder,
		emptyToNil(entry.Notes),
		entry.CreatedAt,
		entry.UpdatedAt,
	))
	if err != nil {
		return nil, fmt.Errorf("upserting guidance entry: %w", err)
	}
	return created, nil
}

func (s *Store) ListEntries(ctx context.Context, projectID string, includeGlobal bool) ([]Entry, error) {
	rows, err := s.pool.Query(ctx, listEntriesSQL, projectID, includeGlobal, GlobalProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing guidance entries: %w", err)
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning guidance entries: %w", err)
	}
	return entries, nil
}

func (s *Store) DeleteEntry(ctx context.Context, projectID string, entryID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, deleteEntrySQL, projectID, entryID)
	if err != nil {
		return false, fmt.Errorf("deleting guidance entry: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) DocumentReferences(ctx context.Context, documentProjectID string, documentSlug string) ([]DocumentReference, error) {
	rows, err := s.pool.Query(ctx, documentReferencesSQL, documentProjectID, documentSlug)
	if err != nil {
		return nil, fmt.Errorf("listing guidance document references: %w", err)
	}
	defer rows.Close()
	refs := []DocumentReference{}
	for rows.Next() {
		var ref DocumentReference
		var audience []byte
		if err := rows.Scan(&ref.EntryID, &ref.ScopeProjectID, &ref.Importance, &audience, &ref.SortOrder, &ref.Notes, &ref.CreatedAt, &ref.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning guidance document reference: %w", err)
		}
		ref.RefKind = "agent_guidance"
		ref.Description = "Agent guidance references this document."
		ref.Audience = audienceFromJSON(audience)
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning guidance document references: %w", err)
	}
	return refs, nil
}

func (s *Store) CreateBinding(ctx context.Context, binding *KnowledgeBinding) (*KnowledgeBinding, error) {
	created, err := scanBinding(s.pool.QueryRow(ctx, createBindingSQL,
		binding.TargetKind, binding.TargetRef, binding.ScopeKind, binding.ScopeRef, jsonOrNil(binding.Audience), binding.Priority,
		binding.SortOrder, binding.ReadPolicy, emptyToNil(binding.ReadWhen), binding.ShadowsGlobal, binding.CreatedAt, binding.UpdatedAt,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateBinding
		}
		return nil, fmt.Errorf("creating knowledge binding: %w", err)
	}
	return created, nil
}

func (s *Store) GetBinding(ctx context.Context, bindingID int64) (*KnowledgeBinding, error) {
	binding, err := scanBinding(s.pool.QueryRow(ctx, getBindingSQL, bindingID))
	if err != nil {
		return nil, fmt.Errorf("getting knowledge binding: %w", err)
	}
	return binding, nil
}

func (s *Store) ListBindings(ctx context.Context, query BindingListQuery) (BindingListResult, error) {
	queryLimit := query.Limit
	if queryLimit > 0 {
		queryLimit++
	}
	rows, err := s.pool.Query(ctx, listBindingsSQL, queryLimit, query.Offset)
	if err != nil {
		return BindingListResult{}, fmt.Errorf("listing knowledge bindings: %w", err)
	}
	defer rows.Close()
	bindings := []KnowledgeBinding{}
	for rows.Next() {
		binding, scanErr := scanBinding(rows)
		if scanErr != nil {
			return BindingListResult{}, scanErr
		}
		bindings = append(bindings, *binding)
	}
	if err := rows.Err(); err != nil {
		return BindingListResult{}, fmt.Errorf("scanning knowledge bindings: %w", err)
	}
	result := BindingListResult{Bindings: bindings}
	if query.Limit > 0 && len(result.Bindings) > query.Limit {
		result.Bindings = result.Bindings[:query.Limit]
		next := query.Offset + query.Limit
		result.NextOffset = &next
	}
	return result, nil
}

func (s *Store) UpdateBinding(ctx context.Context, binding *KnowledgeBinding) (*KnowledgeBinding, error) {
	updated, err := scanBinding(s.pool.QueryRow(ctx, updateBindingSQL,
		binding.ID, binding.ScopeKind, binding.ScopeRef, jsonOrNil(binding.Audience), binding.Priority, binding.SortOrder,
		binding.ReadPolicy, emptyToNil(binding.ReadWhen), binding.ShadowsGlobal, binding.UpdatedAt,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingNotFound
		}
		if isUniqueViolation(err) {
			return nil, ErrDuplicateBinding
		}
		return nil, fmt.Errorf("updating knowledge binding: %w", err)
	}
	return updated, nil
}

func (s *Store) DeleteBinding(ctx context.Context, bindingID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, deleteBindingSQL, bindingID)
	if err != nil {
		return false, fmt.Errorf("deleting knowledge binding: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(row rowScanner) (*Entry, error) {
	var entry Entry
	var audience []byte
	var notes *string
	if err := row.Scan(&entry.ID, &entry.ProjectID, &entry.DocumentProjectID, &entry.DocumentSlug, &entry.Importance, &audience, &entry.SortOrder, &notes, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEntryNotFound
		}
		return nil, fmt.Errorf("scanning guidance entry: %w", err)
	}
	entry.Audience = audienceFromJSON(audience)
	if notes != nil {
		entry.Notes = *notes
	}
	return &entry, nil
}

func scanBinding(row rowScanner) (*KnowledgeBinding, error) {
	var binding KnowledgeBinding
	var audience []byte
	var readWhen *string
	if err := row.Scan(&binding.ID, &binding.TargetKind, &binding.TargetRef, &binding.ScopeKind, &binding.ScopeRef, &audience,
		&binding.Priority, &binding.SortOrder, &binding.ReadPolicy, &readWhen, &binding.ShadowsGlobal, &binding.CreatedAt, &binding.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingNotFound
		}
		return nil, fmt.Errorf("scanning knowledge binding: %w", err)
	}
	binding.Audience = audienceFromJSON(audience)
	if readWhen != nil {
		binding.ReadWhen = *readWhen
	}
	return &binding, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func jsonOrNil(values []string) []byte {
	if len(values) == 0 {
		return nil
	}
	data, _ := json.Marshal(values)
	return data
}

func audienceFromJSON(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil
	}
	normalized, err := normalizeAudience(values)
	if err != nil {
		return nil
	}
	return normalized
}

func emptyToNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

const entryColumns = `id, project_id, document_project_id, document_slug, importance, audience, sort_order, notes, created_at, updated_at`

const upsertEntrySQL = `
insert into den_guidance.agent_guidance_entries(project_id, document_project_id, document_slug, importance, audience, sort_order, notes, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
on conflict (project_id, document_project_id, document_slug) do update
set importance = excluded.importance,
    audience = excluded.audience,
    sort_order = excluded.sort_order,
    notes = excluded.notes,
    updated_at = excluded.updated_at
returning ` + entryColumns

const listEntriesSQL = `
select ` + entryColumns + `
from den_guidance.agent_guidance_entries
where project_id = $1
   or ($2::boolean = true and project_id = $3)
order by
  case when project_id = $3 then 0 else 1 end,
  sort_order asc,
  case importance when 'required' then 0 else 1 end,
  document_project_id asc,
  document_slug asc,
  id asc`

const deleteEntrySQL = `
delete from den_guidance.agent_guidance_entries
where project_id = $1 and id = $2`

const documentReferencesSQL = `
select id, project_id, importance, audience, sort_order, coalesce(notes, ''), created_at, updated_at
from den_guidance.agent_guidance_entries
where document_project_id = $1 and document_slug = $2
order by project_id asc, sort_order asc, id asc`

const bindingColumns = `id, target_kind, target_ref, scope_kind, scope_ref, audience, priority, sort_order, read_policy, read_when, shadows_global, created_at, updated_at`

const createBindingSQL = `
insert into den_guidance.knowledge_bindings(target_kind, target_ref, scope_kind, scope_ref, audience, priority, sort_order, read_policy, read_when, shadows_global, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
returning ` + bindingColumns

const getBindingSQL = `select ` + bindingColumns + ` from den_guidance.knowledge_bindings where id = $1`

const listBindingsSQL = `select ` + bindingColumns + ` from den_guidance.knowledge_bindings order by scope_kind, scope_ref, target_ref, id limit nullif($1, 0) offset $2`

const updateBindingSQL = `
update den_guidance.knowledge_bindings
set scope_kind = $2, scope_ref = $3, audience = $4, priority = $5, sort_order = $6, read_policy = $7, read_when = $8, shadows_global = $9, updated_at = $10
where id = $1
returning ` + bindingColumns

const deleteBindingSQL = `delete from den_guidance.knowledge_bindings where id = $1`
