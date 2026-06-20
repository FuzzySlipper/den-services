package timeline

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTimelineOrderingAndInitialPage(t *testing.T) {
	now := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []TimelineItem{
		testObservationItem(11, now.Add(time.Second), false),
		testMessageItem(10, now),
		testObservationItem(12, now, false),
		testMessageItem(13, now.Add(2*time.Second)),
	}}
	service := NewService(store, fixedClock(now), testConfig())
	scope, err := NewChannelScope(123)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.ListItems(context.Background(), scope, "", 3, false)
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}

	got := timelineIDs(response.Items)
	want := []string{"obs:12", "obs:11", "msg:13"}
	if !sameStrings(got, want) {
		t.Fatalf("timeline ids = %v, want %v", got, want)
	}
}

func TestTimelineSameTimestampOrdersConversationBeforeObservation(t *testing.T) {
	now := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []TimelineItem{
		testObservationItem(11, now, false),
		testMessageItem(10, now),
	}}
	service := NewService(store, fixedClock(now), testConfig())
	scope, err := NewChannelScope(123)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.ListItems(context.Background(), scope, "", 10, false)
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}

	got := timelineIDs(response.Items)
	want := []string{"msg:10", "obs:11"}
	if !sameStrings(got, want) {
		t.Fatalf("timeline ids = %v, want %v", got, want)
	}
}

func TestTimelineAfterCursorReturnsStrictlyLaterItems(t *testing.T) {
	now := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	after, err := NewTimelineCursor(now, CursorSourceMessage, 10)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := after.Encode()
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{items: []TimelineItem{
		testMessageItem(10, now),
		testObservationItem(11, now, false),
		testMessageItem(12, now.Add(time.Second)),
	}}
	service := NewService(store, fixedClock(now), testConfig())
	scope, err := NewChannelScope(123)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.ListItems(context.Background(), scope, encoded, 10, false)
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}

	got := timelineIDs(response.Items)
	want := []string{"obs:11", "msg:12"}
	if !sameStrings(got, want) {
		t.Fatalf("timeline ids = %v, want %v", got, want)
	}
}

func TestTimelineDebugExcludedByDefaultAndIncludedByFlag(t *testing.T) {
	now := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{items: []TimelineItem{
		testObservationItem(11, now, true),
		testMessageItem(12, now.Add(time.Second)),
	}}
	service := NewService(store, fixedClock(now), testConfig())
	scope, err := NewChannelScope(123)
	if err != nil {
		t.Fatal(err)
	}

	withoutDebug, err := service.ListItems(context.Background(), scope, "", 10, false)
	if err != nil {
		t.Fatalf("ListItems() without debug error = %v", err)
	}
	if got := timelineIDs(withoutDebug.Items); !sameStrings(got, []string{"msg:12"}) {
		t.Fatalf("without debug ids = %v", got)
	}

	withDebug, err := service.ListItems(context.Background(), scope, "", 10, true)
	if err != nil {
		t.Fatalf("ListItems() with debug error = %v", err)
	}
	if got := timelineIDs(withDebug.Items); !sameStrings(got, []string{"obs:11", "msg:12"}) {
		t.Fatalf("with debug ids = %v", got)
	}
}

func TestCursorRoundTripAndRejectMalformed(t *testing.T) {
	now := time.Date(2026, 6, 20, 8, 0, 0, 123, time.UTC)
	cursor, err := NewTimelineCursor(now, CursorSourceObservation, 42)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := cursor.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if decoded.ID != 42 || decoded.Source != CursorSourceObservation || !decoded.OccurredAt.Equal(now) {
		t.Fatalf("decoded cursor = %+v, want source obs id 42 at %s", decoded, now)
	}
	if _, err := DecodeCursor("not-valid-base64"); err == nil {
		t.Fatal("DecodeCursor() error = nil, want malformed cursor error")
	}
}

type fakeStore struct {
	items []TimelineItem
	query ListItemsQuery
}

func (s *fakeStore) Ping(context.Context) error {
	return nil
}

func (s *fakeStore) ListItems(_ context.Context, query ListItemsQuery) ([]TimelineItem, error) {
	s.query = query
	var items []TimelineItem
	for _, item := range s.items {
		if !query.IncludeDebug && item.RenderKind == RenderKindDiagnostic {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func testMessageItem(id int64, occurredAt time.Time) TimelineItem {
	body := "hello"
	channelID := int64(123)
	sourceID := sourceIDFromInt(id)
	return TimelineItem{
		TimelineID:      "msg:" + sourceID,
		OccurredAt:      occurredAt,
		SourceDomain:    SourceDomainConversation,
		SourceID:        sourceID,
		SourceCursor:    CursorSourceMessage,
		SourceNumericID: id,
		EventKind:       "channel_message",
		RenderKind:      RenderKindMessage,
		DisplayOnly:     true,
		ChannelID:       &channelID,
		Actor: TimelineActor{
			Type:     "agent",
			Identity: "codex",
		},
		Body:     &body,
		Severity: "info",
		Metadata: json.RawMessage(`{}`),
		SourceRef: TimelineSourceRef{
			Domain: "conversation",
			Table:  "den_channels.channel_messages",
			ID:     sourceID,
		},
	}
}

func testObservationItem(id int64, occurredAt time.Time, debug bool) TimelineItem {
	summary := "working"
	channelID := int64(123)
	renderKind := RenderKindBreadcrumb
	if debug {
		renderKind = RenderKindDiagnostic
	}
	sourceID := sourceIDFromInt(id)
	return TimelineItem{
		TimelineID:      "obs:" + sourceID,
		OccurredAt:      occurredAt,
		SourceDomain:    SourceDomainObservation,
		SourceID:        sourceID,
		SourceCursor:    CursorSourceObservation,
		SourceNumericID: id,
		EventKind:       "agent_activity.v1",
		RenderKind:      renderKind,
		DisplayOnly:     true,
		ChannelID:       &channelID,
		Actor: TimelineActor{
			Type:     "agent",
			Identity: "codex",
		},
		Summary:  &summary,
		Severity: "info",
		Metadata: json.RawMessage(`{}`),
		SourceRef: TimelineSourceRef{
			Domain: "observation",
			Table:  "den_observation.activity_events",
			ID:     sourceID,
		},
	}
}

func testConfig() *Config {
	return &Config{
		BindAddr:     "127.0.0.1:0",
		DatabaseURL:  "postgres://example",
		ServiceToken: "test-token",
		DefaultLimit: 2,
		MaxLimit:     10,
		Stream: StreamConfig{
			PollInterval:      5 * time.Millisecond,
			HeartbeatInterval: 5 * time.Millisecond,
		},
		HTTP: HTTPConfig{ReadHeaderTimeout: time.Second},
	}
}

func fixedClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

func timelineIDs(items []TimelineItemResponse) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.TimelineID)
	}
	return ids
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
