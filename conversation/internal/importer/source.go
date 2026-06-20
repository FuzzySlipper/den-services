package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func LoadSQLite(ctx context.Context, path string, limit int) (*SourceData, ExclusionCounts, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ExclusionCounts{}, errors.New("source path is required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, ExclusionCounts{}, fmt.Errorf("opening sqlite source: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, ExclusionCounts{}, fmt.Errorf("pinging sqlite source: %w", err)
	}

	schema, err := loadSQLiteSchema(ctx, db)
	if err != nil {
		return nil, ExclusionCounts{}, err
	}

	data := &SourceData{}
	data.Channels, err = loadChannels(ctx, db, schema, limit)
	if err != nil {
		return nil, ExclusionCounts{}, err
	}
	data.Messages, err = loadMessages(ctx, db, schema, limit)
	if err != nil {
		return nil, ExclusionCounts{}, err
	}
	data.Memberships, err = loadMemberships(ctx, db, schema, limit)
	if err != nil {
		return nil, ExclusionCounts{}, err
	}
	data.Reactions, err = loadReactions(ctx, db, schema, limit)
	if err != nil {
		return nil, ExclusionCounts{}, err
	}
	data.ReadCursors, err = loadReadCursors(ctx, db, schema, limit)
	if err != nil {
		return nil, ExclusionCounts{}, err
	}
	data.ProjectLinks, err = loadProjectLinks(ctx, db, schema, limit)
	if err != nil {
		return nil, ExclusionCounts{}, err
	}

	exclusions := ExclusionCounts{}
	filtered := data.ReadCursors[:0]
	for _, cursor := range data.ReadCursors {
		if normalizeReaderType(cursor.ReaderType) != "human" {
			exclusions.NonHumanReadCursors++
			continue
		}
		cursor.ReaderType = "human"
		filtered = append(filtered, cursor)
	}
	data.ReadCursors = filtered
	return data, exclusions, nil
}

type sqliteSchema map[string]map[string]bool

func loadSQLiteSchema(ctx context.Context, db *sql.DB) (sqliteSchema, error) {
	rows, err := db.QueryContext(ctx, `
select name
from sqlite_master
where type = 'table'`)
	if err != nil {
		return nil, fmt.Errorf("listing sqlite tables: %w", err)
	}
	defer rows.Close()

	schema := sqliteSchema{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("scanning sqlite table: %w", err)
		}
		columns, err := loadSQLiteColumns(ctx, db, table)
		if err != nil {
			return nil, err
		}
		schema[table] = columns
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading sqlite tables: %w", err)
	}
	return schema, nil
}

func loadSQLiteColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "pragma table_info("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("listing sqlite columns for %s: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("scanning sqlite columns for %s: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading sqlite columns for %s: %w", table, err)
	}
	return columns, nil
}

func loadChannels(ctx context.Context, db *sql.DB, schema sqliteSchema, limit int) ([]LegacyChannel, error) {
	if !schema.hasTable("channels") {
		return nil, nil
	}
	query := "select " + strings.Join([]string{
		column(schema, "channels", "id", "0"),
		column(schema, "channels", "slug", "''"),
		column(schema, "channels", "display_name", "''"),
		column(schema, "channels", "kind", "'ad_hoc'"),
		column(schema, "channels", "project_id", "null"),
		column(schema, "channels", "space_id", "null"),
		column(schema, "channels", "created_by", "'legacy-import'"),
		column(schema, "channels", "visibility", "'normal'"),
		column(schema, "channels", "settings_json", "null"),
		column(schema, "channels", "created_at", "null"),
		column(schema, "channels", "updated_at", "null"),
		column(schema, "channels", "archived_at", "null"),
	}, ", ") + " from channels order by id asc" + limitClause(limit)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying legacy channels: %w", err)
	}
	defer rows.Close()

	var channels []LegacyChannel
	for rows.Next() {
		var channel LegacyChannel
		var projectID, spaceID, settings, createdAt, updatedAt, archivedAt sql.NullString
		if err := rows.Scan(
			&channel.ID,
			&channel.Slug,
			&channel.DisplayName,
			&channel.Kind,
			&projectID,
			&spaceID,
			&channel.CreatedBy,
			&channel.Visibility,
			&settings,
			&createdAt,
			&updatedAt,
			&archivedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning legacy channel: %w", err)
		}
		channel.ProjectID = nullStringPtr(projectID)
		channel.SpaceID = nullStringPtr(spaceID)
		channel.Settings = jsonObject(settings)
		channel.CreatedAt = parseTimeOrNow(createdAt)
		channel.UpdatedAt = parseTimeOrDefault(updatedAt, channel.CreatedAt)
		channel.ArchivedAt = parseOptionalTime(archivedAt)
		channels = append(channels, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading legacy channels: %w", err)
	}
	return channels, nil
}

