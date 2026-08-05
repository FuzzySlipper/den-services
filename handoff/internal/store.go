package handoff

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging handoff store: %w", err)
	}
	return nil
}

func (s *Store) Set(ctx context.Context, value *Handoff) (*Handoff, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning handoff replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanHandoff(tx.QueryRow(ctx, upsertHandoffSQL,
		value.Label(), value.BodyMarkdown(), emptyToNil(value.UpdatedBy()), value.UpdatedAt(),
	))
	if err != nil {
		return nil, fmt.Errorf("upserting current handoff: %w", err)
	}
	if _, err := tx.Exec(ctx, insertRevisionSQL,
		current.Label(), current.Revision(), current.BodyMarkdown(), emptyToNil(current.UpdatedBy()), current.UpdatedAt(),
	); err != nil {
		return nil, fmt.Errorf("recording handoff revision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing handoff replacement: %w", err)
	}
	return current, nil
}

func (s *Store) Get(ctx context.Context, label string) (*Handoff, error) {
	value, err := scanHandoff(s.pool.QueryRow(ctx, getHandoffSQL, label))
	if err != nil {
		return nil, err
	}
	return value, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanHandoff(row rowScanner) (*Handoff, error) {
	var params NewHandoffParams
	var updatedBy *string
	if err := row.Scan(&params.Label, &params.BodyMarkdown, &params.Revision, &params.CreatedAt, &params.UpdatedAt, &updatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrHandoffNotFound
		}
		return nil, fmt.Errorf("scanning handoff: %w", err)
	}
	if updatedBy != nil {
		params.UpdatedBy = *updatedBy
	}
	return NewHandoff(params)
}

func emptyToNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

const handoffColumns = `label, body_markdown, revision, created_at, updated_at, updated_by`

const upsertHandoffSQL = `
insert into den_handoff.handoffs(label, body_markdown, revision, created_at, updated_at, updated_by)
values ($1, $2, 1, $4, $4, $3)
on conflict (label) do update set
    body_markdown = excluded.body_markdown,
    revision = den_handoff.handoffs.revision + 1,
    updated_at = greatest(den_handoff.handoffs.updated_at, excluded.updated_at),
    updated_by = excluded.updated_by
returning ` + handoffColumns

const insertRevisionSQL = `
insert into den_handoff.handoff_revisions(label, revision, body_markdown, updated_by, submitted_at)
values ($1, $2, $3, $4, $5)`

const getHandoffSQL = `select ` + handoffColumns + ` from den_handoff.handoffs where label = $1`
