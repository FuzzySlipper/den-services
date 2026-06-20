package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDestination struct {
	pool *pgxpool.Pool
}

func NewPostgresDestination(pool *pgxpool.Pool) (*PostgresDestination, error) {
	if pool == nil {
		return nil, fmt.Errorf("postgres pool is required")
	}
	return &PostgresDestination{pool: pool}, nil
}

func (d *PostgresDestination) UpsertChannel(ctx context.Context, source string, channel LegacyChannel) (int64, error) {
	var channelID int64
	query := upsertChannelBySlugSQL
	if channel.Kind == "project_default" && channel.ProjectID != nil {
		query = upsertProjectDefaultChannelSQL
	}
	err := d.pool.QueryRow(ctx, query,
		source,
		channel.ID,
		channel.Slug,
		channel.DisplayName,
		channel.Kind,
		channel.ProjectID,
		channel.SpaceID,
		channel.CreatedBy,
		normalizeVisibility(channel.Visibility),
		channel.Settings,
		channel.CreatedAt,
		channel.UpdatedAt,
		channel.ArchivedAt,
	).Scan(&channelID)
	if err != nil {
		return 0, err
	}
	return channelID, nil
}

func (d *PostgresDestination) UpsertMessage(ctx context.Context, source string, message LegacyMessage, channelID int64) (int64, error) {
	var messageID int64
	sourceID := fmt.Sprintf("den-channels:channel_messages:%d", message.ID)
	dedupeKey := fmt.Sprintf("legacy:den-channels:message:%d", message.ID)
	err := d.pool.QueryRow(ctx, upsertMessageSQL,
		source,
		message.ID,
		message.ChannelID,
		channelID,
		message.SenderType,
		message.SenderIdentity,
		message.Body,
		normalizeMessageKind(message.MessageKind),
		"legacy_import",
		sourceID,
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
		legacyMetadata(message),
		dedupeKey,
		message.CreatedAt,
		message.EditedAt,
		message.DeletedAt,
	).Scan(&messageID)
	if err != nil {
		return 0, err
	}
	return messageID, nil
}

func (d *PostgresDestination) UpdateMessageReferences(ctx context.Context, source string, message LegacyMessage) error {
	_, err := d.pool.Exec(ctx, updateMessageReferencesSQL,
		source,
		message.ID,
		message.ThreadRootMessageID,
		message.ReplyToMessageID,
	)
	return err
}

func (d *PostgresDestination) UpsertMembership(ctx context.Context, source string, membership LegacyMembership, channelID int64) (int64, error) {
	var membershipID int64
	var profileIdentity *string
	if membership.MemberType == "agent" {
		profileIdentity = &membership.MemberIdentity
	}
	err := d.pool.QueryRow(ctx, upsertMembershipSQL,
		source,
		membership.ID,
		membership.ChannelID,
		channelID,
		membership.MemberType,
		membership.MemberIdentity,
		profileIdentity,
		membership.MembershipStatus,
		membership.WakePolicy,
		membership.CanSend,
		membership.CanReact,
		membership.CanInvite,
		membership.MembershipPurpose,
		membership.Settings,
		membership.CreatedAt,
		membership.UpdatedAt,
	).Scan(&membershipID)
	if err != nil {
		return 0, err
	}
	return membershipID, nil
}

func (d *PostgresDestination) UpsertReaction(ctx context.Context, source string, reaction LegacyReaction, messageID int64, channelID int64) (int64, error) {
	var reactionID int64
	err := d.pool.QueryRow(ctx, upsertReactionSQL,
		source,
		reaction.ID,
		reaction.MessageID,
		messageID,
		channelID,
		reaction.ReactorType,
		reaction.ReactorIdentity,
		reaction.Reaction,
		reaction.CreatedAt,
	).Scan(&reactionID)
	if err != nil {
		return 0, err
	}
	return reactionID, nil
}

