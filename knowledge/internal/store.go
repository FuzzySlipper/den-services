package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
		return fmt.Errorf("pinging knowledge store: %w", err)
	}
	return nil
}

func (s *Store) UpsertEntry(ctx context.Context, entry *Entry, changeNote string) (*Entry, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning knowledge upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existingID int64
	existing, err := scanEntry(tx.QueryRow(ctx, getEntryBySlugForUpdateSQL, entry.Slug()))
	switch {
	case err == nil:
		existingID = existing.ID()
		tags, err := loadTags(ctx, tx, existing.ID())
		if err != nil {
			return nil, err
		}
		existing.tags = tags
		if err := insertRevision(ctx, tx, existing, entry.UpdatedBy(), changeNote); err != nil {
			return nil, err
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, fmt.Errorf("loading existing knowledge entry: %w", err)
	}
	created, err := scanEntry(tx.QueryRow(ctx, upsertEntrySQL,
		entry.Slug(),
		entry.Title(),
		emptyToNil(entry.Summary()),
		entry.BodyMarkdown(),
		entry.Kind(),
		entry.Status(),
		entry.CurationState(),
		jsonOrNil(entry.Audience()),
		jsonOrNil(entry.Aliases()),
		jsonOrNilSourceRefs(entry.SourceRefs()),
		emptyToNil(entry.AccuracyNotes()),
		emptyToNil(entry.ReplacementSlug()),
		timeOrNil(entry.LastReviewedAt()),
		timeOrNil(entry.ReviewDueAt()),
		emptyToNil(entry.CreatedBy()),
		emptyToNil(entry.UpdatedBy()),
		entry.CreatedAt(),
		entry.UpdatedAt(),
	))
	if err != nil {
		return nil, fmt.Errorf("upserting knowledge entry: %w", err)
	}
	if existingID != 0 {
		if _, err := tx.Exec(ctx, deleteTagsSQL, existingID); err != nil {
			return nil, fmt.Errorf("deleting knowledge tags: %w", err)
		}
	}
	for _, tag := range entry.Tags() {
		if _, err := tx.Exec(ctx, insertTagSQL, created.ID(), tag); err != nil {
			return nil, fmt.Errorf("inserting knowledge tag: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing knowledge upsert: %w", err)
	}
	created.tags = entry.Tags()
	return created, nil
}

func (s *Store) GetEntry(ctx context.Context, slug string, includeArchived bool) (*Entry, error) {
	entry, err := scanEntry(s.pool.QueryRow(ctx, getEntrySQL, slug, includeArchived))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entryNotFound(slug)
	}
	if err != nil {
		return nil, fmt.Errorf("getting knowledge entry %s: %w", slug, err)
	}
	tags, err := s.loadTags(ctx, entry.ID())
	if err != nil {
		return nil, err
	}
	entry.tags = tags
	return entry, nil
}

func (s *Store) ListEntries(ctx context.Context, query ListQuery) ([]EntrySummary, error) {
	rows, err := s.pool.Query(ctx, listEntriesSQL,
		emptyToNil(query.Kind),
		emptyToNil(query.Status),
		query.IncludeDeprecated,
		query.IncludeUnreviewed,
		query.IncludeArchived,
		query.RequiredTags,
		query.AnyTags,
		query.Audience,
		query.Limit,
		query.Offset,
	)
	if err != nil {
		return nil, fmt.Errorf("listing knowledge entries: %w", err)
	}
	defer rows.Close()
	summaries := []EntrySummary{}
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning knowledge summaries: %w", err)
	}
	for i := range summaries {
		tags, err := s.loadTags(ctx, summaries[i].ID)
		if err != nil {
			return nil, err
		}
		summaries[i].Tags = tags
	}
	return summaries, nil
}

func (s *Store) SearchEntries(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	rows, err := s.pool.Query(ctx, searchEntriesSQL,
		query.Query,
		headlineOptions,
		emptyToNil(query.Kind),
		emptyToNil(query.Status),
		query.IncludeDeprecated,
		query.IncludeUnreviewed,
		query.IncludeArchived,
		query.RequiredTags,
		query.AnyTags,
		query.Audience,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("searching knowledge entries: %w", err)
	}
	defer rows.Close()
	results := []SearchResult{}
	for rows.Next() {
		result, err := scanSearchResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning knowledge search results: %w", err)
	}
	for i := range results {
		tags, err := s.loadTagsBySlug(ctx, results[i].Slug)
		if err != nil {
			return nil, err
		}
		results[i].Tags = tags
	}
	return results, nil
}

