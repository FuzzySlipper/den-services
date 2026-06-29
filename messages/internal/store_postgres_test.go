package messages

import (
	"context"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

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
}
