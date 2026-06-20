package conversation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConversationDisplayNeverWakes(t *testing.T) {
	store := newMemoryConversationStore(t)
	service := newTestConversationService(store)
	channel := mustCreateChannel(t, service)

	message, err := service.AppendMessage(context.Background(), channel.ID, AppendMessageRequest{
		SenderType:     "agent",
		SenderIdentity: "den-mcp-planner",
		Body:           "legacy display row",
		MessageKind:    "agent_text",
		SourceKind:     "legacy_import",
	}, "legacy-message-1")
	if err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	if message.ID == 0 {
		t.Fatal("message ID is zero")
	}
	if len(store.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(store.messages))
	}
}

func TestConversationReplayDoesNotCreateDeliveryIntent(t *testing.T) {
	store := newMemoryConversationStore(t)
	service := newTestConversationService(store)
	channel := mustCreateChannel(t, service)

	first, err := service.AppendMessage(context.Background(), channel.ID, AppendMessageRequest{
		SenderType:     "system",
		SenderIdentity: "legacy-importer",
		Body:           "display-only transcript",
		MessageKind:    "system_event",
		SourceKind:     "legacy_import",
	}, "legacy-replay-1")
	if err != nil {
		t.Fatalf("first AppendMessage() error = %v", err)
	}
	second, err := service.AppendMessage(context.Background(), channel.ID, AppendMessageRequest{
		SenderType:     "system",
		SenderIdentity: "legacy-importer",
		Body:           "display-only transcript",
		MessageKind:    "system_event",
		SourceKind:     "legacy_import",
	}, "legacy-replay-1")
	if err != nil {
		t.Fatalf("second AppendMessage() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate replay ID = %d, want %d", second.ID, first.ID)
	}
	if len(store.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(store.messages))
	}
}

func TestHumanReadCursorDoesNotCreateAgentCursor(t *testing.T) {
	store := newMemoryConversationStore(t)
	service := newTestConversationService(store)
	channel := mustCreateChannel(t, service)
	lastReadMessageID := int64(7)

	cursor, err := service.PutReadCursor(context.Background(), channel.ID, PutReadCursorRequest{
		ReaderType:        "human",
		ReaderIdentity:    "patchfoot",
		LastReadMessageID: &lastReadMessageID,
	})
	if err != nil {
		t.Fatalf("PutReadCursor() error = %v", err)
	}
	if cursor.ReaderType != "human" {
		t.Fatalf("ReaderType = %s, want human", cursor.ReaderType)
	}
	if len(store.cursors) != 1 {
		t.Fatalf("cursors = %d, want 1", len(store.cursors))
	}

	_, err = service.PutReadCursor(context.Background(), channel.ID, PutReadCursorRequest{
		ReaderType:     "agent",
		ReaderIdentity: "den-mcp-runner",
	})
	if !errors.Is(err, ErrInvalidReadCursor) {
		t.Fatalf("PutReadCursor(agent) error = %v, want %v", err, ErrInvalidReadCursor)
	}
	if len(store.cursors) != 1 {
		t.Fatalf("cursors after agent write = %d, want 1", len(store.cursors))
	}
}

func TestMembershipIsNotLiveness(t *testing.T) {
	store := newMemoryConversationStore(t)
	service := newTestConversationService(store)
	channel := mustCreateChannel(t, service)

	membership, err := service.PutMembership(context.Background(), channel.ID, PutMembershipRequest{
		MemberType:        "agent",
		MemberIdentity:    "den-mcp-runner",
		MembershipStatus:  "left",
		WakePolicy:        "never",
		MembershipPurpose: "ordinary",
	})
	if err != nil {
		t.Fatalf("PutMembership() error = %v", err)
	}
	if membership.MembershipStatus != "left" {
		t.Fatalf("MembershipStatus = %s, want left", membership.MembershipStatus)
	}
	if membership.LeftAt == nil {
		t.Fatal("LeftAt is nil")
	}
	if len(store.memberships) != 1 {
		t.Fatalf("memberships = %d, want 1", len(store.memberships))
	}
}

func TestConversationWriteIdempotency(t *testing.T) {
	store := newMemoryConversationStore(t)
	service := newTestConversationService(store)
	channel := mustCreateChannel(t, service)
	req := AppendMessageRequest{
		SenderType:     "human",
		SenderIdentity: "patchfoot",
		Body:           "same message",
		MessageKind:    "human_text",
		SourceKind:     "conversation",
	}

	first, err := service.AppendMessage(context.Background(), channel.ID, req, "idem-1")
	if err != nil {
		t.Fatalf("first AppendMessage() error = %v", err)
	}
	second, err := service.AppendMessage(context.Background(), channel.ID, req, "idem-1")
	if err != nil {
		t.Fatalf("second AppendMessage() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second ID = %d, want %d", second.ID, first.ID)
	}
	if len(store.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(store.messages))
	}
}

func TestConversationWriteRequiresIdempotencyKey(t *testing.T) {
	service := newTestConversationService(newMemoryConversationStore(t))
	channel := mustCreateChannel(t, service)

	_, err := service.AppendMessage(context.Background(), channel.ID, AppendMessageRequest{
		SenderType:     "human",
		SenderIdentity: "patchfoot",
		Body:           "missing key",
		MessageKind:    "human_text",
		SourceKind:     "conversation",
	}, "")
	if !errors.Is(err, ErrMissingDedupeKey) {
		t.Fatalf("AppendMessage() error = %v, want %v", err, ErrMissingDedupeKey)
	}
}

func TestConversationNoActivityEventRegressionRoutes(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{
		"/v1/conversation/channels/1/activity-events",
		"/v1/conversation/lifecycle-events",
		"/v1/conversation/active-work",
		"/v1/conversation/worker-pool/instances",
	} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Authorization", "Bearer conversation-token")
		recorder := httptest.NewRecorder()

		server.Handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusNotFound)
		}
	}
}

func newTestConversationService(store ConversationStore) *Service {
	return NewService(store, fixedClock, &Config{DefaultLimit: 100, MaxLimit: 500})
}

func mustCreateChannel(t *testing.T, service *Service) *Channel {
	t.Helper()
	channel, err := service.CreateChannel(context.Background(), CreateChannelRequest{
		Slug:        "den-services",
		DisplayName: "den-services",
		Kind:        "project_default",
		ProjectID:   ptr("den-services"),
		CreatedBy:   "den-system",
		Visibility:  "normal",
	})
	if err != nil {
		t.Fatalf("CreateChannel() error = %v", err)
	}
	return channel
}

func fixedClock() time.Time {
	return time.Date(2026, 6, 20, 2, 0, 0, 0, time.UTC)
}

func ptr(value string) *string {
	return &value
}
