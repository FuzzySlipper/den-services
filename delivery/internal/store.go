package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"den-services/shared/identity"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Create(ctx context.Context, intent *DeliveryIntent) (*DeliveryIntent, error) {
	targetJSON, err := json.Marshal(intent.TargetIdentity())
	if err != nil {
		return nil, fmt.Errorf("encoding target identity: %w", err)
	}
	created, err := scanIntent(s.pool.QueryRow(ctx, createIntentSQL,
		targetJSON,
		intent.State(),
		intent.IdempotencyKey(),
		intent.CreatedAt(),
		intent.ExpiresAt(),
		intent.SourceRef(),
		intent.ChannelMessageID(),
	))
	if err != nil {
		return nil, fmt.Errorf("creating delivery intent: %w", err)
	}
	return created, nil
}

func (s *Store) GetByID(ctx context.Context, id int64) (*DeliveryIntent, error) {
	intent, err := scanIntent(s.pool.QueryRow(ctx, getIntentSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("getting delivery intent %d: %w", id, err)
	}
	return intent, nil
}

func (s *Store) List(ctx context.Context, state *IntentState) ([]*DeliveryIntent, error) {
	var rows pgx.Rows
	var err error
	if state == nil {
		rows, err = s.pool.Query(ctx, listIntentsSQL)
	} else {
		rows, err = s.pool.Query(ctx, listIntentsByStateSQL, *state)
	}
	if err != nil {
		return nil, fmt.Errorf("listing delivery intents: %w", err)
	}
	defer rows.Close()

	var intents []*DeliveryIntent
	for rows.Next() {
		intent, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading delivery intents: %w", err)
	}
	return intents, nil
}

func (s *Store) ClaimPending(ctx context.Context, id int64, token string, claimedBy identity.AgentIdentity, at time.Time) (*DeliveryIntent, error) {
	claimedByJSON, err := json.Marshal(claimedBy)
	if err != nil {
		return nil, fmt.Errorf("encoding claimed_by: %w", err)
	}
	intent, err := scanIntent(s.pool.QueryRow(ctx, claimPendingSQL, id, token, claimedByJSON, at.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, s.claimConflict(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("claiming delivery intent %d: %w", id, err)
	}
	return intent, nil
}

func (s *Store) ExpireIfPending(ctx context.Context, id int64, at time.Time) error {
	if _, err := s.pool.Exec(ctx, expireIfPendingSQL, id, at.UTC()); err != nil {
		return fmt.Errorf("expiring pending delivery intent %d: %w", id, err)
	}
	return nil
}

func (s *Store) ReportEvent(ctx context.Context, id int64, claimToken string, eventType string, payload []byte, at time.Time) (*DeliveryIntent, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning lifecycle event: %w", err)
	}
	defer tx.Rollback(ctx)

	current, err := scanIntent(tx.QueryRow(ctx, getIntentForUpdateSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("loading delivery intent %d for lifecycle: %w", id, err)
	}
	if err := current.canReport(eventType, claimToken); err != nil {
		return nil, conflict(err)
	}

	nextState := IntentStateRunning
	var completedAt *time.Time
	if eventType == "completed" {
		nextState = IntentStateCompleted
		completed := at.UTC()
		completedAt = &completed
	}
	if eventType == "failed" {
		nextState = IntentStateFailed
		completed := at.UTC()
		completedAt = &completed
	}

	intent, err := scanIntent(tx.QueryRow(ctx, updateLifecycleSQL, id, nextState, completedAt))
	if err != nil {
		return nil, fmt.Errorf("updating delivery intent %d lifecycle: %w", id, err)
	}
	if _, err := tx.Exec(ctx, insertEventSQL, id, eventType, payload, at.UTC()); err != nil {
		return nil, fmt.Errorf("recording delivery event %d: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing delivery lifecycle event: %w", err)
	}
	return intent, nil
}

func (s *Store) ExpirePendingBefore(ctx context.Context, before time.Time, at time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, expirePendingBeforeSQL, before.UTC(), at.UTC())
	if err != nil {
		return 0, fmt.Errorf("expiring pending delivery intents: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) ListRunningBefore(ctx context.Context, before time.Time) ([]*DeliveryIntent, error) {
	rows, err := s.pool.Query(ctx, listRunningBeforeSQL, before.UTC())
	if err != nil {
		return nil, fmt.Errorf("listing running delivery intents: %w", err)
	}
	defer rows.Close()

	var intents []*DeliveryIntent
	for rows.Next() {
		intent, err := scanIntent(rows)
		if err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading running delivery intents: %w", err)
	}
	return intents, nil
}

func (s *Store) FailRunning(ctx context.Context, id int64, at time.Time) error {
	if _, err := s.pool.Exec(ctx, failRunningSQL, id, at.UTC()); err != nil {
		return fmt.Errorf("failing running delivery intent %d: %w", id, err)
	}
	return nil
}

func (s *Store) claimConflict(ctx context.Context, id int64) error {
	intent, err := s.GetByID(ctx, id)
	if err != nil {
		return err
	}
	switch intent.State() {
	case IntentStateCompleted, IntentStateFailed, IntentStateCancelled, IntentStateDisplayOnly:
		return conflict(ErrIntentAlreadyCompleted)
	case IntentStateExpired:
		return conflict(ErrIntentExpired)
	default:
		return conflict(ErrIntentAlreadyClaimed)
	}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanIntent(row rowScanner) (*DeliveryIntent, error) {
	var id int64
	var targetJSON []byte
	var state IntentState
	var idempotencyKey string
	var createdAt time.Time
	var expiresAt time.Time
	var claimedAt *time.Time
	var claimToken *string
	var claimedByJSON []byte
	var completedAt *time.Time
	var sourceRef *string
	var channelMessageID *int64
	var cutoverWatermark *string
	if err := row.Scan(&id, &targetJSON, &state, &idempotencyKey, &createdAt, &expiresAt, &claimedAt, &claimToken, &claimedByJSON, &completedAt, &sourceRef, &channelMessageID, &cutoverWatermark); err != nil {
		return nil, err
	}
	var target identity.AgentIdentity
	if err := json.Unmarshal(targetJSON, &target); err != nil {
		return nil, fmt.Errorf("decoding target identity: %w", err)
	}
	var claimedBy *identity.AgentIdentity
	if len(claimedByJSON) > 0 {
		var decoded identity.AgentIdentity
		if err := json.Unmarshal(claimedByJSON, &decoded); err != nil {
			return nil, fmt.Errorf("decoding claimed_by: %w", err)
		}
		claimedBy = &decoded
	}
	return rehydrateDeliveryIntent(id, target, state, idempotencyKey, createdAt, expiresAt, claimedAt, claimToken, claimedBy, completedAt, sourceRef, channelMessageID, cutoverWatermark)
}

const intentColumns = `
id, target_identity, state, idempotency_key, created_at, expires_at,
claimed_at, claim_token, claimed_by, completed_at, source_ref, channel_message_id, cutover_watermark`

const createIntentSQL = `
insert into den_delivery.delivery_intents (target_identity, state, idempotency_key, created_at, expires_at, source_ref, channel_message_id)
values ($1, $2, $3, $4, $5, $6, $7)
on conflict (idempotency_key) do update
set idempotency_key = excluded.idempotency_key
returning ` + intentColumns

const getIntentSQL = `
select ` + intentColumns + `
from den_delivery.delivery_intents
where id = $1`

const getIntentForUpdateSQL = `
select ` + intentColumns + `
from den_delivery.delivery_intents
where id = $1
for update`

const listIntentsSQL = `
select ` + intentColumns + `
from den_delivery.delivery_intents
order by created_at desc`

const listIntentsByStateSQL = `
select ` + intentColumns + `
from den_delivery.delivery_intents
where state = $1
order by created_at desc`

const claimPendingSQL = `
update den_delivery.delivery_intents
set state = 'claimed',
	claim_token = $2,
	claimed_by = $3,
	claimed_at = $4
where id = $1
	and state = 'pending'
returning ` + intentColumns

const expireIfPendingSQL = `
update den_delivery.delivery_intents
set state = 'expired',
	completed_at = $2
where id = $1
	and state = 'pending'`

const updateLifecycleSQL = `
update den_delivery.delivery_intents
set state = $2,
	completed_at = $3
where id = $1
returning ` + intentColumns

const insertEventSQL = `
insert into den_delivery.delivery_events (intent_id, event_type, payload, created_at)
values ($1, $2, $3, $4)`

const expirePendingBeforeSQL = `
update den_delivery.delivery_intents
set state = 'expired',
	completed_at = $2
where state = 'pending'
	and created_at <= $1`

const listRunningBeforeSQL = `
select ` + intentColumns + `
from den_delivery.delivery_intents
where state = 'running'
	and claimed_at is not null
	and claimed_at <= $1`

const failRunningSQL = `
update den_delivery.delivery_intents
set state = 'failed',
	completed_at = $2
where id = $1
	and state = 'running'`
