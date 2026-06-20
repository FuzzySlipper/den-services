package timeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresStoreObservationScopeMatching(t *testing.T) {
	adminURL := os.Getenv("DEN_SERVICES_TEST_ADMIN_DATABASE_URL")
	if adminURL == "" {
		t.Skip("DEN_SERVICES_TEST_ADMIN_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fixture := newTimelinePostgresFixture(t, ctx, adminURL)
	defer fixture.tearDown(t, ctx)
	fixture.createSchema(t, ctx)

	channelID, linkedMessageID := fixture.seedTimelineSources(t, ctx)
	store := NewStore(fixture.pool)
	scope, err := NewChannelScope(channelID)
	if err != nil {
		t.Fatal(err)
	}

	items, err := store.ListItems(ctx, ListItemsQuery{
		Scope:        scope,
		Limit:        20,
		IncludeDebug: false,
	})
	if err != nil {
		t.Fatalf("ListItems(channel default) error = %v", err)
	}

	got := itemSummaries(items)
	want := []string{"channel-direct", "channel-message-link"}
	if !sameStrings(got, want) {
		t.Fatalf("channel observation summaries = %v, want %v", got, want)
	}
	for _, item := range items {
		if item.TimelineID == "msg:"+sourceIDFromInt(linkedMessageID) {
			continue
		}
		if item.ChannelID == nil || *item.ChannelID != channelID {
			t.Fatalf("item %s channel_id = %v, want %d", item.TimelineID, item.ChannelID, channelID)
		}
	}

	withDebug, err := store.ListItems(ctx, ListItemsQuery{
		Scope:        scope,
		Limit:        20,
		IncludeDebug: true,
	})
	if err != nil {
		t.Fatalf("ListItems(channel debug) error = %v", err)
	}
	if !containsSummary(withDebug, "channel-debug") {
		t.Fatalf("debug channel event missing with include_debug=true: %v", itemSummaries(withDebug))
	}
}

func TestPostgresStoreProjectScopeMatching(t *testing.T) {
	adminURL := os.Getenv("DEN_SERVICES_TEST_ADMIN_DATABASE_URL")
	if adminURL == "" {
		t.Skip("DEN_SERVICES_TEST_ADMIN_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fixture := newTimelinePostgresFixture(t, ctx, adminURL)
	defer fixture.tearDown(t, ctx)
	fixture.createSchema(t, ctx)
	fixture.seedTimelineSources(t, ctx)

	store := NewStore(fixture.pool)
	scope, err := NewProjectScope("project-alpha")
	if err != nil {
		t.Fatal(err)
	}

	items, err := store.ListItems(ctx, ListItemsQuery{
		Scope:        scope,
		Limit:        20,
		IncludeDebug: false,
	})
	if err != nil {
		t.Fatalf("ListItems(project default) error = %v", err)
	}

	if !containsTimelinePrefix(items, "msg:") {
		t.Fatalf("project timeline missing conversation message: %v", timelineIDsFromDomainItems(items))
	}
	got := itemSummaries(items)
	want := []string{"project-direct"}
	if !sameStrings(got, want) {
		t.Fatalf("project observation summaries = %v, want %v", got, want)
	}
	if containsSummary(items, "project-debug") {
		t.Fatalf("debug project event included by default: %v", got)
	}

	withDebug, err := store.ListItems(ctx, ListItemsQuery{
		Scope:        scope,
		Limit:        20,
		IncludeDebug: true,
	})
	if err != nil {
		t.Fatalf("ListItems(project debug) error = %v", err)
	}
	if !containsSummary(withDebug, "project-debug") {
		t.Fatalf("debug project event missing with include_debug=true: %v", itemSummaries(withDebug))
	}
}

type timelinePostgresFixture struct {
	adminURL string
	database string
	pool     *pgxpool.Pool
}

func newTimelinePostgresFixture(t *testing.T, ctx context.Context, adminURL string) *timelinePostgresFixture {
	t.Helper()
	databaseName := "den_timeline_test_" + randomHex(t)
	databaseURL, err := databaseURLForTestName(adminURL, databaseName)
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connecting admin database: %v", err)
	}
	defer adminPool.Close()

	if _, err := adminPool.Exec(ctx, "create database "+pgx.Identifier{databaseName}.Sanitize()); err != nil {
		t.Fatalf("creating test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connecting test database: %v", err)
	}
	return &timelinePostgresFixture{
		adminURL: adminURL,
		database: databaseName,
		pool:     pool,
	}
}

func (f *timelinePostgresFixture) tearDown(t *testing.T, ctx context.Context) {
	t.Helper()
	f.pool.Close()
	adminPool, err := pgxpool.New(ctx, f.adminURL)
	if err != nil {
		t.Fatalf("connecting admin database for teardown: %v", err)
	}
	defer adminPool.Close()
	if _, err := adminPool.Exec(ctx, "drop database if exists "+pgx.Identifier{f.database}.Sanitize()+" with (force)"); err != nil {
		t.Fatalf("dropping test database: %v", err)
	}
}

func (f *timelinePostgresFixture) createSchema(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, err := f.pool.Exec(ctx, timelineTestDDL); err != nil {
		t.Fatalf("creating timeline test schema: %v", err)
	}
}

func (f *timelinePostgresFixture) seedTimelineSources(t *testing.T, ctx context.Context) (int64, int64) {
	t.Helper()
	baseTime := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	var channelID int64
	var otherChannelID int64
	if err := f.pool.QueryRow(ctx, insertChannelSQLForTest, "timeline-alpha", "project-alpha", baseTime).Scan(&channelID); err != nil {
		t.Fatalf("inserting alpha channel: %v", err)
	}
	if err := f.pool.QueryRow(ctx, insertChannelSQLForTest, "timeline-beta", "project-beta", baseTime).Scan(&otherChannelID); err != nil {
		t.Fatalf("inserting beta channel: %v", err)
	}
	var linkedMessageID int64
	if err := f.pool.QueryRow(ctx, insertMessageSQLForTest, channelID, "hello", baseTime.Add(time.Second)).Scan(&linkedMessageID); err != nil {
		t.Fatalf("inserting linked message: %v", err)
	}
	if _, err := f.pool.Exec(ctx, insertMessageSQLForTest, otherChannelID, "other", baseTime.Add(2*time.Second)); err != nil {
		t.Fatalf("inserting other message: %v", err)
	}

	insertActivity(t, ctx, f.pool, baseTime.Add(3*time.Second), `{"summary":"channel-direct","work_ref":{"channel_id":`+sourceIDFromInt(channelID)+`}}`)
	insertActivity(t, ctx, f.pool, baseTime.Add(4*time.Second), `{"summary":"channel-message-link","work_ref":{"channel_message_id":`+sourceIDFromInt(linkedMessageID)+`}}`)
	insertActivity(t, ctx, f.pool, baseTime.Add(5*time.Second), `{"summary":"project-direct","work_ref":{"project_id":"project-alpha"}}`)
	insertActivity(t, ctx, f.pool, baseTime.Add(6*time.Second), `{"summary":"channel-debug","visibility":"debug","work_ref":{"channel_id":`+sourceIDFromInt(channelID)+`}}`)
	insertActivity(t, ctx, f.pool, baseTime.Add(7*time.Second), `{"summary":"project-debug","visibility":"debug","work_ref":{"project_id":"project-alpha"}}`)
	insertActivity(t, ctx, f.pool, baseTime.Add(8*time.Second), `{"summary":"unrelated","work_ref":{"channel_id":`+sourceIDFromInt(otherChannelID)+`,"project_id":"project-beta"}}`)
	return channelID, linkedMessageID
}

func insertActivity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, occurredAt time.Time, payload string) {
	t.Helper()
	_, err := pool.Exec(ctx, insertActivitySQLForTest, "agent_activity.v1", []byte(`{"profile":"tester","instance_id":"tester@fixture"}`), []byte(payload), occurredAt)
	if err != nil {
		t.Fatalf("inserting activity %s: %v", payload, err)
	}
}

func itemSummaries(items []TimelineItem) []string {
	var summaries []string
	for _, item := range items {
		if item.SourceDomain != SourceDomainObservation || item.Summary == nil {
			continue
		}
		summaries = append(summaries, *item.Summary)
	}
	return summaries
}

func containsSummary(items []TimelineItem, summary string) bool {
	for _, item := range items {
		if item.Summary != nil && *item.Summary == summary {
			return true
		}
	}
	return false
}

func containsTimelinePrefix(items []TimelineItem, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(item.TimelineID, prefix) {
			return true
		}
	}
	return false
}

func timelineIDsFromDomainItems(items []TimelineItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.TimelineID)
	}
	return ids
}