func (s *Store) ListRevisions(ctx context.Context, slug string) ([]RevisionSummary, error) {
	rows, err := s.pool.Query(ctx, listRevisionsSQL, slug)
	if err != nil {
		return nil, fmt.Errorf("listing knowledge revisions: %w", err)
	}
	defer rows.Close()
	revisions := []RevisionSummary{}
	for rows.Next() {
		var revision RevisionSummary
		if err := rows.Scan(&revision.ID, &revision.EntryID, &revision.RevisionNumber, &revision.Title, &revision.Kind, &revision.Status, &revision.CurationState, &revision.ChangeNote, &revision.ChangedBy, &revision.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning knowledge revision: %w", err)
		}
		revisions = append(revisions, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning knowledge revisions: %w", err)
	}
	return revisions, nil
}

func insertRevision(ctx context.Context, tx pgx.Tx, existing *Entry, changedBy string, changeNote string) error {
	var nextRevision int
	if err := tx.QueryRow(ctx, nextRevisionSQL, existing.ID()).Scan(&nextRevision); err != nil {
		return fmt.Errorf("finding next knowledge revision: %w", err)
	}
	if _, err := tx.Exec(ctx, insertRevisionSQL,
		existing.ID(),
		nextRevision,
		existing.Title(),
		emptyToNil(existing.Summary()),
		existing.BodyMarkdown(),
		existing.Kind(),
		existing.Status(),
		existing.CurationState(),
		jsonOrNil(existing.Tags()),
		jsonOrNil(existing.Audience()),
		jsonOrNil(existing.Aliases()),
		jsonOrNilSourceRefs(existing.SourceRefs()),
		emptyToNil(existing.AccuracyNotes()),
		emptyToNil(existing.ReplacementSlug()),
		emptyToNil(changedBy),
		emptyToNil(changeNote),
	); err != nil {
		return fmt.Errorf("inserting knowledge revision: %w", err)
	}
	return nil
}

func (s *Store) loadTags(ctx context.Context, entryID int64) ([]string, error) {
	return loadTags(ctx, s.pool, entryID)
}

type tagQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadTags(ctx context.Context, querier tagQuerier, entryID int64) ([]string, error) {
	rows, err := querier.Query(ctx, loadTagsSQL, entryID)
	if err != nil {
		return nil, fmt.Errorf("loading knowledge tags: %w", err)
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("scanning knowledge tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *Store) loadTagsBySlug(ctx context.Context, slug string) ([]string, error) {
	rows, err := s.pool.Query(ctx, loadTagsBySlugSQL, slug)
	if err != nil {
		return nil, fmt.Errorf("loading knowledge tags by slug: %w", err)
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("scanning knowledge tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEntry(row scanner) (*Entry, error) {
	var params NewEntryParams
	var audience []byte
	var aliases []byte
	var sourceRefs []byte
	if err := row.Scan(&params.ID, &params.Slug, &params.Title, &params.Summary, &params.BodyMarkdown, &params.Kind, &params.Status, &params.CurationState, &audience, &aliases, &sourceRefs, &params.AccuracyNotes, &params.ReplacementSlug, &params.LastReviewedAt, &params.ReviewDueAt, &params.CreatedBy, &params.UpdatedBy, &params.CreatedAt, &params.UpdatedAt); err != nil {
		return nil, err
	}
	if err := decodeJSON(audience, &params.Audience); err != nil {
		return nil, err
	}
	if err := decodeJSON(aliases, &params.Aliases); err != nil {
		return nil, err
	}
	if err := decodeJSON(sourceRefs, &params.SourceRefs); err != nil {
		return nil, err
	}
	return NewEntry(params)
}

func scanSummary(row scanner) (EntrySummary, error) {
	var summary EntrySummary
	var audience []byte
	var aliases []byte
	var sourceRefs []byte
	if err := row.Scan(&summary.ID, &summary.Slug, &summary.Title, &summary.Summary, &summary.Kind, &summary.Status, &summary.CurationState, &audience, &aliases, &sourceRefs, &summary.AccuracyNotes, &summary.ReplacementSlug, &summary.LastReviewedAt, &summary.ReviewDueAt, &summary.CreatedBy, &summary.UpdatedBy, &summary.CreatedAt, &summary.UpdatedAt); err != nil {
		return EntrySummary{}, fmt.Errorf("scanning knowledge summary: %w", err)
	}
	if err := decodeJSON(audience, &summary.Audience); err != nil {
		return EntrySummary{}, err
	}
	if err := decodeJSON(aliases, &summary.Aliases); err != nil {
		return EntrySummary{}, err
	}
	if err := decodeJSON(sourceRefs, &summary.SourceRefs); err != nil {
		return EntrySummary{}, err
	}
	return summary, nil
}

func scanSearchResult(row scanner) (SearchResult, error) {
	var result SearchResult
	var audience []byte
	var aliases []byte
	var sourceRefs []byte
	if err := row.Scan(&result.Slug, &result.Title, &result.Summary, &result.Kind, &result.Status, &result.CurationState, &audience, &aliases, &sourceRefs, &result.Snippet, &result.Rank, &result.UpdatedAt, &result.LastReviewedAt); err != nil {
		return SearchResult{}, fmt.Errorf("scanning knowledge search result: %w", err)
	}
	if err := decodeJSON(audience, &result.Audience); err != nil {
		return SearchResult{}, err
	}
	if err := decodeJSON(aliases, &result.Aliases); err != nil {
		return SearchResult{}, err
	}
	if err := decodeJSON(sourceRefs, &result.SourceRefs); err != nil {
		return SearchResult{}, err
	}
	return result, nil
}

func decodeJSON(data []byte, dest any) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decoding knowledge json: %w", err)
	}
	return nil
}

func jsonOrNil(value []string) any {
	if len(value) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func jsonOrNilSourceRefs(value []SourceRef) any {
	if len(value) == 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func timeOrNil(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

const (
	entryColumns   = `id, slug, title, coalesce(summary, ''), body_markdown, kind, status, curation_state, audience_json, aliases_json, source_refs_json, coalesce(accuracy_notes, ''), coalesce(replacement_slug, ''), last_reviewed_at, review_due_at, coalesce(created_by, ''), coalesce(updated_by, ''), created_at, updated_at`
	summaryColumns = `id, slug, title, coalesce(summary, ''), kind, status, curation_state, audience_json, aliases_json, source_refs_json, coalesce(accuracy_notes, ''), coalesce(replacement_slug, ''), last_reviewed_at, review_due_at, coalesce(created_by, ''), coalesce(updated_by, ''), created_at, updated_at`
)

const headlineOptions = `StartSel=<b>, StopSel=</b>, MaxWords=32, MinWords=8, ShortWord=3, HighlightAll=false, MaxFragments=2, FragmentDelimiter=...`

const getEntryBySlugForUpdateSQL = `select ` + entryColumns + ` from den_knowledge.knowledge_entries where slug = $1 for update`

const upsertEntrySQL = `
insert into den_knowledge.knowledge_entries(slug, title, summary, body_markdown, kind, status, curation_state, audience_json, aliases_json, source_refs_json, accuracy_notes, replacement_slug, last_reviewed_at, review_due_at, created_by, updated_by, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
on conflict (slug) do update set
  title = excluded.title,
  summary = excluded.summary,
  body_markdown = excluded.body_markdown,
  kind = excluded.kind,
  status = excluded.status,
  curation_state = excluded.curation_state,
  audience_json = excluded.audience_json,
  aliases_json = excluded.aliases_json,
  source_refs_json = excluded.source_refs_json,
  accuracy_notes = excluded.accuracy_notes,
  replacement_slug = excluded.replacement_slug,
  last_reviewed_at = excluded.last_reviewed_at,
  review_due_at = excluded.review_due_at,
  updated_by = excluded.updated_by,
  updated_at = excluded.updated_at
returning ` + entryColumns

const getEntrySQL = `
select ` + entryColumns + `
from den_knowledge.knowledge_entries
where slug = $1 and ($2::boolean or status != 'archived')`

const listEntriesSQL = `
select ` + summaryColumns + `
from den_knowledge.knowledge_entries ke
where ($1::text is null or ke.kind = $1)
  and (
    ($2::text is not null and ke.status = any(string_to_array($2, ',')))
    or ($2::text is null and (ke.status = 'reviewed' or ($3::boolean and ke.status = 'deprecated') or ($4::boolean and ke.status in ('draft', 'needs_review'))))
  )
  and ($5::boolean or ke.status != 'archived')
  and (coalesce(array_length($6::text[], 1), 0) = 0 or exists (
    select 1 from den_knowledge.knowledge_entry_tags ket
    where ket.entry_id = ke.id
    group by ket.entry_id
    having array_agg(ket.tag order by ket.tag) @> $6::text[]
  ))
  and (coalesce(array_length($7::text[], 1), 0) = 0 or exists (
    select 1 from den_knowledge.knowledge_entry_tags ket2
    where ket2.entry_id = ke.id and ket2.tag = any($7::text[])
  ))
  and (coalesce(array_length($8::text[], 1), 0) = 0 or coalesce(ke.audience_json, '[]'::jsonb) ?& $8::text[])
order by ke.updated_at desc, ke.id desc
limit $9 offset $10`

const searchEntriesSQL = `
with search as (
  select websearch_to_tsquery('english', $1) as query
)
select ke.slug, ke.title, coalesce(ke.summary, ''), ke.kind, ke.status, ke.curation_state,
       ke.audience_json, ke.aliases_json, ke.source_refs_json,
       ts_headline('english', coalesce(ke.summary, '') || ' ' || coalesce(ke.body_markdown, ''), search.query, $2) as snippet,
       ts_rank_cd(ke.search_vector, search.query) as rank,
       ke.updated_at, ke.last_reviewed_at
from den_knowledge.knowledge_entries ke
cross join search
where ke.search_vector @@ search.query
  and ($3::text is null or ke.kind = $3)
  and (
    ($4::text is not null and ke.status = any(string_to_array($4, ',')))
    or ($4::text is null and (ke.status = 'reviewed' or ($5::boolean and ke.status = 'deprecated') or ($6::boolean and ke.status in ('draft', 'needs_review'))))
  )
  and ($7::boolean or ke.status != 'archived')
  and (coalesce(array_length($8::text[], 1), 0) = 0 or exists (
    select 1 from den_knowledge.knowledge_entry_tags ket
    where ket.entry_id = ke.id
    group by ket.entry_id
    having array_agg(ket.tag order by ket.tag) @> $8::text[]
  ))
  and (coalesce(array_length($9::text[], 1), 0) = 0 or exists (
    select 1 from den_knowledge.knowledge_entry_tags ket2
    where ket2.entry_id = ke.id and ket2.tag = any($9::text[])
  ))
  and (coalesce(array_length($10::text[], 1), 0) = 0 or coalesce(ke.audience_json, '[]'::jsonb) ?& $10::text[])
order by rank desc, ke.updated_at desc, ke.id desc
limit $11`

const nextRevisionSQL = `select coalesce(max(revision_number), 0) + 1 from den_knowledge.knowledge_entry_revisions where entry_id = $1`

const insertRevisionSQL = `
insert into den_knowledge.knowledge_entry_revisions(entry_id, revision_number, title, summary, body_markdown, kind, status, curation_state, tags_json, audience_json, aliases_json, source_refs_json, accuracy_notes, replacement_slug, changed_by, change_note)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

const listRevisionsSQL = `
select kr.id, kr.entry_id, kr.revision_number, kr.title, kr.kind, kr.status, kr.curation_state, coalesce(kr.change_note, ''), coalesce(kr.changed_by, ''), kr.created_at
from den_knowledge.knowledge_entry_revisions kr
join den_knowledge.knowledge_entries ke on ke.id = kr.entry_id
where ke.slug = $1
order by kr.revision_number desc`

const (
	deleteTagsSQL     = `delete from den_knowledge.knowledge_entry_tags where entry_id = $1`
	insertTagSQL      = `insert into den_knowledge.knowledge_entry_tags(entry_id, tag) values ($1, $2) on conflict do nothing`
	loadTagsSQL       = `select tag from den_knowledge.knowledge_entry_tags where entry_id = $1 order by tag`
	loadTagsBySlugSQL = `
select ket.tag
from den_knowledge.knowledge_entry_tags ket
join den_knowledge.knowledge_entries ke on ke.id = ket.entry_id
where ke.slug = $1
order by ket.tag`
)