func loadMessages(ctx context.Context, db *sql.DB, schema sqliteSchema, limit int) ([]LegacyMessage, error) {
	if !schema.hasTable("channel_messages") {
		return nil, nil
	}
	columns := []string{
		column(schema, "channel_messages", "id", "0"),
		column(schema, "channel_messages", "channel_id", "0"),
		column(schema, "channel_messages", "sender_type", "'system'"),
		column(schema, "channel_messages", "sender_identity", "'legacy-import'"),
		column(schema, "channel_messages", "body", "''"),
		column(schema, "channel_messages", "message_kind", "'system_event'"),
		column(schema, "channel_messages", "source_kind", "null"),
		column(schema, "channel_messages", "source_id", "null"),
		column(schema, "channel_messages", "source_project_id", "null"),
		column(schema, "channel_messages", "target_project_id", "null"),
		column(schema, "channel_messages", "target_task_id", "null"),
		column(schema, "channel_messages", "assignment_id", "null"),
		column(schema, "channel_messages", "checkpoint_type", "null"),
		column(schema, "channel_messages", "checkpoint_handle", "null"),
		column(schema, "channel_messages", "worker_run_id", "null"),
		column(schema, "channel_messages", "worker_role", "null"),
		column(schema, "channel_messages", "profile_identity", "null"),
		column(schema, "channel_messages", "agent_instance_id", "null"),
		column(schema, "channel_messages", "pool_member_id", "null"),
		column(schema, "channel_messages", "session_owner_id", "null"),
		column(schema, "channel_messages", "session_id", "null"),
		column(schema, "channel_messages", "summary", "null"),
		column(schema, "channel_messages", "deep_link", "null"),
		column(schema, "channel_messages", "thread_root_message_id", "null"),
		column(schema, "channel_messages", "reply_to_message_id", "null"),
		column(schema, "channel_messages", "metadata_json", "null"),
		column(schema, "channel_messages", "delivery_request_id", "null"),
		column(schema, "channel_messages", "dedupe_key", "null"),
		column(schema, "channel_messages", "created_at", "null"),
		column(schema, "channel_messages", "edited_at", "null"),
		column(schema, "channel_messages", "deleted_at", "null"),
	}
	query := "select " + strings.Join(columns, ", ") + " from channel_messages order by id asc" + limitClause(limit)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying legacy messages: %w", err)
	}
	defer rows.Close()

	var messages []LegacyMessage
	for rows.Next() {
		var message LegacyMessage
		var sourceKind, sourceID, sourceProjectID, targetProjectID sql.NullString
		var targetTaskID sql.NullInt64
		var assignmentID, checkpointType, checkpointHandle, workerRunID, workerRole sql.NullString
		var profileIdentity, agentInstanceID, poolMemberID, sessionOwnerID, sessionID sql.NullString
		var summary, deepLink sql.NullString
		var threadRootID, replyToID sql.NullInt64
		var metadata, deliveryRequestID, dedupeKey sql.NullString
		var createdAt, editedAt, deletedAt sql.NullString
		if err := rows.Scan(
			&message.ID,
			&message.ChannelID,
			&message.SenderType,
			&message.SenderIdentity,
			&message.Body,
			&message.MessageKind,
			&sourceKind,
			&sourceID,
			&sourceProjectID,
			&targetProjectID,
			&targetTaskID,
			&assignmentID,
			&checkpointType,
			&checkpointHandle,
			&workerRunID,
			&workerRole,
			&profileIdentity,
			&agentInstanceID,
			&poolMemberID,
			&sessionOwnerID,
			&sessionID,
			&summary,
			&deepLink,
			&threadRootID,
			&replyToID,
			&metadata,
			&deliveryRequestID,
			&dedupeKey,
			&createdAt,
			&editedAt,
			&deletedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning legacy message: %w", err)
		}
		message.SenderType = normalizeActorType(message.SenderType)
		message.LegacySourceKind = nullStringPtr(sourceKind)
		message.LegacySourceID = nullStringPtr(sourceID)
		message.SourceProjectID = nullStringPtr(sourceProjectID)
		message.TargetProjectID = nullStringPtr(targetProjectID)
		message.TargetTaskID = nullInt64Ptr(targetTaskID)
		message.AssignmentID = nullStringPtr(assignmentID)
		message.CheckpointType = nullStringPtr(checkpointType)
		message.CheckpointHandle = nullStringPtr(checkpointHandle)
		message.WorkerRunID = nullStringPtr(workerRunID)
		message.WorkerRole = nullStringPtr(workerRole)
		message.ProfileIdentity = nullStringPtr(profileIdentity)
		message.AgentInstanceID = nullStringPtr(agentInstanceID)
		message.PoolMemberID = nullStringPtr(poolMemberID)
		message.SessionOwnerID = nullStringPtr(sessionOwnerID)
		message.SessionID = nullStringPtr(sessionID)
		message.Summary = nullStringPtr(summary)
		message.DeepLink = nullStringPtr(deepLink)
		message.ThreadRootMessageID = nullInt64Ptr(threadRootID)
		message.ReplyToMessageID = nullInt64Ptr(replyToID)
		message.Metadata = jsonObject(metadata)
		message.DeliveryRequestID = nullStringPtr(deliveryRequestID)
		message.LegacyDedupeKey = nullStringPtr(dedupeKey)
		message.CreatedAt = parseTimeOrNow(createdAt)
		message.EditedAt = parseOptionalTime(editedAt)
		message.DeletedAt = parseOptionalTime(deletedAt)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading legacy messages: %w", err)
	}
	return messages, nil
}

