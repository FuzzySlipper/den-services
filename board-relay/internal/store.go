package boardrelay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging board relay store: %w", err)
	}
	return nil
}

func (s *Store) FindByBoard(ctx context.Context, projectID string, boardKind string, boardID int64) (*Mapping, error) {
	return s.find(ctx, findByBoardSQL, projectID, boardKind, boardID)
}

func (s *Store) FindByGitHub(ctx context.Context, projectID string, githubKind string, githubID int64) (*Mapping, error) {
	return s.find(ctx, findByGitHubSQL, projectID, githubKind, githubID)
}

func (s *Store) find(ctx context.Context, query string, arguments ...any) (*Mapping, error) {
	mapping, err := scanMapping(s.pool.QueryRow(ctx, query, arguments...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading board relay mapping: %w", err)
	}
	return mapping, nil
}

func (s *Store) Save(ctx context.Context, mapping Mapping) error {
	_, err := s.pool.Exec(ctx, saveMappingSQL,
		mapping.ProjectID, mapping.BoardKind, mapping.BoardID, mapping.GitHubKind, mapping.GitHubID,
		mapping.IssueNumber, mapping.GitHubURL, mapping.Origin, mapping.GitHubUpdatedAt, mapping.CreatedAt, mapping.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("saving board relay mapping: %w", err)
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanMapping(row rowScanner) (*Mapping, error) {
	var mapping Mapping
	if err := row.Scan(
		&mapping.ProjectID, &mapping.BoardKind, &mapping.BoardID, &mapping.GitHubKind, &mapping.GitHubID,
		&mapping.IssueNumber, &mapping.GitHubURL, &mapping.Origin, &mapping.GitHubUpdatedAt, &mapping.CreatedAt, &mapping.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &mapping, nil
}

const mappingColumns = `
project_id, board_kind, board_id, github_kind, github_id,
issue_number, github_url, origin, github_updated_at, created_at, updated_at`

const findByBoardSQL = `
select ` + mappingColumns + `
from den_board_relay.mappings
where project_id = $1 and board_kind = $2 and board_id = $3`

const findByGitHubSQL = `
select ` + mappingColumns + `
from den_board_relay.mappings
where project_id = $1 and github_kind = $2 and github_id = $3`

const saveMappingSQL = `
insert into den_board_relay.mappings (
    project_id, board_kind, board_id, github_kind, github_id,
    issue_number, github_url, origin, github_updated_at, created_at, updated_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
on conflict (project_id, github_kind, github_id) do update set
    board_kind = excluded.board_kind,
    board_id = excluded.board_id,
    issue_number = excluded.issue_number,
    github_url = excluded.github_url,
    origin = excluded.origin,
    github_updated_at = excluded.github_updated_at,
    updated_at = excluded.updated_at`

func newMapping(projectID string, boardKind string, boardID int64, githubKind string, githubID int64, issueNumber int64, githubURL string, origin string, githubUpdatedAt time.Time, now time.Time) Mapping {
	return Mapping{
		ProjectID: projectID, BoardKind: boardKind, BoardID: boardID, GitHubKind: githubKind, GitHubID: githubID,
		IssueNumber: issueNumber, GitHubURL: githubURL, Origin: origin, GitHubUpdatedAt: githubUpdatedAt.UTC(), CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
}
