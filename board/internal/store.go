package board

import (
	"context"
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
		return fmt.Errorf("pinging board store: %w", err)
	}
	return nil
}

func (s *Store) CreatePost(ctx context.Context, post *Post) (*Post, error) {
	created, err := scanPost(s.pool.QueryRow(ctx, createPostSQL,
		post.ProjectID, post.Title, post.BodyMarkdown, post.AuthorIdentity, jsonOrNil(post.MetadataJSON), post.CreatedAt, post.UpdatedAt,
	))
	if err != nil {
		return nil, fmt.Errorf("creating board post: %w", err)
	}
	return created, nil
}

func (s *Store) GetPost(ctx context.Context, id int64) (*Post, error) {
	post, err := scanPost(s.pool.QueryRow(ctx, getPostSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting board post: %w", err)
	}
	return post, nil
}

func (s *Store) ListPosts(ctx context.Context, query ListPostsQuery) (PostPage, error) {
	rows, err := s.pool.Query(ctx, listPostsSQL, query.ProjectID, query.AfterID, query.Limit+1)
	if err != nil {
		return PostPage{}, fmt.Errorf("listing board posts: %w", err)
	}
	defer rows.Close()
	posts := make([]PostSummary, 0, query.Limit)
	for rows.Next() {
		var post PostSummary
		if err := rows.Scan(&post.ID, &post.ProjectID, &post.Title, &post.AuthorIdentity, &post.CreatedAt, &post.UpdatedAt); err != nil {
			return PostPage{}, fmt.Errorf("scanning board post summary: %w", err)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return PostPage{}, fmt.Errorf("scanning board post summaries: %w", err)
	}
	return postPage(posts, query.Limit), nil
}

func (s *Store) Search(ctx context.Context, query SearchQuery) (SearchPage, error) {
	rows, err := s.pool.Query(ctx, searchSQL, query.ProjectID, "%"+query.Query+"%", query.AfterID, query.Limit+1)
	if err != nil {
		return SearchPage{}, fmt.Errorf("searching board: %w", err)
	}
	defer rows.Close()
	results := make([]SearchResult, 0, query.Limit)
	for rows.Next() {
		var result SearchResult
		if err := rows.Scan(&result.Kind, &result.ID, &result.PostID, &result.ProjectID, &result.Title, &result.AuthorIdentity, &result.Snippet, &result.Rank, &result.CreatedAt); err != nil {
			return SearchPage{}, fmt.Errorf("scanning board search result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return SearchPage{}, fmt.Errorf("scanning board search results: %w", err)
	}
	return searchPage(results, query.Limit), nil
}

func (s *Store) CreateComment(ctx context.Context, comment *Comment) (*Comment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning board comment creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var postStatus string
	if err := tx.QueryRow(ctx, lockPostForCommentSQL, comment.PostID).Scan(&postStatus); errors.Is(err, pgx.ErrNoRows) || postStatus != PostStatusActive {
		return nil, postNotFound()
	} else if err != nil {
		return nil, fmt.Errorf("locking board post for comment creation: %w", err)
	}
	created, err := scanComment(tx.QueryRow(ctx, createCommentSQL,
		comment.PostID, comment.ParentCommentID, comment.AuthorIdentity, comment.BodyMarkdown, jsonOrNil(comment.MetadataJSON), comment.CreatedAt, comment.UpdatedAt,
	))
	if err != nil {
		return nil, fmt.Errorf("creating board comment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing board comment creation: %w", err)
	}
	return created, nil
}

func (s *Store) GetComment(ctx context.Context, id int64) (*Comment, error) {
	comment, err := scanComment(s.pool.QueryRow(ctx, getCommentSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting board comment: %w", err)
	}
	return comment, nil
}

func (s *Store) ListComments(ctx context.Context, query ListCommentsQuery) (CommentPage, error) {
	rows, err := s.pool.Query(ctx, listCommentsSQL, query.PostID, query.ParentCommentID, query.AfterID, query.Limit+1)
	if err != nil {
		return CommentPage{}, fmt.Errorf("listing board comments: %w", err)
	}
	defer rows.Close()
	comments := make([]Comment, 0, query.Limit)
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return CommentPage{}, fmt.Errorf("scanning board comment: %w", err)
		}
		comments = append(comments, *comment)
	}
	if err := rows.Err(); err != nil {
		return CommentPage{}, fmt.Errorf("scanning board comments: %w", err)
	}
	return commentPage(comments, query.Limit), nil
}

func (s *Store) GetCommentPath(ctx context.Context, id int64, limit int) (CommentPath, error) {
	target, err := s.GetComment(ctx, id)
	if err != nil || target == nil {
		return CommentPath{}, err
	}
	post, err := s.GetPost(ctx, target.PostID)
	if err != nil || post == nil {
		return CommentPath{}, err
	}
	rows, err := s.pool.Query(ctx, commentPathSQL, id, target.PostID, limit)
	if err != nil {
		return CommentPath{}, fmt.Errorf("getting board comment path: %w", err)
	}
	defer rows.Close()
	comments := make([]Comment, 0, limit)
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return CommentPath{}, fmt.Errorf("scanning board comment path: %w", err)
		}
		comments = append(comments, *comment)
	}
	if err := rows.Err(); err != nil {
		return CommentPath{}, fmt.Errorf("scanning board comment path: %w", err)
	}
	truncated := len(comments) > 0 && comments[0].ParentCommentID != nil
	return CommentPath{Post: post, Comments: comments, Truncated: truncated}, nil
}

func (s *Store) PurgePost(ctx context.Context, id int64, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning board post purge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, purgePostSQL, id, now)
	if err != nil {
		return fmt.Errorf("purging board post: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return postNotFound()
	}
	if _, err := tx.Exec(ctx, purgePostCommentsSQL, id, now); err != nil {
		return fmt.Errorf("purging board post comments: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing board post purge: %w", err)
	}
	return nil
}

func (s *Store) PurgeComment(ctx context.Context, id int64, now time.Time) error {
	tag, err := s.pool.Exec(ctx, purgeCommentSQL, id, now)
	if err != nil {
		return fmt.Errorf("purging board comment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return commentNotFound()
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPost(row rowScanner) (*Post, error) {
	var post Post
	if err := row.Scan(&post.ID, &post.ProjectID, &post.Title, &post.BodyMarkdown, &post.AuthorIdentity, &post.MetadataJSON, &post.Status, &post.CreatedAt, &post.UpdatedAt, &post.DeletedAt); err != nil {
		return nil, err
	}
	return &post, nil
}

func scanComment(row rowScanner) (*Comment, error) {
	var comment Comment
	if err := row.Scan(&comment.ID, &comment.PostID, &comment.ParentCommentID, &comment.AuthorIdentity, &comment.BodyMarkdown, &comment.MetadataJSON, &comment.Status, &comment.CreatedAt, &comment.UpdatedAt, &comment.DeletedAt); err != nil {
		return nil, err
	}
	return &comment, nil
}

func jsonOrNil(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func postPage(posts []PostSummary, limit int) PostPage {
	if len(posts) <= limit {
		return PostPage{Posts: posts}
	}
	next := posts[limit-1].ID
	return PostPage{Posts: posts[:limit], NextAfterID: &next}
}

func commentPage(comments []Comment, limit int) CommentPage {
	if len(comments) <= limit {
		return CommentPage{Comments: comments}
	}
	next := comments[limit-1].ID
	return CommentPage{Comments: comments[:limit], NextAfterID: &next}
}

func searchPage(results []SearchResult, limit int) SearchPage {
	if len(results) <= limit {
		return SearchPage{Results: results}
	}
	next := results[limit-1].ID
	return SearchPage{Results: results[:limit], NextAfterID: &next}
}

const (
	postColumns    = `id, project_id, coalesce(title, ''), coalesce(body_markdown, ''), coalesce(author_identity, ''), metadata_json, status, created_at, updated_at, deleted_at`
	commentColumns = `id, post_id, parent_comment_id, coalesce(author_identity, ''), coalesce(body_markdown, ''), metadata_json, status, created_at, updated_at, deleted_at`
)

const createPostSQL = `
insert into den_board.posts(project_id, title, body_markdown, author_identity, metadata_json, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7)
returning ` + postColumns

const getPostSQL = `select ` + postColumns + ` from den_board.posts where id = $1`

const listPostsSQL = `
select id, project_id, title, author_identity, created_at, updated_at
from den_board.posts
where project_id = $1 and status = 'active' and id > $2
order by id asc
limit $3`

const createCommentSQL = `
insert into den_board.comments(post_id, parent_comment_id, author_identity, body_markdown, metadata_json, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7)
returning ` + commentColumns

const lockPostForCommentSQL = `select status from den_board.posts where id = $1 for update`

const getCommentSQL = `select ` + commentColumns + ` from den_board.comments where id = $1`

const listCommentsSQL = `
select ` + commentColumns + `
from den_board.comments c
where c.post_id = $1
  and c.parent_comment_id is not distinct from $2::bigint
  and c.id > $3
  and (
    c.status = 'active'
    or exists (select 1 from den_board.comments child where child.parent_comment_id = c.id)
  )
order by c.id asc
limit $4`

const commentPathSQL = `
with recursive path as (
  select c.*, 1 as depth
  from den_board.comments c
  where c.id = $1 and c.post_id = $2
  union all
  select parent.*, path.depth + 1
  from den_board.comments parent
  join path on path.parent_comment_id = parent.id
  where parent.post_id = $2 and path.depth < $3
)
select ` + commentColumns + ` from path order by depth desc`

const searchSQL = `
select kind, id, post_id, project_id, title, author_identity, snippet, rank, created_at
from (
  select 'post'::text as kind, p.id, p.id as post_id, p.project_id, p.title,
         p.author_identity, left(p.body_markdown, 240) as snippet,
         case when p.title ilike $2 then 2.0 else 1.0 end::double precision as rank, p.created_at
  from den_board.posts p
  where p.project_id = $1 and p.status = 'active'
    and (p.title ilike $2 or p.body_markdown ilike $2)
  union all
  select 'comment'::text, c.id, c.post_id, p.project_id, p.title,
         c.author_identity, left(c.body_markdown, 240), 1.0::double precision, c.created_at
  from den_board.comments c
  join den_board.posts p on p.id = c.post_id
  where p.project_id = $1 and p.status = 'active' and c.status = 'active'
    and c.body_markdown ilike $2
) results
where id > $3
order by id asc
limit $4`

const purgePostSQL = `
update den_board.posts
set title = null, body_markdown = null, author_identity = null, metadata_json = null,
    status = 'deleted', deleted_at = $2, updated_at = greatest(updated_at, $2)
where id = $1 and status = 'active'`

const purgePostCommentsSQL = `
update den_board.comments
set body_markdown = null, author_identity = null, metadata_json = null,
    status = 'deleted', deleted_at = $2, updated_at = greatest(updated_at, $2)
where post_id = $1 and status = 'active'`

const purgeCommentSQL = `
update den_board.comments
set body_markdown = null, author_identity = null, metadata_json = null,
    status = 'deleted', deleted_at = $2, updated_at = greatest(updated_at, $2)
where id = $1 and status = 'active'`