func loadMemberships(ctx context.Context, db *sql.DB, schema sqliteSchema, limit int) ([]LegacyMembership, error) {
	if !schema.hasTable("channel_memberships") {
		return nil, nil
	}
	query := "select " + strings.Join([]string{
		column(schema, "channel_memberships", "id", "0"),
		column(schema, "channel_memberships", "channel_id", "0"),
		column(schema, "channel_memberships", "member_type", "'agent'"),
		column(schema, "channel_memberships", "member_identity", "''"),
		column(schema, "channel_memberships", "membership_status", "'active'"),
		column(schema, "channel_memberships", "wake_policy", "'mentions_only'"),
		column(schema, "channel_memberships", "can_send", "1"),
		column(schema, "channel_memberships", "can_react", "1"),
		column(schema, "channel_memberships", "can_invite", "0"),
		column(schema, "channel_memberships", "membership_purpose", "'ordinary'"),
		column(schema, "channel_memberships", "settings_json", "null"),
		column(schema, "channel_memberships", "created_at", "null"),
		column(schema, "channel_memberships", "updated_at", "null"),
	}, ", ") + " from channel_memberships order by id asc" + limitClause(limit)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying legacy memberships: %w", err)
	}
	defer rows.Close()

	var memberships []LegacyMembership
	for rows.Next() {
		var membership LegacyMembership
		var canSend, canReact, canInvite int
		var purpose, settings, createdAt, updatedAt sql.NullString
		if err := rows.Scan(
			&membership.ID,
			&membership.ChannelID,
			&membership.MemberType,
			&membership.MemberIdentity,
			&membership.MembershipStatus,
			&membership.WakePolicy,
			&canSend,
			&canReact,
			&canInvite,
			&purpose,
			&settings,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning legacy membership: %w", err)
		}
		membership.MemberType = normalizeActorType(membership.MemberType)
		membership.CanSend = canSend != 0
		membership.CanReact = canReact != 0
		membership.CanInvite = canInvite != 0
		membership.MembershipPurpose = nullStringDefault(purpose, "ordinary")
		membership.Settings = jsonObject(settings)
		membership.CreatedAt = parseTimeOrNow(createdAt)
		membership.UpdatedAt = parseTimeOrDefault(updatedAt, membership.CreatedAt)
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading legacy memberships: %w", err)
	}
	return memberships, nil
}

func loadReactions(ctx context.Context, db *sql.DB, schema sqliteSchema, limit int) ([]LegacyReaction, error) {
	if !schema.hasTable("channel_reactions") {
		return nil, nil
	}
	query := "select " + strings.Join([]string{
		column(schema, "channel_reactions", "id", "0"),
		column(schema, "channel_reactions", "channel_message_id", "0"),
		column(schema, "channel_reactions", "reactor_type", "'system'"),
		column(schema, "channel_reactions", "reactor_identity", "'legacy-import'"),
		column(schema, "channel_reactions", "reaction_key", "''"),
		column(schema, "channel_reactions", "created_at", "null"),
	}, ", ") + " from channel_reactions order by id asc" + limitClause(limit)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying legacy reactions: %w", err)
	}
	defer rows.Close()

	var reactions []LegacyReaction
	for rows.Next() {
		var reaction LegacyReaction
		var createdAt sql.NullString
		if err := rows.Scan(
			&reaction.ID,
			&reaction.MessageID,
			&reaction.ReactorType,
			&reaction.ReactorIdentity,
			&reaction.Reaction,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning legacy reaction: %w", err)
		}
		reaction.ReactorType = normalizeActorType(reaction.ReactorType)
		reaction.CreatedAt = parseTimeOrNow(createdAt)
		reactions = append(reactions, reaction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading legacy reactions: %w", err)
	}
	return reactions, nil
}

