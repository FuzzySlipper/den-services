package observation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLegacyChannelsChatSourceReadsDisplayOnlyConversationMessages(t *testing.T) {
	var nonGetRequests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			nonGetRequests = append(nonGetRequests, r.Method+" "+r.URL.Path)
			http.Error(w, "writes forbidden", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/api/channels":
			_, _ = w.Write([]byte(`[{"id":7}]`))
		case "/api/channels/7/messages":
			_, _ = w.Write([]byte(`[
				{"id":71,"channelId":7,"senderIdentity":"planner","body":"visible chat","messageKind":"agent_text","sourceKind":null,"createdAt":"2026-06-19 12:00:00"},
				{"id":72,"channelId":7,"senderIdentity":"planner","body":"wake row","messageKind":"human_text","sourceKind":"wake_event","createdAt":"2026-06-19 12:01:00"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewLegacyChannelsChatSource(server.URL, time.Second)
	if err != nil {
		t.Fatalf("NewLegacyChannelsChatSource() error = %v", err)
	}
	events, err := source.ListChatEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListChatEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	event := events[0]
	if event.EventID != "conversation:71" || event.SourceDomain != SourceDomainConversation || event.EventType != "message" {
		t.Fatalf("event identity = %s/%s/%s", event.EventID, event.SourceDomain, event.EventType)
	}
	if !event.DisplayOnly {
		t.Fatal("conversation event display_only = false, want true")
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("payload JSON error = %v", err)
	}
	if payload["body"] != "visible chat" || payload["source"] != "legacy_den_channels_http" {
		t.Fatalf("payload = %#v", payload)
	}
	if len(nonGetRequests) != 0 {
		t.Fatalf("chat source made non-GET requests: %s", strings.Join(nonGetRequests, ", "))
	}
}

func TestStoreWithChatSourceDoesNotWriteWhileReadingLane(t *testing.T) {
	base := newFakeObservationStore()
	base.deliveryEvents = []LaneEvent{eventAt("delivery:1", SourceDomainDelivery, fixedTime())}
	chat := &staticChatSource{events: []LaneEvent{eventAt("conversation:1", SourceDomainConversation, fixedTime().Add(time.Minute))}}
	store := NewStoreWithChatSource(base, chat)
	service := NewObservationService(store, fixedClock, 10, 100)

	events, err := service.Lane(context.Background(), "")
	if err != nil {
		t.Fatalf("Lane() error = %v", err)
	}
	if len(events) != 2 || events[0].EventID != "conversation:1" {
		t.Fatalf("events = %#v", events)
	}
	if len(base.appended) != 0 {
		t.Fatalf("lane appended %d events, want 0", len(base.appended))
	}
	if base.activeWorkCalls != 0 {
		t.Fatalf("lane touched active work %d times, want 0", base.activeWorkCalls)
	}
}

type staticChatSource struct {
	events []LaneEvent
}

func (s *staticChatSource) ListChatEvents(_ context.Context, _ int) ([]LaneEvent, error) {
	return append([]LaneEvent(nil), s.events...), nil
}
