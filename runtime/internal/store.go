package runtime

import (
	"context"
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

func (s *Store) RegisterInstance(ctx context.Context, instance *RuntimeInstance) (*RuntimeInstance, error) {
	row := s.pool.QueryRow(ctx, registerInstanceSQL,
		instance.InstanceID().String(),
		instance.ProfileIdentity().String(),
		instance.Host(),
		instance.PID(),
		instance.State(),
		instance.StartedAt(),
	)
	registered, err := scanRuntimeInstance(row)
	if err != nil {
		return nil, fmt.Errorf("registering runtime instance %s: %w", instance.InstanceID(), err)
	}
	return registered, nil
}

func (s *Store) ListInstances(ctx context.Context, state *RuntimeState) ([]*RuntimeInstance, error) {
	var rows pgx.Rows
	var err error
	if state == nil {
		rows, err = s.pool.Query(ctx, listInstancesSQL)
	} else {
		rows, err = s.pool.Query(ctx, listInstancesByStateSQL, *state)
	}
	if err != nil {
		return nil, fmt.Errorf("listing runtime instances: %w", err)
	}
	defer rows.Close()

	var instances []*RuntimeInstance
	for rows.Next() {
		instance, err := scanRuntimeInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading runtime instances: %w", err)
	}
	return instances, nil
}

func (s *Store) GetInstance(ctx context.Context, instanceID identity.AgentInstanceID) (*RuntimeInstance, error) {
	instance, err := scanRuntimeInstance(s.pool.QueryRow(ctx, getInstanceSQL, instanceID.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(ErrInstanceNotFound, instanceID.String())
	}
	if err != nil {
		return nil, fmt.Errorf("getting runtime instance %s: %w", instanceID, err)
	}
	return instance, nil
}

func (s *Store) Heartbeat(ctx context.Context, instanceID identity.AgentInstanceID, at time.Time) (*RuntimeInstance, error) {
	instance, err := scanRuntimeInstance(s.pool.QueryRow(ctx, heartbeatSQL, instanceID.String(), at.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(ErrInstanceNotFound, instanceID.String())
	}
	if err != nil {
		return nil, fmt.Errorf("heartbeating runtime instance %s: %w", instanceID, err)
	}
	return instance, nil
}

func (s *Store) CreateSubscription(ctx context.Context, subscription *ChannelSubscription) (*ChannelSubscription, error) {
	row := s.pool.QueryRow(ctx, createSubscriptionSQL,
		subscription.RuntimeInstanceID().String(),
		subscription.ChannelID(),
		subscription.WakePolicyOverride(),
		subscription.CreatedAt(),
	)
	created, err := scanSubscription(row)
	if err != nil {
		return nil, fmt.Errorf("creating subscription: %w", err)
	}
	return created, nil
}

func (s *Store) GetSubscription(ctx context.Context, subscriptionID int64) (*ChannelSubscription, error) {
	subscription, err := scanSubscription(s.pool.QueryRow(ctx, getSubscriptionSQL, subscriptionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(ErrSubscriptionNotFound, fmt.Sprintf("%d", subscriptionID))
	}
	if err != nil {
		return nil, fmt.Errorf("getting subscription %d: %w", subscriptionID, err)
	}
	return subscription, nil
}

func (s *Store) MarkSubscriptionPolled(ctx context.Context, subscriptionID int64, cursor int64, at time.Time) (*ChannelSubscription, error) {
	subscription, err := scanSubscription(s.pool.QueryRow(ctx, markSubscriptionPolledSQL, subscriptionID, cursor, at.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(ErrSubscriptionNotFound, fmt.Sprintf("%d", subscriptionID))
	}
	if err != nil {
		return nil, fmt.Errorf("marking subscription %d polled: %w", subscriptionID, err)
	}
	return subscription, nil
}

func (s *Store) MarkStale(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, markStaleSQL, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("marking stale runtimes: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) MarkDead(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, markDeadSQL, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("marking dead runtimes: %w", err)
	}
	return tag.RowsAffected(), nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRuntimeInstance(row rowScanner) (*RuntimeInstance, error) {
	var instanceID string
	var profileIdentity string
	var host string
	var pid *int
	var state RuntimeState
	var startedAt time.Time
	var lastHeartbeatAt *time.Time
	var stoppedAt *time.Time
	var degradedReason *string
	if err := row.Scan(&instanceID, &profileIdentity, &host, &pid, &state, &startedAt, &lastHeartbeatAt, &stoppedAt, &degradedReason); err != nil {
		return nil, err
	}
	return rehydrateRuntimeInstance(
		identity.AgentInstanceID(instanceID),
		identity.ProfileIdentity(profileIdentity),
		host,
		pid,
		state,
		startedAt,
		lastHeartbeatAt,
		stoppedAt,
		degradedReason,
	)
}

func scanSubscription(row rowScanner) (*ChannelSubscription, error) {
	var subscriptionID int64
	var instanceID string
	var channelID int64
	var cursorPosition int64
	var lastPolledAt *time.Time
	var wakePolicyOverride *string
	var createdAt time.Time
	if err := row.Scan(&subscriptionID, &instanceID, &channelID, &cursorPosition, &lastPolledAt, &wakePolicyOverride, &createdAt); err != nil {
		return nil, err
	}
	return rehydrateChannelSubscription(subscriptionID, identity.AgentInstanceID(instanceID), channelID, cursorPosition, lastPolledAt, wakePolicyOverride, createdAt)
}

const runtimeInstanceColumns = `
instance_id, profile_identity, host, pid, state, started_at, last_heartbeat_at, stopped_at, degraded_reason`

const subscriptionColumns = `
subscription_id, runtime_instance_id, channel_id, cursor_position, last_polled_at, wake_policy_override, created_at`

const registerInstanceSQL = `
insert into den_runtime.runtime_instances (instance_id, profile_identity, host, pid, state, started_at)
values ($1, $2, $3, $4, $5, $6)
on conflict (instance_id) do update
set profile_identity = excluded.profile_identity,
	host = excluded.host,
	pid = excluded.pid,
	state = excluded.state,
	started_at = excluded.started_at,
	last_heartbeat_at = null,
	stopped_at = null,
	degraded_reason = null
returning ` + runtimeInstanceColumns

const listInstancesSQL = `
select ` + runtimeInstanceColumns + `
from den_runtime.runtime_instances
order by started_at desc`

const listInstancesByStateSQL = `
select ` + runtimeInstanceColumns + `
from den_runtime.runtime_instances
where state = $1
order by started_at desc`

const getInstanceSQL = `
select ` + runtimeInstanceColumns + `
from den_runtime.runtime_instances
where instance_id = $1`

const heartbeatSQL = `
update den_runtime.runtime_instances
set last_heartbeat_at = $2,
	state = case
		when state in ('starting', 'stale', 'dead') then 'active'
		else state
	end
where instance_id = $1
returning ` + runtimeInstanceColumns

const createSubscriptionSQL = `
insert into den_runtime.channel_subscriptions (runtime_instance_id, channel_id, wake_policy_override, created_at)
values ($1, $2, $3, $4)
on conflict (runtime_instance_id, channel_id) do update
set wake_policy_override = excluded.wake_policy_override
returning ` + subscriptionColumns

const getSubscriptionSQL = `
select ` + subscriptionColumns + `
from den_runtime.channel_subscriptions
where subscription_id = $1`

const markSubscriptionPolledSQL = `
update den_runtime.channel_subscriptions
set cursor_position = greatest(cursor_position, $2),
	last_polled_at = $3
where subscription_id = $1
returning ` + subscriptionColumns

const markStaleSQL = `
update den_runtime.runtime_instances
set state = 'stale'
where state in ('starting', 'active', 'idle', 'busy', 'degraded')
	and last_heartbeat_at is not null
	and last_heartbeat_at <= $1`

const markDeadSQL = `
update den_runtime.runtime_instances
set state = 'dead'
where state in ('starting', 'active', 'idle', 'busy', 'degraded', 'stale')
	and last_heartbeat_at is not null
	and last_heartbeat_at <= $1`
