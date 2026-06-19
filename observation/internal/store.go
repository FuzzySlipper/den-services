package observation

import (
	"context"
	"encoding/json"
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

func (s *Store) AppendActivityEvent(ctx context.Context, event *ActivityEvent) (*ActivityEvent, error) {
	agentIdentityJSON, err := marshalOptionalIdentity(event.AgentIdentity())
	if err != nil {
		return nil, err
	}
	var runtimeInstanceID *string
	if event.RuntimeInstanceID() != nil {
		value := event.RuntimeInstanceID().String()
		runtimeInstanceID = &value
	}
	inserted, err := scanActivityEvent(s.pool.QueryRow(ctx, appendActivityEventSQL,
		event.SourceDomain(),
		event.EventType(),
		agentIdentityJSON,
		runtimeInstanceID,
		event.Payload(),
		event.CreatedAt(),
	))
	if err != nil {
		return nil, fmt.Errorf("appending activity event: %w", err)
	}
	return inserted, nil
}

func (s *Store) ListActivityEvents(ctx context.Context, limit int) ([]LaneEvent, error) {
	rows, err := s.pool.Query(ctx, listActivityEventsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("listing observation activity events: %w", err)
	}
	defer rows.Close()
	return scanLaneEvents(rows)
}

func (s *Store) ListDeliveryEvents(ctx context.Context, limit int) ([]LaneEvent, error) {
	rows, err := s.pool.Query(ctx, listDeliveryEventsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("listing delivery lane events: %w", err)
	}
	defer rows.Close()
	return scanLaneEvents(rows)
}

func (s *Store) ListRuntimeEvents(ctx context.Context, limit int) ([]LaneEvent, error) {
	rows, err := s.pool.Query(ctx, listRuntimeEventsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("listing runtime lane events: %w", err)
	}
	defer rows.Close()
	return scanLaneEvents(rows)
}

func (s *Store) ListChatEvents(ctx context.Context, limit int) ([]LaneEvent, error) {
	rows, err := s.pool.Query(ctx, listChatEventsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("listing conversation lane events: %w", err)
	}
	defer rows.Close()
	return scanLaneEvents(rows)
}

func (s *Store) ListActiveWork(ctx context.Context) ([]ActiveWorkItem, error) {
	rows, err := s.pool.Query(ctx, listActiveWorkSQL)
	if err != nil {
		return nil, fmt.Errorf("listing active work: %w", err)
	}
	defer rows.Close()
	return scanActiveWorkItems(rows)
}

func (s *Store) ListRuntimeProjections(ctx context.Context, agentID string) ([]RuntimeProjection, error) {
	rows, err := s.pool.Query(ctx, listRuntimeProjectionsSQL, agentID)
	if err != nil {
		return nil, fmt.Errorf("listing runtime projections for %s: %w", agentID, err)
	}
	defer rows.Close()

	var projections []RuntimeProjection
	for rows.Next() {
		projection, err := scanRuntimeProjection(rows)
		if err != nil {
			return nil, err
		}
		projections = append(projections, projection)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading runtime projections: %w", err)
	}
	return projections, nil
}

func (s *Store) ListActiveWorkForAgent(ctx context.Context, agentID string) ([]ActiveWorkItem, error) {
	rows, err := s.pool.Query(ctx, listActiveWorkForAgentSQL, agentID)
	if err != nil {
		return nil, fmt.Errorf("listing active work for %s: %w", agentID, err)
	}
	defer rows.Close()
	return scanActiveWorkItems(rows)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanActivityEvent(row rowScanner) (*ActivityEvent, error) {
	var eventID int64
	var sourceDomain SourceDomain
	var eventType string
	var agentIdentityJSON []byte
	var runtimeInstanceID *string
	var payload json.RawMessage
	var displayOnly bool
	var createdAt time.Time
	if err := row.Scan(&eventID, &sourceDomain, &eventType, &agentIdentityJSON, &runtimeInstanceID, &payload, &displayOnly, &createdAt); err != nil {
		return nil, err
	}
	agentIdentity, err := unmarshalOptionalIdentity(agentIdentityJSON)
	if err != nil {
		return nil, err
	}
	var instanceID *identity.AgentInstanceID
	if runtimeInstanceID != nil {
		value := identity.AgentInstanceID(*runtimeInstanceID)
		instanceID = &value
	}
	return rehydrateActivityEvent(eventID, sourceDomain, eventType, agentIdentity, instanceID, payload, displayOnly, createdAt)
}

func scanLaneEvents(rows pgx.Rows) ([]LaneEvent, error) {
	var events []LaneEvent
	for rows.Next() {
		event, err := scanLaneEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading lane events: %w", err)
	}
	return events, nil
}

func scanLaneEvent(row rowScanner) (LaneEvent, error) {
	var eventID string
	var sourceDomain SourceDomain
	var eventType string
	var agentIdentityJSON []byte
	var runtimeInstanceID *string
	var payload json.RawMessage
	var displayOnly bool
	var createdAt time.Time
	if err := row.Scan(&eventID, &sourceDomain, &eventType, &agentIdentityJSON, &runtimeInstanceID, &payload, &displayOnly, &createdAt); err != nil {
		return LaneEvent{}, err
	}
	agentIdentity, err := unmarshalOptionalIdentity(agentIdentityJSON)
	if err != nil {
		return LaneEvent{}, err
	}
	var instanceID *identity.AgentInstanceID
	if runtimeInstanceID != nil {
		value := identity.AgentInstanceID(*runtimeInstanceID)
		instanceID = &value
	}
	return LaneEvent{
		EventID:           eventID,
		SourceDomain:      sourceDomain,
		EventType:         eventType,
		AgentIdentity:     agentIdentity,
		RuntimeInstanceID: instanceID,
		Payload:           normalizePayload(payload),
		DisplayOnly:       displayOnly,
		CreatedAt:         createdAt.UTC(),
	}, nil
}

func scanActiveWorkItems(rows pgx.Rows) ([]ActiveWorkItem, error) {
	var items []ActiveWorkItem
	for rows.Next() {
		item, err := scanActiveWorkItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading active work: %w", err)
	}
	return items, nil
}

func scanActiveWorkItem(row rowScanner) (ActiveWorkItem, error) {
	var intentID int64
	var targetJSON []byte
	var state string
	var claimedByJSON []byte
	var runtimeInstanceID *string
	var runtimeState *string
	var sourceRef *string
	var channelMessageID *int64
	var createdAt time.Time
	if err := row.Scan(&intentID, &targetJSON, &state, &claimedByJSON, &runtimeInstanceID, &runtimeState, &sourceRef, &channelMessageID, &createdAt); err != nil {
		return ActiveWorkItem{}, err
	}
	var target identity.AgentIdentity
	if err := json.Unmarshal(targetJSON, &target); err != nil {
		return ActiveWorkItem{}, fmt.Errorf("decoding target identity: %w", err)
	}
	claimedBy, err := unmarshalOptionalIdentity(claimedByJSON)
	if err != nil {
		return ActiveWorkItem{}, err
	}
	var instanceID *identity.AgentInstanceID
	if runtimeInstanceID != nil {
		value := identity.AgentInstanceID(*runtimeInstanceID)
		instanceID = &value
	}
	return ActiveWorkItem{
		IntentID:          intentID,
		TargetIdentity:    target,
		State:             state,
		ClaimedBy:         claimedBy,
		RuntimeInstanceID: instanceID,
		RuntimeState:      runtimeState,
		SourceRef:         sourceRef,
		ChannelMessageID:  channelMessageID,
		CreatedAt:         createdAt.UTC(),
	}, nil
}

func scanRuntimeProjection(row rowScanner) (RuntimeProjection, error) {
	var instanceID string
	var profileIdentity string
	var host string
	var state string
	var lastHeartbeatAt *time.Time
	var startedAt time.Time
	var displayOnly bool
	if err := row.Scan(&instanceID, &profileIdentity, &host, &state, &lastHeartbeatAt, &startedAt, &displayOnly); err != nil {
		return RuntimeProjection{}, err
	}
	return RuntimeProjection{
		RuntimeInstanceID: identity.AgentInstanceID(instanceID),
		ProfileIdentity:   identity.ProfileIdentity(profileIdentity),
		Host:              host,
		State:             state,
		LastHeartbeatAt:   lastHeartbeatAt,
		StartedAt:         startedAt.UTC(),
		DisplayOnly:       displayOnly,
	}, nil
}

func marshalOptionalIdentity(agentIdentity *identity.AgentIdentity) ([]byte, error) {
	if agentIdentity == nil {
		return nil, nil
	}
	data, err := json.Marshal(agentIdentity)
	if err != nil {
		return nil, fmt.Errorf("encoding agent identity: %w", err)
	}
	return data, nil
}

func unmarshalOptionalIdentity(data []byte) (*identity.AgentIdentity, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var decoded identity.AgentIdentity
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decoding agent identity: %w", err)
	}
	return &decoded, nil
}

const activityEventColumns = `
event_id, source_domain, event_type, agent_identity, runtime_instance_id, payload, display_only, created_at`

const appendActivityEventSQL = `
insert into den_observation.activity_events (
	source_domain,
	event_type,
	agent_identity,
	runtime_instance_id,
	payload,
	display_only,
	created_at
) values ($1, $2, $3, $4, $5, true, $6)
returning ` + activityEventColumns

const listActivityEventsSQL = `
select 'observation:' || event_id::text as event_id,
	source_domain,
	event_type,
	agent_identity,
	runtime_instance_id,
	payload,
	display_only,
	created_at
from den_observation.activity_events
order by created_at desc
limit $1`

const listDeliveryEventsSQL = `
select 'delivery:' || intent_id::text as event_id,
	'delivery' as source_domain,
	'intent_' || state as event_type,
	target_identity as agent_identity,
	claimed_instance_id as runtime_instance_id,
	jsonb_build_object(
		'intent_id', intent_id,
		'state', state,
		'source_ref', source_ref,
		'channel_message_id', channel_message_id
	) as payload,
	false as display_only,
	created_at
from den_delivery.active_intents
order by created_at desc
limit $1`

const listRuntimeEventsSQL = `
select 'runtime:' || runtime_instance_id as event_id,
	'runtime' as source_domain,
	'runtime_' || state as event_type,
	jsonb_build_object('profile', profile_identity, 'instance_id', runtime_instance_id) as agent_identity,
	runtime_instance_id,
	jsonb_build_object(
		'host', host,
		'state', state,
		'last_heartbeat_at', last_heartbeat_at
	) as payload,
	display_only,
	coalesce(last_heartbeat_at, started_at) as created_at
from den_runtime.instance_states
order by coalesce(last_heartbeat_at, started_at) desc
limit $1`

const listChatEventsSQL = `
select 'conversation:' || message_id::text as event_id,
	'conversation' as source_domain,
	'message' as event_type,
	author_identity as agent_identity,
	null::text as runtime_instance_id,
	jsonb_build_object(
		'channel_id', channel_id,
		'body', body
	) as payload,
	true as display_only,
	created_at
from den_channels.chat_history
order by created_at desc
limit $1`

const listActiveWorkBaseSQL = `
select work.intent_id,
	work.target_identity,
	work.state,
	work.claimed_by,
	work.claimed_instance_id,
	runtime.state,
	work.source_ref,
	work.channel_message_id,
	work.created_at
from den_delivery.active_intents work
left join den_runtime.instance_states runtime on runtime.runtime_instance_id = work.claimed_instance_id`

const listActiveWorkSQL = listActiveWorkBaseSQL + `
order by work.created_at desc`

const listActiveWorkForAgentSQL = listActiveWorkBaseSQL + `
where work.target_profile = $1
	or work.claimed_profile = $1
order by work.created_at desc`

const listRuntimeProjectionsSQL = `
select runtime_instance_id,
	profile_identity,
	host,
	state,
	last_heartbeat_at,
	started_at,
	display_only
from den_runtime.instance_states
where profile_identity = $1
	or runtime_instance_id = $1
order by started_at desc`
