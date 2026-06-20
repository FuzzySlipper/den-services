package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

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
		return fmt.Errorf("pinging timeline store: %w", err)
	}
	return nil
}

func (s *Store) ListItems(ctx context.Context, query ListItemsQuery) ([]TimelineItem, error) {
	channelID, projectID := scopeArgs(query.Scope)
	if query.After == nil {
		rows, err := s.pool.Query(ctx, listNewestItemsSQL, channelID, projectID, query.IncludeDebug, query.Limit)
		if err != nil {
			return nil, fmt.Errorf("listing newest timeline items: %w", err)
		}
		defer rows.Close()
		return scanTimelineItems(rows)
	}
	rows, err := s.pool.Query(ctx,
		listItemsAfterSQL,
		channelID,
		projectID,
		query.IncludeDebug,
		query.After.OccurredAt,
		query.After.SourceRank(),
		query.After.ID,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing timeline items after cursor: %w", err)
	}
	defer rows.Close()
	return scanTimelineItems(rows)
}

func scopeArgs(scope TimelineScope) (*int64, *string) {
	if scope.Kind == ScopeKindChannel {
		return scope.ChannelID, nil
	}
	return nil, scope.ProjectID
}

func scanTimelineItems(rows pgx.Rows) ([]TimelineItem, error) {
	var items []TimelineItem
	for rows.Next() {
		item, err := scanTimelineItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading timeline items: %w", err)
	}
	return items, nil
}

func scanTimelineItem(row rowScanner) (TimelineItem, error) {
	var item TimelineItem
	var sourceCursor CursorSource
	var metadata json.RawMessage
	var channelID *int64
	var sourceRankValue int
	if err := row.Scan(
		&item.TimelineID,
		&item.OccurredAt,
		&item.SourceDomain,
		&item.SourceID,
		&sourceCursor,
		&item.SourceNumericID,
		&item.EventKind,
		&item.RenderKind,
		&item.DisplayOnly,
		&channelID,
		&item.ProjectID,
		&item.TaskID,
		&item.Actor.Type,
		&item.Actor.Identity,
		&item.Actor.ProfileIdentity,
		&item.Actor.AgentInstanceID,
		&item.Body,
		&item.Summary,
		&item.Severity,
		&metadata,
		&item.SourceRef.Domain,
		&item.SourceRef.Table,
		&item.SourceRef.ID,
		&sourceRankValue,
	); err != nil {
		return TimelineItem{}, err
	}
	item.OccurredAt = item.OccurredAt.UTC()
	item.SourceCursor = sourceCursor
	item.ChannelID = channelID
	item.Metadata = defaultJSON(metadata)
	if item.Severity == "" {
		item.Severity = "info"
	}
	return item, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

const timelineUnionSQL = `
with timeline_items as (
	select
		'msg:' || m.id::text as timeline_id,
		m.created_at as occurred_at,
		'conversation'::text as source_domain,
		m.id::text as source_id,
		'msg'::text as source_cursor,
		m.id as source_numeric_id,
		'channel_message'::text as event_kind,
		'message'::text as render_kind,
		true as display_only,
		m.channel_id,
		coalesce(m.target_project_id, c.project_id, m.source_project_id) as project_id,
		m.target_task_id as task_id,
		m.sender_type as actor_type,
		m.sender_identity as actor_identity,
		m.profile_identity,
		m.agent_instance_id,
		m.body,
		m.summary,
		'info'::text as severity,
		m.metadata,
		'conversation'::text as source_ref_domain,
		'den_channels.channel_messages'::text as source_ref_table,
		m.id::text as source_ref_id,
		1 as source_rank
	from den_channels.channel_messages m
	join den_channels.channels c on c.id = m.channel_id
	where m.deleted_at is null
		and (
			($1::bigint is not null and m.channel_id = $1)
			or ($2::text is not null and coalesce(m.target_project_id, c.project_id, m.source_project_id) = $2)
		)
	union all
	select
		'obs:' || a.event_id::text as timeline_id,
		a.created_at as occurred_at,
		'observation'::text as source_domain,
		a.event_id::text as source_id,
		'obs'::text as source_cursor,
		a.event_id as source_numeric_id,
		a.event_type as event_kind,
		case
			when coalesce(a.payload #>> '{visibility}', '') = 'debug' then 'diagnostic'
			when coalesce(a.payload #>> '{render_kind}', '') <> '' then a.payload #>> '{render_kind}'
			when a.event_type like '%progress%' then 'progress'
			when a.event_type like '%evidence%' then 'evidence'
			else 'breadcrumb'
		end as render_kind,
		a.display_only,
		case
			when (a.payload #>> '{work_ref,channel_id}') ~ '^[0-9]+$'
				then (a.payload #>> '{work_ref,channel_id}')::bigint
			when linked.channel_id is not null then linked.channel_id
			else null
		end as channel_id,
		a.payload #>> '{work_ref,project_id}' as project_id,
		case
			when (a.payload #>> '{work_ref,task_id}') ~ '^[0-9]+$'
				then (a.payload #>> '{work_ref,task_id}')::bigint
			else null
		end as task_id,
		'agent'::text as actor_type,
		coalesce(a.agent_identity ->> 'profile', a.runtime_instance_id, a.source_domain) as actor_identity,
		a.agent_identity ->> 'profile' as profile_identity,
		coalesce(a.agent_identity ->> 'instance_id', a.runtime_instance_id) as agent_instance_id,
		null::text as body,
		a.payload #>> '{summary}' as summary,
		coalesce(nullif(a.payload #>> '{severity}', ''), 'info') as severity,
		a.payload as metadata,
		'observation'::text as source_ref_domain,
		'den_observation.activity_events'::text as source_ref_table,
		a.event_id::text as source_ref_id,
		2 as source_rank
	from den_observation.activity_events a
	left join den_channels.channel_messages linked
		on (a.payload #>> '{work_ref,channel_message_id}') ~ '^[0-9]+$'
		and linked.id = (a.payload #>> '{work_ref,channel_message_id}')::bigint
		and linked.deleted_at is null
	where ($3::boolean or coalesce(a.payload #>> '{visibility}', '') <> 'debug')
		and (
			$1::bigint is not null
			and (
				a.payload #>> '{work_ref,channel_id}' = $1::text
				or linked.channel_id = $1
			)
			or $2::text is not null
			and a.payload #>> '{work_ref,project_id}' = $2
		)
)`

const timelineSelectColumns = `
timeline_id, occurred_at, source_domain, source_id, source_cursor, source_numeric_id,
event_kind, render_kind, display_only, channel_id, project_id, task_id, actor_type,
actor_identity, profile_identity, agent_instance_id, body, summary, severity, metadata,
source_ref_domain, source_ref_table, source_ref_id, source_rank`

const listNewestItemsSQL = timelineUnionSQL + `
select ` + timelineSelectColumns + `
from (
	select ` + timelineSelectColumns + `
	from timeline_items
	order by occurred_at desc, source_rank desc, source_numeric_id desc
	limit $4
) newest
order by occurred_at asc, source_rank asc, source_numeric_id asc`

const listItemsAfterSQL = timelineUnionSQL + `
select ` + timelineSelectColumns + `
from timeline_items
where (occurred_at, source_rank, source_numeric_id) > ($4::timestamptz, $5::integer, $6::bigint)
order by occurred_at asc, source_rank asc, source_numeric_id asc
limit $7`

func sourceIDFromInt(id int64) string {
	return strconv.FormatInt(id, 10)
}