func loadReadCursors(ctx context.Context, db *sql.DB, schema sqliteSchema, limit int) ([]LegacyReadCursor, error) {
	if !schema.hasTable("channel_read_cursors") {
		return nil, nil
	}
	query := "select " + strings.Join([]string{
		column(schema, "channel_read_cursors", "id", "0"),
		column(schema, "channel_read_cursors", "channel_id", "0"),
		column(schema, "channel_read_cursors", "reader_type", "''"),
		column(schema, "channel_read_cursors", "reader_identity", "''"),
		column(schema, "channel_read_cursors", "instance_id", "null"),
		column(schema, "channel_read_cursors", "last_read_channel_message_id", "null"),
		column(schema, "channel_read_cursors", "last_read_at", "null"),
	}, ", ") + " from channel_read_cursors order by id asc" + limitClause(limit)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying legacy read cursors: %w", err)
	}
	defer rows.Close()

	var cursors []LegacyReadCursor
	for rows.Next() {
		var cursor LegacyReadCursor
		var instanceID, lastReadAt sql.NullString
		var lastReadMessageID sql.NullInt64
		if err := rows.Scan(
			&cursor.ID,
			&cursor.ChannelID,
			&cursor.ReaderType,
			&cursor.ReaderIdentity,
			&instanceID,
			&lastReadMessageID,
			&lastReadAt,
		); err != nil {
			return nil, fmt.Errorf("scanning legacy read cursor: %w", err)
		}
		cursor.InstanceID = nullStringPtr(instanceID)
		cursor.LastReadMessageID = nullInt64Ptr(lastReadMessageID)
		cursor.LastReadAt = parseTimeOrNow(lastReadAt)
		cursors = append(cursors, cursor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading legacy read cursors: %w", err)
	}
	return cursors, nil
}

func loadProjectLinks(ctx context.Context, db *sql.DB, schema sqliteSchema, limit int) ([]LegacyProjectLink, error) {
	if !schema.hasTable("channel_project_links") {
		return nil, nil
	}
	query := "select " + strings.Join([]string{
		column(schema, "channel_project_links", "id", "0"),
		column(schema, "channel_project_links", "channel_id", "0"),
		column(schema, "channel_project_links", "project_id", "''"),
		column(schema, "channel_project_links", "relation_kind", "'linked'"),
		column(schema, "channel_project_links", "is_primary", "0"),
		column(schema, "channel_project_links", "settings_json", "null"),
		column(schema, "channel_project_links", "created_at", "null"),
	}, ", ") + " from channel_project_links order by id asc" + limitClause(limit)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying legacy project links: %w", err)
	}
	defer rows.Close()

	var links []LegacyProjectLink
	for rows.Next() {
		var link LegacyProjectLink
		var isPrimary int
		var settings, createdAt sql.NullString
		if err := rows.Scan(
			&link.ID,
			&link.ChannelID,
			&link.ProjectID,
			&link.RelationKind,
			&isPrimary,
			&settings,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning legacy project link: %w", err)
		}
		link.IsPrimary = isPrimary != 0
		link.Settings = jsonObject(settings)
		link.CreatedAt = parseTimeOrNow(createdAt)
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading legacy project links: %w", err)
	}
	return links, nil
}

func (s sqliteSchema) hasTable(table string) bool {
	_, ok := s[table]
	return ok
}

func column(schema sqliteSchema, table string, name string, fallback string) string {
	if schema[table][name] {
		return name
	}
	return fallback + " as " + name
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func limitClause(limit int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" limit %d", limit)
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	return &value.String
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullStringDefault(value sql.NullString, fallback string) string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return fallback
	}
	return value.String
}

func jsonObject(value sql.NullString) json.RawMessage {
	if !value.Valid || strings.TrimSpace(value.String) == "" || !json.Valid([]byte(value.String)) {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(value.String)
}

func parseTimeOrNow(value sql.NullString) time.Time {
	return parseTimeOrDefault(value, time.Now().UTC())
}

func parseTimeOrDefault(value sql.NullString, fallback time.Time) time.Time {
	parsed := parseOptionalTime(value)
	if parsed == nil {
		return fallback
	}
	return *parsed
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	raw := strings.TrimSpace(value.String)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	return nil
}

func normalizeActorType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "user", "human":
		return "human"
	case "bridge":
		return "system"
	case "":
		return "system"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeReaderType(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "user") {
		return "human"
	}
	return strings.ToLower(strings.TrimSpace(value))
}
