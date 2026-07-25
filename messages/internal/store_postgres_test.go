package messages

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestReviewPacketIdempotencyPreservesAppendOnlyRole(t *testing.T) {
	if !strings.Contains(createMessageSQL, "do nothing") || strings.Contains(createMessageSQL, "do update") {
		t.Fatalf("review packet conflict handling must not require table UPDATE: %s", createMessageSQL)
	}
	if !strings.Contains(getMessageByReviewPacketSQL, "review_packet_id") {
		t.Fatalf("review packet conflict readback is missing: %s", getMessageByReviewPacketSQL)
	}
}

func TestStorePostgresRepresentativeFlow(t *testing.T) {
	databaseURL := os.Getenv("DEN_MESSAGES_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_MESSAGES_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)
	now := time.Now().UTC()
	taskID := int64(123)
	message, err := NewMessage(NewMessageParams{ProjectID: "store-smoke", TaskID: &taskID, Sender: "pi", Content: "store smoke", Intent: IntentGeneral, CreatedAt: now})
	if err != nil {
		t.Fatalf("NewMessage() error = %v", err)
	}
	created, err := store.CreateMessage(ctx, message)
	if err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}
	found, err := store.GetMessage(ctx, created.ID())
	if err != nil {
		t.Fatalf("GetMessage() error = %v", err)
	}
	if found.ID() != created.ID() || found.ProjectID() != "store-smoke" {
		t.Fatalf("found = %#v", found)
	}
	if err := store.MarkRead(ctx, "agent", []int64{created.ID()}); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}
	unread, err := store.UnreadAfterCursor(ctx, "store-smoke", "agent", 0, 10)
	if err != nil {
		t.Fatalf("UnreadAfterCursor() error = %v", err)
	}
	for _, item := range unread {
		if item.ID() == created.ID() {
			t.Fatalf("read message returned as unread: %#v", unread)
		}
	}

	packetMetadata := map[string]any{"review_packet_id": 987654321}
	firstPacket, err := NewMessage(NewMessageParams{
		ProjectID: "store-smoke", TaskID: &taskID, Sender: "reviewer", Content: "canonical packet",
		Intent: IntentReviewFeedback, Metadata: packetMetadata, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewMessage(first packet) error = %v", err)
	}
	firstPacket, err = store.CreateMessage(ctx, firstPacket)
	if err != nil {
		t.Fatalf("CreateMessage(first packet) error = %v", err)
	}
	retryPacket, err := NewMessage(NewMessageParams{
		ProjectID: "store-smoke", TaskID: &taskID, Sender: "reviewer", Content: "ambiguous-response retry",
		Intent: IntentReviewFeedback, Metadata: packetMetadata, CreatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("NewMessage(retry packet) error = %v", err)
	}
	retryPacket, err = store.CreateMessage(ctx, retryPacket)
	if err != nil {
		t.Fatalf("CreateMessage(retry packet) error = %v", err)
	}
	if retryPacket.ID() != firstPacket.ID() || retryPacket.Content() != firstPacket.Content() {
		t.Fatalf("idempotent retry = %#v, first = %#v", retryPacket, firstPacket)
	}
}
