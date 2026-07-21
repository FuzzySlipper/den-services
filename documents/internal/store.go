package documents

import (
	"context"
	"database/sql"
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
		return fmt.Errorf("pinging documents store: %w", err)
	}
	return nil
}

func (s *Store) UpsertDocument(ctx context.Context, document *Document) (*Document, error) {
	doc, err := scanDocument(s.pool.QueryRow(ctx, upsertDocumentSQL,
		document.ProjectID(),
		document.Slug(),
		document.Title(),
		document.Content(),
		document.DocType(),
		jsonOrNil(document.Tags()),
		emptyToNil(document.Summary()),
		document.CreatedAt(),
		document.UpdatedAt(),
	))
	if err != nil {
		return nil, fmt.Errorf("upserting document: %w", err)
	}
	return doc, nil
}

func (s *Store) GetDocument(ctx context.Context, projectID string, slug string) (*Document, error) {
	doc, err := scanDocument(s.pool.QueryRow(ctx, getDocumentSQL, projectID, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, documentNotFound(projectID, slug)
	}
	if err != nil {
		return nil, fmt.Errorf("getting document %s/%s: %w", projectID, slug, err)
	}
	return doc, nil
}

func (s *Store) ListDocuments(ctx context.Context, query ListDocumentsQuery) ([]DocumentSummary, error) {
	rows, err := s.pool.Query(ctx, listDocumentsSQL,
		emptyToNil(query.ProjectID),
		emptyToNil(query.DocType),
		query.Tags,
		query.HasVisibility,
		emptyToNil(query.Visibility),
	)
	if err != nil {
		return nil, fmt.Errorf("listing documents: %w", err)
	}
	defer rows.Close()
	var docs []DocumentSummary
	for rows.Next() {
		summary, err := scanDocumentSummary(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning documents: %w", err)
	}
	return docs, nil
}

func (s *Store) SearchDocuments(ctx context.Context, query string, projectID string, visibility string) ([]DocumentSearchResult, error) {
	rows, err := s.pool.Query(ctx, searchDocumentsSQL, query, emptyToNil(projectID), visibility, headlineOptions)
	if err != nil {
		return nil, fmt.Errorf("searching documents: %w", err)
	}
	defer rows.Close()
	var results []DocumentSearchResult
	for rows.Next() {
		var result DocumentSearchResult
		if err := rows.Scan(&result.ProjectID, &result.Slug, &result.Title, &result.DocType, &result.Visibility, &result.Summary, &result.Snippet, &result.Rank); err != nil {
			return nil, fmt.Errorf("scanning document search result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning document search results: %w", err)
	}
	return results, nil
}

func (s *Store) DeleteDocument(ctx context.Context, projectID string, slug string) (bool, error) {
	tag, err := s.pool.Exec(ctx, deleteDocumentSQL, projectID, slug)
	if err != nil {
		return false, fmt.Errorf("deleting document %s/%s: %w", projectID, slug, err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) UpdateVisibility(ctx context.Context, projectID string, slug string, visibility string, updatedAt time.Time) (*Document, error) {
	doc, err := scanDocument(s.pool.QueryRow(ctx, updateVisibilitySQL, projectID, slug, visibility, updatedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, documentNotFound(projectID, slug)
	}
	if err != nil {
		return nil, fmt.Errorf("updating document visibility: %w", err)
	}
	return doc, nil
}

func (s *Store) GetOrCreateThread(ctx context.Context, projectID string, slug string, threadKey string, title string, createdBy string, targetAnchor string, now time.Time) (*DiscussionThread, error) {
	thread, err := scanThread(s.pool.QueryRow(ctx, getThreadByTargetSQL, projectID, slug, threadKey))
	if err == nil {
		return thread, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("getting discussion thread: %w", err)
	}
	return s.CreateThread(ctx, DiscussionThread{
		TargetType:      TargetTypeDocument,
		TargetProjectID: projectID,
		TargetSlug:      slug,
		TargetAnchor:    targetAnchor,
		ThreadKey:       threadKey,
		Title:           title,
		Status:          ThreadStatusOpen,
		CreatedBy:       createdBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
}

func (s *Store) CreateThread(ctx context.Context, thread DiscussionThread) (*DiscussionThread, error) {
	created, err := scanThread(s.pool.QueryRow(ctx, createThreadSQL,
		thread.TargetType,
		thread.TargetProjectID,
		thread.TargetID,
		emptyToNil(thread.TargetSlug),
		emptyToNil(thread.TargetAnchor),
		thread.ThreadKey,
		thread.Title,
		thread.Status,
		thread.CreatedBy,
		emptyToNil(thread.Summary),
		emptyToNil(thread.ResolutionSummary),
		bytesOrNil(thread.MetadataJSON),
		thread.CreatedAt,
		thread.UpdatedAt,
	))
	if err != nil {
		return nil, fmt.Errorf("creating discussion thread: %w", err)
	}
	return created, nil
}

func (s *Store) GetThread(ctx context.Context, id int64) (*DiscussionThread, error) {
	thread, err := scanThread(s.pool.QueryRow(ctx, getThreadSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, threadNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting discussion thread %d: %w", id, err)
	}
	return thread, nil
}

func (s *Store) ListThreads(ctx context.Context, query ListThreadsQuery) ([]DiscussionThread, error) {
	limit := normalizeDiscussionThreadLimit(query.Limit)
	rows, err := s.pool.Query(ctx, listThreadsSQL, query.TargetType, query.TargetProjectID, query.TargetSlug, emptyToNil(query.Status), limit)
	if err != nil {
		return nil, fmt.Errorf("listing discussion threads: %w", err)
	}
	defer rows.Close()
	var threads []DiscussionThread
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, *thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning discussion threads: %w", err)
	}
	return threads, nil
}

func (s *Store) UpdateThread(ctx context.Context, id int64, patch ThreadPatch, updatedAt time.Time) (*DiscussionThread, error) {
	thread, err := scanThread(s.pool.QueryRow(ctx, updateThreadSQL,
		id,
		patch.Status,
		patch.Title,
		patch.Summary,
		patch.ResolutionSummary,
		patch.HasMetadata,
		bytesOrNil(patch.MetadataJSON),
		updatedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, threadNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("updating discussion thread: %w", err)
	}
	return thread, nil
}

func (s *Store) AddComment(ctx context.Context, comment DiscussionComment, now time.Time) (*DiscussionComment, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning add comment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created, err := scanComment(tx.QueryRow(ctx, addCommentSQL,
		comment.ThreadID,
		comment.ParentCommentID,
		comment.AuthorIdentity,
		comment.BodyMarkdown,
		comment.CommentKind,
		comment.Status,
		bytesOrNil(comment.MentionsJSON),
		bytesOrNil(comment.SourceRefsJSON),
		bytesOrNil(comment.MetadataJSON),
		now,
		now,
	))
	if err != nil {
		return nil, fmt.Errorf("adding discussion comment: %w", err)
	}
	if _, err := tx.Exec(ctx, touchThreadSQL, comment.ThreadID, now); err != nil {
		return nil, fmt.Errorf("touching discussion thread: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing add comment: %w", err)
	}
	return created, nil
}

func (s *Store) GetComment(ctx context.Context, id int64) (*DiscussionComment, error) {
	comment, err := scanComment(s.pool.QueryRow(ctx, getCommentSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, commentNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting discussion comment %d: %w", id, err)
	}
	return comment, nil
}

func (s *Store) ListComments(ctx context.Context, threadID int64) ([]DiscussionComment, error) {
	rows, err := s.pool.Query(ctx, listCommentsSQL, threadID)
	if err != nil {
		return nil, fmt.Errorf("listing discussion comments: %w", err)
	}
	defer rows.Close()
	var comments []DiscussionComment
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, *comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning discussion comments: %w", err)
	}
	return comments, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDocument(row scanner) (*Document, error) {
	var params NewDocumentParams
	var tags []byte
	var summary sql.NullString
	if err := row.Scan(&params.ID, &params.ProjectID, &params.Slug, &params.Title, &params.Content, &params.DocType, &params.Visibility, &tags, &summary, &params.CreatedAt, &params.UpdatedAt); err != nil {
		return nil, err
	}
	params.Summary = nullableString(summary)
	if len(tags) > 0 {
		if err := json.Unmarshal(tags, &params.Tags); err != nil {
			return nil, fmt.Errorf("decoding document tags: %w", err)
		}
	}
	return NewDocument(params)
}

func scanDocumentSummary(row scanner) (DocumentSummary, error) {
	var summary DocumentSummary
	var tags []byte
	var summaryText sql.NullString
	if err := row.Scan(&summary.ID, &summary.ProjectID, &summary.Slug, &summary.Title, &summary.DocType, &summary.Visibility, &tags, &summaryText, &summary.UpdatedAt); err != nil {
		return DocumentSummary{}, fmt.Errorf("scanning document summary: %w", err)
	}
	summary.Summary = nullableString(summaryText)
	if len(tags) > 0 {
		if err := json.Unmarshal(tags, &summary.Tags); err != nil {
			return DocumentSummary{}, fmt.Errorf("decoding document summary tags: %w", err)
		}
	}
	return summary, nil
}

func scanThread(row scanner) (*DiscussionThread, error) {
	var thread DiscussionThread
	var metadata []byte
	var targetProjectID sql.NullString
	var targetSlug sql.NullString
	var targetAnchor sql.NullString
	var summary sql.NullString
	var resolutionSummary sql.NullString
	if err := row.Scan(
		&thread.ID,
		&thread.TargetType,
		&targetProjectID,
		&thread.TargetID,
		&targetSlug,
		&targetAnchor,
		&thread.ThreadKey,
		&thread.Title,
		&thread.Status,
		&thread.CreatedBy,
		&summary,
		&resolutionSummary,
		&metadata,
		&thread.LastCommentAt,
		&thread.CreatedAt,
		&thread.UpdatedAt,
	); err != nil {
		return nil, err
	}
	thread.TargetProjectID = nullableString(targetProjectID)
	thread.TargetSlug = nullableString(targetSlug)
	thread.TargetAnchor = nullableString(targetAnchor)
	thread.Summary = nullableString(summary)
	thread.ResolutionSummary = nullableString(resolutionSummary)
	thread.MetadataJSON = cloneBytes(metadata)
	return &thread, nil
}

func nullableString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func scanComment(row scanner) (*DiscussionComment, error) {
	var comment DiscussionComment
	var mentions []byte
	var refs []byte
	var metadata []byte
	if err := row.Scan(&comment.ID, &comment.ThreadID, &comment.ParentCommentID, &comment.AuthorIdentity, &comment.BodyMarkdown, &comment.CommentKind, &comment.Status, &mentions, &refs, &metadata, &comment.CreatedAt, &comment.EditedAt, &comment.UpdatedAt); err != nil {
		return nil, err
	}
	comment.MentionsJSON = cloneBytes(mentions)
	comment.SourceRefsJSON = cloneBytes(refs)
	comment.MetadataJSON = cloneBytes(metadata)
	return &comment, nil
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

func bytesOrNil(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

const (
	documentColumns = `id, project_id, slug, title, content, doc_type, visibility, tags, summary, created_at, updated_at`
	threadColumns   = `id, target_type, target_project_id, target_id, target_slug, target_anchor, thread_key, title, status, created_by, summary, resolution_summary, metadata_json, last_comment_at, created_at, updated_at`
	commentColumns  = `id, thread_id, parent_comment_id, author_identity, body_markdown, comment_kind, status, mentions_json, source_refs_json, metadata_json, created_at, edited_at, updated_at`
)

const headlineOptions = `StartSel=<b>, StopSel=</b>, MaxWords=32, MinWords=8, ShortWord=3, HighlightAll=false, MaxFragments=2, FragmentDelimiter=...`

const upsertDocumentSQL = `
insert into den_documents.documents(project_id, slug, title, content, doc_type, tags, summary, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
on conflict (project_id, slug) do update set
  title = excluded.title,
  content = excluded.content,
  doc_type = excluded.doc_type,
  tags = excluded.tags,
  summary = excluded.summary,
  updated_at = excluded.updated_at
returning ` + documentColumns

const getDocumentSQL = `select ` + documentColumns + ` from den_documents.documents where project_id = $1 and slug = $2`

const listDocumentsSQL = `
select id, project_id, slug, title, doc_type, visibility, tags, summary, updated_at
from den_documents.documents
where ($1::text is null or project_id = $1)
  and ($2::text is null or doc_type = $2)
  and (coalesce(array_length($3::text[], 1), 0) = 0 or tags ?& $3::text[])
  and (($4::boolean = true and visibility = $5) or ($4::boolean = false and visibility = 'normal'))
order by updated_at desc, id desc`

const searchDocumentsSQL = `
with search as (
  select websearch_to_tsquery('english', $1) as query
)
select d.project_id, d.slug, d.title, d.doc_type, d.visibility, coalesce(d.summary, ''),
       ts_headline('english', coalesce(d.content, d.summary, d.title, ''), search.query, $4) as snippet,
       ts_rank_cd(d.search_vector, search.query) as rank
from den_documents.documents d
cross join search
where d.search_vector @@ search.query
  and ($2::text is null or d.project_id = $2)
  and d.visibility = $3
order by rank desc, d.updated_at desc, d.id desc`

const deleteDocumentSQL = `delete from den_documents.documents where project_id = $1 and slug = $2`

const updateVisibilitySQL = `
update den_documents.documents
set visibility = $3, updated_at = $4
where project_id = $1 and slug = $2
returning ` + documentColumns

const getThreadByTargetSQL = `
select ` + threadColumns + `
from den_documents.discussion_threads
where target_type = 'document' and target_project_id = $1 and target_slug = $2 and thread_key = $3`

const createThreadSQL = `
insert into den_documents.discussion_threads(target_type, target_project_id, target_id, target_slug, target_anchor, thread_key, title, status, created_by, summary, resolution_summary, metadata_json, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
on conflict (target_type, coalesce(target_project_id, ''), coalesce(target_slug, ''), coalesce(target_id, -1), coalesce(target_anchor, ''), coalesce(thread_key, '')) do update set updated_at = den_documents.discussion_threads.updated_at
returning ` + threadColumns

const getThreadSQL = `select ` + threadColumns + ` from den_documents.discussion_threads where id = $1`

const listThreadsSQL = `
select ` + threadColumns + `
from den_documents.discussion_threads
where target_type = $1
  and target_project_id = $2
  and target_slug = $3
  and ($4::text is null or status = $4)
order by updated_at desc, id desc
limit $5`

const updateThreadSQL = `
update den_documents.discussion_threads
set status = coalesce($2, status),
    title = coalesce($3, title),
    summary = coalesce($4, summary),
    resolution_summary = coalesce($5, resolution_summary),
    metadata_json = case when $6::boolean then $7 else metadata_json end,
    updated_at = $8
where id = $1
returning ` + threadColumns

const addCommentSQL = `
insert into den_documents.discussion_comments(thread_id, parent_comment_id, author_identity, body_markdown, comment_kind, status, mentions_json, source_refs_json, metadata_json, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
returning ` + commentColumns

const touchThreadSQL = `
update den_documents.discussion_threads
set last_comment_at = $2, updated_at = $2
where id = $1`

const getCommentSQL = `select ` + commentColumns + ` from den_documents.discussion_comments where id = $1`

const listCommentsSQL = `
select ` + commentColumns + `
from den_documents.discussion_comments
where thread_id = $1
order by created_at asc, id asc`
