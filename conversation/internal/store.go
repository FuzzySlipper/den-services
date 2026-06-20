package conversation

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
		return fmt.Errorf("pinging conversation store: %w", err)
	}
	return nil
}

func (s *Store) CreateChannel(ctx context.Context, channel *Channel) (*Channel, error) {
	created, err := scanChannel(s.pool.QueryRow(ctx, createChannelSQL,
		channel.Slug,
		channel.DisplayName,
		channel.Kind,
		channel.ProjectID,
		channel.SpaceID,
		channel.CreatedBy,
		channel.Visibility,
		channel.Settings,
		channel.CreatedAt,
		channel.UpdatedAt,
	))
	if err != nil {
		return nil, fmt.Errorf("creating channel: %w", err)
	}
	return created, nil
}

func (s *Store) ListChannels(ctx context.Context, query ListChannelsQuery) ([]*Channel, error) {
	rows, err := s.pool.Query(ctx, listChannelsSQL, query.ProjectID, query.Kind, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	defer rows.Close()
	return scanChannels(rows)
}

func (s *Store) GetChannel(ctx context.Context, id int64) (*Channel, error) {
	channel, err := scanChannel(s.pool.QueryRow(ctx, getChannelSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(ErrChannelNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("getting channel %d: %w", id, err)
	}
	return channel, nil
}

func (s *Store) UpsertProjectDefaultChannel(ctx context.Context, projectID string, req PutDefaultChannelRequest, at time.Time) (*Channel, error) {
	if req.ChannelID != nil {
		channel, err := scanChannel(s.pool.QueryRow(ctx, linkExistingDefaultChannelSQL, *req.ChannelID, projectID, req.CreatedBy, at))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFound(ErrChannelNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("linking default channel for project %s: %w", projectID, err)
		}
		return channel, nil
	}
	channel, err := scanChannel(s.pool.QueryRow(ctx, upsertProjectDefaultChannelSQL,
		req.Slug,
		req.DisplayName,
		projectID,
		req.CreatedBy,
		defaultJSON(req.Settings),
		at,
	))
	if err != nil {
		return nil, fmt.Errorf("upserting default channel for project %s: %w", projectID, err)
	}
	return channel, nil
}

func (s *Store) AppendMessage(ctx context.Context, message *ChannelMessage) (*ChannelMessage, error) {
	created, err := scanMessage(s.pool.QueryRow(ctx, appendMessageSQL,
		message.ChannelID,
		message.SenderType,
		message.SenderIdentity,
		message.Body,
		message.MessageKind,
		message.SourceKind,
		message.SourceID,
		message.SourceProjectID,
		message.TargetProjectID,
		message.TargetTaskID,
		message.AssignmentID,
		message.WorkerRunID,
		message.WorkerRole,
		message.ProfileIdentity,
		message.AgentInstanceID,
		message.PoolMemberID,
		message.SessionOwnerID,
		message.SessionID,
		message.Summary,
		message.DeepLink,
		message.ThreadRootMessageID,
		message.ReplyToMessageID,
		message.Metadata,
		message.DedupeKey,
		message.CreatedAt,
	))
	if err != nil {
		return nil, fmt.Errorf("appending channel message: %w", err)
	}
	return created, nil
}

func (s *Store) ListMessages(ctx context.Context, query ListMessagesQuery) ([]*ChannelMessage, error) {
	rows, err := s.pool.Query(ctx, listMessagesSQL, query.ChannelID, query.AfterID, query.AssignmentID, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("listing channel messages: %w", err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) UpsertMembership(ctx context.Context, membership *ChannelMembership) (*ChannelMembership, error) {
	upserted, err := scanMembership(s.pool.QueryRow(ctx, upsertMembershipSQL,
		membership.ChannelID,
		membership.MemberType,
		membership.MemberIdentity,
		membership.ProfileIdentity,
		membership.MembershipStatus,
		membership.WakePolicy,
		membership.CanSend,
		membership.CanReact,
		membership.CanInvite,
		membership.MembershipPurpose,
		membership.Settings,
		membership.CreatedAt,
		membership.UpdatedAt,
	))
	if err != nil {
		return nil, fmt.Errorf("upserting channel membership: %w", err)
	}
	return upserted, nil
}

func (s *Store) ListMemberships(ctx context.Context, query ListMembershipsQuery) ([]*ChannelMembership, error) {
	rows, err := s.pool.Query(ctx, listMembershipsSQL,
		query.MemberIdentity,
		query.MembershipPurpose,
		query.ProjectID,
		query.ChannelID,
		query.IncludeLeft,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing channel memberships: %w", err)
	}
	defer rows.Close()
	return scanMemberships(rows)
}

func (s *Store) AddReaction(ctx context.Context, messageID int64, req AddReactionRequest, at time.Time) (*ChannelReaction, error) {
	reaction, err := scanReaction(s.pool.QueryRow(ctx, addReactionSQL, messageID, req.ReactorType, req.ReactorIdentity, req.Reaction, at))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(ErrMessageNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("adding channel reaction: %w", err)
	}
	return reaction, nil
}

func (s *Store) ListReadCursors(ctx context.Context, channelID int64) ([]*ChannelReadCursor, error) {
	rows, err := s.pool.Query(ctx, listReadCursorsSQL, channelID)
	if err != nil {
		return nil, fmt.Errorf("listing channel read cursors: %w", err)
	}
	defer rows.Close()
	return scanReadCursors(rows)
}

func (s *Store) UpsertReadCursor(ctx context.Context, cursor *ChannelReadCursor) (*ChannelReadCursor, error) {
	upserted, err := scanReadCursor(s.pool.QueryRow(ctx, upsertReadCursorSQL,
		cursor.ChannelID,
		cursor.ReaderType,
		cursor.ReaderIdentity,
		cursor.InstanceID,
		cursor.LastReadMessageID,
		cursor.LastReadAt,
	))
	if err != nil {
		return nil, fmt.Errorf("upserting channel read cursor: %w", err)
	}
	return upserted, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanChannel(row rowScanner) (*Channel, error) {
	var channel Channel
	if err := row.Scan(
		&channel.ID,
		&channel.Slug,
		&channel.DisplayName,
		&channel.Kind,
		&channel.ProjectID,
		&channel.SpaceID,
		&channel.CreatedBy,
		&channel.Visibility,
		&channel.Settings,
		&channel.CreatedAt,
		&channel.UpdatedAt,
		&channel.ArchivedAt,
	); err != nil {
		return nil, err
	}
	return &channel, nil
}

func scanChannels(rows pgx.Rows) ([]*Channel, error) {
	var channels []*Channel
	for rows.Next() {
		channel, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading channels: %w", err)
	}
	return channels, nil
}

func scanMessage(row rowScanner) (*ChannelMessage, error) {
	var message ChannelMessage
	if err := row.Scan(
		&message.ID,
		&message.ChannelID,
		&message.SenderType,
		&message.SenderIdentity,
		&message.Body,
		&message.MessageKind,
		&message.SourceKind,
		&message.SourceID,
		&message.SourceProjectID,
		&message.TargetProjectID,
		&message.TargetTaskID,
		&message.AssignmentID,
		&message.WorkerRunID,
		&message.WorkerRole,
		&message.ProfileIdentity,
		&message.AgentInstanceID,
		&message.PoolMemberID,
		&message.SessionOwnerID,
		&message.SessionID,
		&message.Summary,
		&message.DeepLink,
		&message.ThreadRootMessageID,
		&message.ReplyToMessageID,
		&message.Metadata,
		&message.DedupeKey,
		&message.CreatedAt,
		&message.EditedAt,
		&message.DeletedAt,
	); err != nil {
		return nil, err
	}
	return &message, nil
}

func scanMessages(rows pgx.Rows) ([]*ChannelMessage, error) {
	var messages []*ChannelMessage
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading messages: %w", err)
	}
	return messages, nil
}

func scanMembership(row rowScanner) (*ChannelMembership, error) {
	var membership ChannelMembership
	if err := row.Scan(
		&membership.ID,
		&membership.ChannelID,
		&membership.MemberType,
		&membership.MemberIdentity,
		&membership.ProfileIdentity,
		&membership.MembershipStatus,
		&membership.WakePolicy,
		&membership.CanSend,
		&membership.CanReact,
		&membership.CanInvite,
		&membership.MembershipPurpose,
		&membership.Settings,
		&membership.CreatedAt,
		&membership.UpdatedAt,
		&membership.LeftAt,
	); err != nil {
		return nil, err
	}
	return &membership, nil
}

func scanMemberships(rows pgx.Rows) ([]*ChannelMembership, error) {
	var memberships []*ChannelMembership
	for rows.Next() {
		membership, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading memberships: %w", err)
	}
	return memberships, nil
}

func scanReaction(row rowScanner) (*ChannelReaction, error) {
	var reaction ChannelReaction
	if err := row.Scan(
		&reaction.ID,
		&reaction.MessageID,
		&reaction.ChannelID,
		&reaction.ReactorType,
		&reaction.ReactorIdentity,
		&reaction.Reaction,
		&reaction.CreatedAt,
		&reaction.DeletedAt,
	); err != nil {
		return nil, err
	}
	return &reaction, nil
}

func scanReadCursor(row rowScanner) (*ChannelReadCursor, error) {
	var cursor ChannelReadCursor
	if err := row.Scan(
		&cursor.ChannelID,
		&cursor.ReaderType,
		&cursor.ReaderIdentity,
		&cursor.InstanceID,
		&cursor.LastReadMessageID,
		&cursor.LastReadAt,
	); err != nil {
		return nil, err
	}
	return &cursor, nil
}

func scanReadCursors(rows pgx.Rows) ([]*ChannelReadCursor, error) {
	var cursors []*ChannelReadCursor
	for rows.Next() {
		cursor, err := scanReadCursor(rows)
		if err != nil {
			return nil, err
		}
		cursors = append(cursors, cursor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading read cursors: %w", err)
	}
	return cursors, nil
}

const channelColumns = `
id, slug, display_name, kind, project_id, space_id, created_by, visibility,
settings, created_at, updated_at, archived_at`

const messageColumns = `
id, channel_id, sender_type, sender_identity, body, message_kind, source_kind,
source_id, source_project_id, target_project_id, target_task_id, assignment_id,
worker_run_id, worker_role, profile_identity, agent_instance_id, pool_member_id,
session_owner_id, session_id, summary, deep_link, thread_root_message_id,
reply_to_message_id, metadata, dedupe_key, created_at, edited_at, deleted_at`

const membershipColumns = `
id, channel_id, member_type, member_identity, profile_identity, membership_status,
wake_policy, can_send, can_react, can_invite, membership_purpose, settings,
created_at, updated_at, left_at`

const prefixedMembershipColumns = `
m.id, m.channel_id, m.member_type, m.member_identity, m.profile_identity, m.membership_status,
m.wake_policy, m.can_send, m.can_react, m.can_invite, m.membership_purpose, m.settings,
m.created_at, m.updated_at, m.left_at`

const reactionColumns = `
id, message_id, channel_id, reactor_type, reactor_identity, reaction, created_at, deleted_at`

const readCursorColumns = `
channel_id, reader_type, reader_identity, instance_id, last_read_message_id, last_read_at`

const createChannelSQL = `
insert into den_channels.channels (
	slug, display_name, kind, project_id, space_id, created_by, visibility, settings, created_at, updated_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
returning ` + channelColumns

const listChannelsSQL = `
select ` + channelColumns + `
from den_channels.channels
where archived_at is null
	and ($1::text is null or project_id = $1)
	and ($2::text is null or kind = $2)
order by created_at desc, id desc
limit $3`

const getChannelSQL = `
select ` + channelColumns + `
from den_channels.channels
where id = $1
	and archived_at is null`

const upsertProjectDefaultChannelSQL = `
insert into den_channels.channels (
	slug, display_name, kind, project_id, created_by, visibility, settings, created_at, updated_at
) values ($1, $2, 'project_default', $3, $4, 'normal', $5, $6, $6)
on conflict (project_id) where project_id is not null and kind = 'project_default' and archived_at is null
do update set
	slug = excluded.slug,
	display_name = excluded.display_name,
	settings = excluded.settings,
	updated_at = excluded.updated_at
returning ` + channelColumns

const linkExistingDefaultChannelSQL = `
update den_channels.channels
set project_id = $2,
	kind = 'project_default',
	updated_at = $4
where id = $1
	and archived_at is null
returning ` + channelColumns

const appendMessageSQL = `
insert into den_channels.channel_messages (
	channel_id, sender_type, sender_identity, body, message_kind, source_kind,
	source_id, source_project_id, target_project_id, target_task_id, assignment_id,
	worker_run_id, worker_role, profile_identity, agent_instance_id, pool_member_id,
	session_owner_id, session_id, summary, deep_link, thread_root_message_id,
	reply_to_message_id, metadata, dedupe_key, created_at
) values (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9, $10, $11,
	$12, $13, $14, $15, $16,
	$17, $18, $19, $20, $21,
	$22, $23, $24, $25
)
on conflict (dedupe_key) do update
set dedupe_key = excluded.dedupe_key
returning ` + messageColumns

const listMessagesSQL = `
select ` + messageColumns + `
from den_channels.channel_messages
where channel_id = $1
	and deleted_at is null
	and ($2::bigint is null or id > $2)
	and ($3::text is null or assignment_id = $3)
order by id asc
limit $4`

const upsertMembershipSQL = `
insert into den_channels.channel_memberships (
	channel_id, member_type, member_identity, profile_identity, membership_status,
	wake_policy, can_send, can_react, can_invite, membership_purpose, settings,
	created_at, updated_at, left_at
) values (
	$1, $2, $3, $4, $5,
	$6, $7, $8, $9, $10, $11,
	$12, $13, case when $5 = 'left' then $13::timestamptz else null end
)
on conflict (channel_id, member_identity, membership_purpose) do update
set member_type = excluded.member_type,
	profile_identity = excluded.profile_identity,
	membership_status = excluded.membership_status,
	wake_policy = excluded.wake_policy,
	can_send = excluded.can_send,
	can_react = excluded.can_react,
	can_invite = excluded.can_invite,
	settings = excluded.settings,
	updated_at = excluded.updated_at,
	left_at = excluded.left_at
returning ` + membershipColumns

const listMembershipsSQL = `
select ` + prefixedMembershipColumns + `
from den_channels.channel_memberships m
join den_channels.channels c on c.id = m.channel_id
where ($1::text is null or m.member_identity = $1)
	and ($2::text is null or m.membership_purpose = $2)
	and ($3::text is null or c.project_id = $3)
	and ($4::bigint is null or m.channel_id = $4)
	and ($5::boolean or m.membership_status <> 'left')
order by m.updated_at desc, m.id desc
limit $6`

const addReactionSQL = `
insert into den_channels.channel_reactions (
	message_id, channel_id, reactor_type, reactor_identity, reaction, created_at
)
select id, channel_id, $2, $3, $4, $5
from den_channels.channel_messages
where id = $1
	and deleted_at is null
on conflict (message_id, reactor_identity, reaction) do update
set deleted_at = null
returning ` + reactionColumns

const listReadCursorsSQL = `
select ` + readCursorColumns + `
from den_channels.channel_read_cursors
where channel_id = $1
order by reader_type asc, reader_identity asc`

const upsertReadCursorSQL = `
insert into den_channels.channel_read_cursors (
	channel_id, reader_type, reader_identity, instance_id, last_read_message_id, last_read_at
) values ($1, $2, $3, $4, $5, $6)
on conflict (channel_id, reader_type, reader_identity) do update
set instance_id = excluded.instance_id,
	last_read_message_id = excluded.last_read_message_id,
	last_read_at = excluded.last_read_at
returning ` + readCursorColumns
