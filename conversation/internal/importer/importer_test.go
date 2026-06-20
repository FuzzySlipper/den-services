package importer

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
)

func TestLoadSQLiteCountsConversationSurfacesAndFiltersNonHumanCursors(t *testing.T) {
	sourcePath := createLegacySQLiteFixture(t)

	data, exclusions, err := LoadSQLite(context.Background(), sourcePath, 0)
	if err != nil {
		t.Fatalf("LoadSQLite() error = %v", err)
	}

	if len(data.Channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(data.Channels))
	}
	if len(data.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(data.Messages))
	}
	if len(data.Memberships) != 1 {
		t.Fatalf("memberships = %d, want 1", len(data.Memberships))
	}
	if len(data.Reactions) != 1 {
		t.Fatalf("reactions = %d, want 1", len(data.Reactions))
	}
	if len(data.ReadCursors) != 1 {
		t.Fatalf("read cursors = %d, want 1", len(data.ReadCursors))
	}
	if len(data.ProjectLinks) != 1 {
		t.Fatalf("project links = %d, want 1", len(data.ProjectLinks))
	}
	if exclusions.NonHumanReadCursors != 1 {
		t.Fatalf("non-human cursor exclusions = %d, want 1", exclusions.NonHumanReadCursors)
	}
	if data.ReadCursors[0].ReaderType != "human" {
		t.Fatalf("reader type = %q, want human", data.ReadCursors[0].ReaderType)
	}
}

func TestRunImportsIdempotentlyThroughDestination(t *testing.T) {
	sourcePath := createLegacySQLiteFixture(t)
	destination := newFakeDestination()
	options := Options{
		SourcePath: sourcePath,
		SourceName: SourceLegacyDenChannels,
		DryRun:     false,
	}

	first, err := Run(context.Background(), options, destination)
	if err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}
	second, err := Run(context.Background(), options, destination)
	if err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}

	if first.Counts != second.Counts {
		t.Fatalf("counts changed: first=%+v second=%+v", first.Counts, second.Counts)
	}
	if destination.channelsCreated != 1 {
		t.Fatalf("channels created = %d, want 1", destination.channelsCreated)
	}
	if destination.messagesCreated != 1 {
		t.Fatalf("messages created = %d, want 1", destination.messagesCreated)
	}
	if destination.membershipsCreated != 1 {
		t.Fatalf("memberships created = %d, want 1", destination.membershipsCreated)
	}
	if destination.reactionsCreated != 1 {
		t.Fatalf("reactions created = %d, want 1", destination.reactionsCreated)
	}
	if destination.cursorsCreated != 1 {
		t.Fatalf("cursors created = %d, want 1", destination.cursorsCreated)
	}
	if destination.projectLinksCreated != 1 {
		t.Fatalf("project links created = %d, want 1", destination.projectLinksCreated)
	}
	message := destination.messages["legacy_den_channels_sqlite/10"]
	if message.LegacySourceKind == nil || *message.LegacySourceKind != "wake_event" {
		t.Fatalf("legacy source kind = %v, want wake_event", message.LegacySourceKind)
	}
	if destination.messageThreadRefs[10] != 10 {
		t.Fatalf("thread ref = %d, want 10", destination.messageThreadRefs[10])
	}
}