func (d *PostgresDestination) UpsertReadCursor(ctx context.Context, source string, cursor LegacyReadCursor, channelID int64, messageID *int64) error {
	_, err := d.pool.Exec(ctx, upsertReadCursorSQL,
		source,
		cursor.ID,
		channelID,
		cursor.ReaderType,
		cursor.ReaderIdentity,
		cursor.InstanceID,
		messageID,
		cursor.LastReadAt,
	)
	return err
}

func (d *PostgresDestination) UpsertProjectLink(ctx context.Context, source string, link LegacyProjectLink, channelID int64) (int64, error) {
	var projectLinkID int64
	err := d.pool.QueryRow(ctx, upsertProjectLinkSQL,
		source,
		link.ID,
		link.ChannelID,
		channelID,
		link.ProjectID,
		normalizeLinkKind(link.RelationKind),
		"legacy-import",
		link.CreatedAt,
		link.IsPrimary,
		link.Settings,
	).Scan(&projectLinkID)
	if err != nil {
		return 0, err
	}
	return projectLinkID, nil
}

func (d *PostgresDestination) Counts(ctx context.Context) (DestinationCounts, error) {
	var counts DestinationCounts
	err := d.pool.QueryRow(ctx, destinationCountsSQL).Scan(
		&counts.Channels,
		&counts.Messages,
		&counts.Memberships,
		&counts.Reactions,
		&counts.ReadCursors,
		&counts.ProjectLinks,
		&counts.ChatHistory,
	)
	if err != nil {
		return DestinationCounts{}, err
	}
	return counts, nil
}

func legacyMetadata(message LegacyMessage) json.RawMessage {
	metadata := map[string]any{}
	if json.Valid(message.Metadata) {
		_ = json.Unmarshal(message.Metadata, &metadata)
	}
	metadata["legacy_import_source"] = SourceLegacyDenChannels
	metadata["legacy_channel_message_id"] = message.ID
	metadata["legacy_channel_id"] = message.ChannelID
	metadata["display_only"] = true
	if message.LegacySourceKind != nil {
		metadata["legacy_source_kind"] = *message.LegacySourceKind
		metadata["legacy_executable_source"] = strings.EqualFold(*message.LegacySourceKind, "wake_event")
	}
	if message.LegacySourceID != nil {
		metadata["legacy_source_id"] = *message.LegacySourceID
	}
	if message.LegacyDedupeKey != nil {
		metadata["legacy_dedupe_key"] = *message.LegacyDedupeKey
	}
	if message.DeliveryRequestID != nil {
		metadata["legacy_delivery_request_id"] = *message.DeliveryRequestID
	}
	if message.CheckpointType != nil {
		metadata["legacy_checkpoint_type"] = *message.CheckpointType
	}
	if message.CheckpointHandle != nil {
		metadata["legacy_checkpoint_handle"] = *message.CheckpointHandle
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return json.RawMessage(`{"display_only":true,"legacy_import_source":"legacy_den_channels_sqlite"}`)
	}
	return data
}