func randomHex(t *testing.T) string {
	t.Helper()
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		t.Fatalf("generating random suffix: %v", err)
	}
	return hex.EncodeToString(bytes)
}

func databaseURLForTestName(rawURL string, databaseName string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing admin database url: %w", err)
	}
	parsed.Path = "/" + databaseName
	return parsed.String(), nil
}

const timelineTestDDL = `
create schema den_channels;
create schema den_observation;

create table den_channels.channels (
	id bigserial primary key,
	slug text not null unique,
	display_name text not null,
	kind text not null,
	project_id text null,
	space_id text null,
	created_by text not null,
	visibility text not null,
	settings jsonb not null default '{}'::jsonb,
	created_at timestamptz not null,
	updated_at timestamptz not null,
	archived_at timestamptz null
);

create table den_channels.channel_messages (
	id bigserial primary key,
	channel_id bigint not null references den_channels.channels(id),
	sender_type text not null,
	sender_identity text not null,
	body text not null,
	message_kind text not null,
	source_kind text not null,
	source_id text null,
	source_project_id text null,
	target_project_id text null,
	target_task_id bigint null,
	assignment_id text null,
	worker_run_id text null,
	worker_role text null,
	profile_identity text null,
	agent_instance_id text null,
	pool_member_id text null,
	session_owner_id text null,
	session_id text null,
	summary text null,
	deep_link text null,
	thread_root_message_id bigint null,
	reply_to_message_id bigint null,
	metadata jsonb not null default '{}'::jsonb,
	dedupe_key text null unique,
	created_at timestamptz not null,
	edited_at timestamptz null,
	deleted_at timestamptz null
);

create table den_observation.activity_events (
	event_id bigserial primary key,
	event_type text not null,
	payload jsonb not null default '{}'::jsonb,
	display_only boolean not null default true,
	created_at timestamptz not null,
	source_domain text not null,
	agent_identity jsonb null,
	runtime_instance_id text null
);`

const insertChannelSQLForTest = `
insert into den_channels.channels (
	slug, display_name, kind, project_id, created_by, visibility, created_at, updated_at
) values ($1, $1, 'test', $2, 'store-test', 'normal', $3, $3)
returning id`

const insertMessageSQLForTest = `
insert into den_channels.channel_messages (
	channel_id, sender_type, sender_identity, body, message_kind, source_kind, created_at
) values ($1, 'human', 'store-test', $2, 'human_text', 'conversation', $3)
returning id`

const insertActivitySQLForTest = `
insert into den_observation.activity_events (
	event_type, source_domain, agent_identity, payload, display_only, created_at
) values ($1, 'observation', $2, $3, true, $4)`