func createLegacySQLiteFixture(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/den-channels.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening sqlite fixture: %v", err)
	}
	defer db.Close()

	statements := []string{
		`create table channels (
			id integer primary key,
			slug text not null,
			display_name text not null,
			kind text not null,
			project_id text,
			space_id text,
			created_by text not null,
			visibility text not null,
			settings_json text,
			created_at text,
			updated_at text,
			archived_at text
		)`,
		`create table channel_messages (
			id integer primary key,
			channel_id integer not null,
			sender_type text not null,
			sender_identity text not null,
			body text not null,
			message_kind text,
			source_kind text,
			source_id text,
			source_project_id text,
			target_project_id text,
			target_task_id integer,
			assignment_id text,
			checkpoint_type text,
			checkpoint_handle text,
			worker_run_id text,
			worker_role text,
			profile_identity text,
			agent_instance_id text,
			pool_member_id text,
			session_owner_id text,
			session_id text,
			summary text,
			deep_link text,
			thread_root_message_id integer,
			reply_to_message_id integer,
			metadata_json text,
			delivery_request_id text,
			dedupe_key text,
			created_at text,
			edited_at text,
			deleted_at text
		)`,
		`create table channel_memberships (
			id integer primary key,
			channel_id integer not null,
			member_type text not null,
			member_identity text not null,
			membership_status text not null,
			wake_policy text not null,
			can_send integer not null,
			can_react integer not null,
			can_invite integer not null,
			membership_purpose text,
			settings_json text,
			created_at text,
			updated_at text
		)`,
		`create table channel_reactions (
			id integer primary key,
			channel_message_id integer not null,
			reactor_type text not null,
			reactor_identity text not null,
			reaction_key text not null,
			created_at text
		)`,
		`create table channel_read_cursors (
			id integer primary key,
			channel_id integer not null,
			reader_type text not null,
			reader_identity text not null,
			instance_id text,
			last_read_channel_message_id integer,
			last_read_at text
		)`,
		`create table channel_project_links (
			id integer primary key,
			channel_id integer not null,
			project_id text not null,
			relation_kind text not null,
			is_primary integer not null,
			created_at text,
			settings_json text
		)`,
		`insert into channels values (
			1, 'den-services', 'den-services', 'project_default', 'den-services', null,
			'legacy', 'normal', '{"legacy":true}', '2026-06-19T01:00:00Z',
			'2026-06-19T01:01:00Z', null
		)`,
		`insert into channel_messages values (
			10, 1, 'user', 'patch', 'hello from legacy', 'human_text',
			'wake_event', 'wake-10', 'den-services', 'den-services', 3009,
			'assignment-1', 'checkpoint', 'handle', 'run-1', 'coder',
			'spawned-coder', 'agent@host', 'pool-1', 'session-owner', 'session-1',
			'summary', '/deep/link', 10, null, '{"legacyMetadata":true}',
			'delivery-legacy', 'legacy-dedupe', '2026-06-19T01:02:00Z', null, null
		)`,
		`insert into channel_memberships values (
			20, 1, 'agent', 'spawned-coder', 'active', 'mentions_only',
			1, 1, 0, 'target_work', '{"note":"fixture"}',
			'2026-06-19T01:03:00Z', '2026-06-19T01:04:00Z'
		)`,
		`insert into channel_reactions values (
			30, 10, 'user', 'patch', 'ack', '2026-06-19T01:05:00Z'
		)`,
		`insert into channel_read_cursors values (
			40, 1, 'user', 'patch', null, 10, '2026-06-19T01:06:00Z'
		)`,
		`insert into channel_read_cursors values (
			41, 1, 'agent', 'spawned-coder', 'agent@host', 10, '2026-06-19T01:07:00Z'
		)`,
		`insert into channel_project_links values (
			50, 1, 'den-services', 'linked', 1, '2026-06-19T01:08:00Z', '{"primary":true}'
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("executing fixture statement: %v\n%s", err, statement)
		}
	}
	return path
}

type fakeDestination struct {
	channels            map[string]int64
	messages            map[string]LegacyMessage
	messageIDs          map[int64]int64
	messageThreadRefs   map[int64]int64
	memberships         map[string]int64
	reactions           map[string]int64
	cursors             map[string]bool
	projectLinks        map[string]int64
	channelsCreated     int
	messagesCreated     int
	membershipsCreated  int
	reactionsCreated    int
	cursorsCreated      int
	projectLinksCreated int
}

func newFakeDestination() *fakeDestination {
	return &fakeDestination{
		channels:          map[string]int64{},
		messages:          map[string]LegacyMessage{},
		messageIDs:        map[int64]int64{},
		messageThreadRefs: map[int64]int64{},
		memberships:       map[string]int64{},
		reactions:         map[string]int64{},
		cursors:           map[string]bool{},
		projectLinks:      map[string]int64{},
	}
}

func (d *fakeDestination) UpsertChannel(_ context.Context, source string, channel LegacyChannel) (int64, error) {
	key := source + "/" + stringID(channel.ID)
	if id, ok := d.channels[key]; ok {
		return id, nil
	}
	id := int64(len(d.channels) + 100)
	d.channels[key] = id
	d.channelsCreated++
	return id, nil
}

func (d *fakeDestination) UpsertMessage(_ context.Context, source string, message LegacyMessage, _ int64) (int64, error) {
	key := source + "/" + stringID(message.ID)
	if id, ok := d.messageIDs[message.ID]; ok {
		d.messages[key] = message
		return id, nil
	}
	id := int64(len(d.messageIDs) + 200)
	d.messageIDs[message.ID] = id
	d.messages[key] = message
	d.messagesCreated++
	return id, nil
}

func (d *fakeDestination) UpdateMessageReferences(_ context.Context, _ string, message LegacyMessage) error {
	if message.ThreadRootMessageID != nil {
		d.messageThreadRefs[message.ID] = *message.ThreadRootMessageID
	}
	return nil
}

func (d *fakeDestination) UpsertMembership(_ context.Context, source string, membership LegacyMembership, _ int64) (int64, error) {
	key := source + "/" + stringID(membership.ID)
	if id, ok := d.memberships[key]; ok {
		return id, nil
	}
	id := int64(len(d.memberships) + 300)
	d.memberships[key] = id
	d.membershipsCreated++
	return id, nil
}

func (d *fakeDestination) UpsertReaction(_ context.Context, source string, reaction LegacyReaction, _ int64, _ int64) (int64, error) {
	key := source + "/" + stringID(reaction.ID)
	if id, ok := d.reactions[key]; ok {
		return id, nil
	}
	id := int64(len(d.reactions) + 400)
	d.reactions[key] = id
	d.reactionsCreated++
	return id, nil
}

func (d *fakeDestination) UpsertReadCursor(_ context.Context, source string, cursor LegacyReadCursor, _ int64, _ *int64) error {
	key := source + "/" + stringID(cursor.ID)
	if d.cursors[key] {
		return nil
	}
	d.cursors[key] = true
	d.cursorsCreated++
	return nil
}

func (d *fakeDestination) UpsertProjectLink(_ context.Context, source string, link LegacyProjectLink, _ int64) (int64, error) {
	key := source + "/" + stringID(link.ID)
	if id, ok := d.projectLinks[key]; ok {
		return id, nil
	}
	id := int64(len(d.projectLinks) + 500)
	d.projectLinks[key] = id
	d.projectLinksCreated++
	return id, nil
}

func (d *fakeDestination) Counts(context.Context) (DestinationCounts, error) {
	return DestinationCounts{
		Channels:     len(d.channels),
		Messages:     len(d.messageIDs),
		Memberships:  len(d.memberships),
		Reactions:    len(d.reactions),
		ReadCursors:  len(d.cursors),
		ProjectLinks: len(d.projectLinks),
		ChatHistory:  len(d.messageIDs),
	}, nil
}

func stringID(id int64) string {
	return strconv.FormatInt(id, 10)
}