func normalizeVisibility(value string) string {
	if strings.TrimSpace(value) == "" {
		return "normal"
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeMessageKind(value string) string {
	if strings.TrimSpace(value) == "" {
		return "system_event"
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeLinkKind(value string) string {
	if strings.TrimSpace(value) == "" {
		return "linked"
	}
	return strings.ToLower(strings.TrimSpace(value))
}

const upsertChannelBySlugSQL = `
with upserted as (
	insert into den_channels.channels (
		slug, display_name, kind, project_id, space_id, created_by, visibility,
		settings, created_at, updated_at, archived_at
	) values (
		$3, $4, $5, $6, $7, $8, $9,
		$10, $11, $12, $13
	)
	on conflict (slug) do update set
		display_name = excluded.display_name,
		kind = excluded.kind,
		project_id = excluded.project_id,
		space_id = excluded.space_id,
		created_by = excluded.created_by,
		visibility = excluded.visibility,
		settings = excluded.settings,
		updated_at = excluded.updated_at,
		archived_at = excluded.archived_at
	returning id
),
mapped as (
	insert into den_channels.legacy_import_channels (
		source, legacy_id, channel_id, legacy_updated_at
	)
	select $1, $2, id, $12
	from upserted
	on conflict (source, legacy_id) do update set
		channel_id = excluded.channel_id,
		legacy_updated_at = excluded.legacy_updated_at,
		imported_at = now()
	returning channel_id
)
select channel_id
from mapped`

const upsertProjectDefaultChannelSQL = `
with upserted as (
	insert into den_channels.channels (
		slug, display_name, kind, project_id, space_id, created_by, visibility,
		settings, created_at, updated_at, archived_at
	) values (
		$3, $4, $5, $6, $7, $8, $9,
		$10, $11, $12, $13
	)
	on conflict (project_id) where project_id is not null and kind = 'project_default' and archived_at is null
	do update set
		slug = excluded.slug,
		display_name = excluded.display_name,
		space_id = excluded.space_id,
		created_by = excluded.created_by,
		visibility = excluded.visibility,
		settings = excluded.settings,
		updated_at = excluded.updated_at,
		archived_at = excluded.archived_at
	returning id
),
mapped as (
	insert into den_channels.legacy_import_channels (
		source, legacy_id, channel_id, legacy_updated_at
	)
	select $1, $2, id, $12
	from upserted
	on conflict (source, legacy_id) do update set
		channel_id = excluded.channel_id,
		legacy_updated_at = excluded.legacy_updated_at,
		imported_at = now()
	returning channel_id
)
select channel_id
from mapped`

const upsertMessageSQL = `
with upserted as (
	insert into den_channels.channel_messages (
		channel_id, sender_type, sender_identity, body, message_kind, source_kind,
		source_id, source_project_id, target_project_id, target_task_id, assignment_id,
		worker_run_id, worker_role, profile_identity, agent_instance_id, pool_member_id,
		session_owner_id, session_id, summary, deep_link, metadata, dedupe_key,
		created_at, edited_at, deleted_at
	) values (
		$4, $5, $6, $7, $8, $9,
		$10, $11, $12, $13, $14,
		$15, $16, $17, $18, $19,
		$20, $21, $22, $23, $24, $25,
		$26, $27, $28
	)
	on conflict (dedupe_key) do update set
		body = excluded.body,
		metadata = excluded.metadata,
		edited_at = excluded.edited_at,
		deleted_at = excluded.deleted_at
	returning id
),
mapped as (
	insert into den_channels.legacy_import_messages (
		source, legacy_id, message_id, legacy_channel_id, legacy_created_at
	)
	select $1, $2, id, $3, $26
	from upserted
	on conflict (source, legacy_id) do update set
		message_id = excluded.message_id,
		legacy_channel_id = excluded.legacy_channel_id,
		legacy_created_at = excluded.legacy_created_at,
		imported_at = now()
	returning message_id
)
select message_id
from mapped`

const updateMessageReferencesSQL = `
update den_channels.channel_messages m
set thread_root_message_id = thread_map.message_id,
	reply_to_message_id = reply_map.message_id
from den_channels.legacy_import_messages current_map
left join den_channels.legacy_import_messages thread_map
	on thread_map.source = current_map.source
	and thread_map.legacy_id = $3
left join den_channels.legacy_import_messages reply_map
	on reply_map.source = current_map.source
	and reply_map.legacy_id = $4
where current_map.source = $1
	and current_map.legacy_id = $2
	and m.id = current_map.message_id`

const upsertMembershipSQL = `
with upserted as (
	insert into den_channels.channel_memberships (
		channel_id, member_type, member_identity, profile_identity, membership_status,
		wake_policy, can_send, can_react, can_invite, membership_purpose, settings,
		created_at, updated_at, left_at
	) values (
		$4, $5, $6, $7, $8,
		$9, $10, $11, $12, $13, $14,
		$15, $16, case when $8 = 'left' then $16::timestamptz else null end
	)
	on conflict (channel_id, member_identity, membership_purpose) do update set
		member_type = excluded.member_type,
		profile_identity = excluded.profile_identity,
		membership_status = excluded.membership_status,
		wake_policy = excluded.wake_policy,
		can_send = excluded.can_send,
		can_react = excluded.can_react,
		can_invite = excluded.can_invite,
		settings = excluded.settings,
		updated_at = excluded.updated_at,
		left_at = excluded.left_at
	returning id
),
mapped as (
	insert into den_channels.legacy_import_memberships (
		source, legacy_id, membership_id, legacy_channel_id
	)
	select $1, $2, id, $3
	from upserted
	on conflict (source, legacy_id) do update set
		membership_id = excluded.membership_id,
		legacy_channel_id = excluded.legacy_channel_id,
		imported_at = now()
	returning membership_id
)
select membership_id
from mapped`

const upsertReactionSQL = `
with upserted as (
	insert into den_channels.channel_reactions (
		message_id, channel_id, reactor_type, reactor_identity, reaction, created_at
	) values (
		$4, $5, $6, $7, $8, $9
	)
	on conflict (message_id, reactor_identity, reaction) do update set
		deleted_at = null
	returning id
),
mapped as (
	insert into den_channels.legacy_import_reactions (
		source, legacy_id, reaction_id, legacy_message_id
	)
	select $1, $2, id, $3
	from upserted
	on conflict (source, legacy_id) do update set
		reaction_id = excluded.reaction_id,
		legacy_message_id = excluded.legacy_message_id,
		imported_at = now()
	returning reaction_id
)
select reaction_id
from mapped`

const upsertReadCursorSQL = `
with upserted as (
	insert into den_channels.channel_read_cursors (
		channel_id, reader_type, reader_identity, instance_id, last_read_message_id, last_read_at
	) values ($3, $4, $5, $6, $7, $8)
	on conflict (channel_id, reader_type, reader_identity) do update set
		instance_id = excluded.instance_id,
		last_read_message_id = excluded.last_read_message_id,
		last_read_at = excluded.last_read_at
	returning channel_id, reader_type, reader_identity
)
insert into den_channels.legacy_import_read_cursors (
	source, legacy_id, channel_id, reader_type, reader_identity
)
select $1, $2, channel_id, reader_type, reader_identity
from upserted
on conflict (source, legacy_id) do update set
	channel_id = excluded.channel_id,
	reader_type = excluded.reader_type,
	reader_identity = excluded.reader_identity,
	imported_at = now()`

const upsertProjectLinkSQL = `
with upserted as (
	insert into den_channels.channel_project_links (
		channel_id, project_id, link_kind, created_by, created_at
	) values ($4, $5, $6, $7, $8)
	on conflict (channel_id, project_id, link_kind) do update set
		created_by = excluded.created_by,
		created_at = excluded.created_at,
		deleted_at = null
	returning id
),
mapped as (
	insert into den_channels.legacy_import_project_links (
		source, legacy_id, project_link_id, legacy_channel_id, legacy_is_primary, legacy_settings
	)
	select $1, $2, id, $3, $9, $10
	from upserted
	on conflict (source, legacy_id) do update set
		project_link_id = excluded.project_link_id,
		legacy_channel_id = excluded.legacy_channel_id,
		legacy_is_primary = excluded.legacy_is_primary,
		legacy_settings = excluded.legacy_settings,
		imported_at = now()
	returning project_link_id
)
select project_link_id
from mapped`

const destinationCountsSQL = `
select
	(select count(*) from den_channels.channels) as channels,
	(select count(*) from den_channels.channel_messages) as messages,
	(select count(*) from den_channels.channel_memberships) as memberships,
	(select count(*) from den_channels.channel_reactions) as reactions,
	(select count(*) from den_channels.channel_read_cursors) as read_cursors,
	(select count(*) from den_channels.channel_project_links) as project_links,
	(select count(*) from den_channels.chat_history) as chat_history`
