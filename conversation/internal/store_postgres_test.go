package conversation

import (
	"context"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestPostgresStorePilotLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DEN_CHANNELS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_CHANNELS_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	service := newTestConversationService(store)
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	channel, err := service.CreateChannel(ctx, CreateChannelRequest{
		Slug:        "store-test-" + suffix,
		DisplayName: "store test",
		Kind:        "test",
		CreatedBy:   "store-test",
		Visibility:  "normal",
	})
	if err != nil {
		t.Fatalf("CreateChannel() error = %v", err)
	}
	message, err := service.AppendMessage(ctx, channel.ID, AppendMessageRequest{
		SenderType:     "human",
		SenderIdentity: "store-test",
		Body:           "hello",
		MessageKind:    "human_text",
		SourceKind:     "conversation",
	}, "store-test-message-"+suffix)
	if err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	replayed, err := service.AppendMessage(ctx, channel.ID, AppendMessageRequest{
		SenderType:     "human",
		SenderIdentity: "store-test",
		Body:           "hello",
		MessageKind:    "human_text",
		SourceKind:     "conversation",
	}, "store-test-message-"+suffix)
	if err != nil {
		t.Fatalf("AppendMessage(replay) error = %v", err)
	}
	if replayed.ID != message.ID {
		t.Fatalf("replay ID = %d, want %d", replayed.ID, message.ID)
	}
	if _, err := service.PutMembership(ctx, channel.ID, PutMembershipRequest{
		MemberType:        "human",
		MemberIdentity:    "store-test",
		MembershipStatus:  "active",
		WakePolicy:        "never",
		MembershipPurpose: "ordinary",
	}); err != nil {
		t.Fatalf("PutMembership() error = %v", err)
	}
	if _, err := service.AddReaction(ctx, message.ID, AddReactionRequest{
		ReactorType:     "human",
		ReactorIdentity: "store-test",
		Reaction:        "ack",
	}); err != nil {
		t.Fatalf("AddReaction() error = %v", err)
	}
	lastReadMessageID := message.ID
	if _, err := service.PutReadCursor(ctx, channel.ID, PutReadCursorRequest{
		ReaderType:        "human",
		ReaderIdentity:    "store-test",
		LastReadMessageID: &lastReadMessageID,
	}); err != nil {
		t.Fatalf("PutReadCursor() error = %v", err)
	}
}
