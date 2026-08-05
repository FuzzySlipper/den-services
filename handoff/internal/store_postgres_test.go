package handoff

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestStorePostgresConcurrentReplacement(t *testing.T) {
	databaseURL := os.Getenv("DEN_HANDOFF_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_HANDOFF_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	label := fmt.Sprintf("store-test/%d", time.Now().UnixNano())
	defer func() {
		_, _ = pool.Exec(ctx, "delete from den_handoff.handoffs where label = $1", label)
	}()
	store := NewStore(pool)
	const writers = 8
	var wait sync.WaitGroup
	wait.Add(writers)
	for index := range writers {
		go func() {
			defer wait.Done()
			value, buildErr := NewHandoff(NewHandoffParams{Label: label, BodyMarkdown: fmt.Sprintf("writer-%d", index), UpdatedAt: time.Now().UTC()})
			if buildErr != nil {
				t.Errorf("NewHandoff() error = %v", buildErr)
				return
			}
			if _, setErr := store.Set(ctx, value); setErr != nil {
				t.Errorf("Set() error = %v", setErr)
			}
		}()
	}
	wait.Wait()
	current, err := store.Get(ctx, label)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current.Revision() != writers {
		t.Fatalf("revision = %d, want %d", current.Revision(), writers)
	}
	if current.UpdatedAt().Before(current.CreatedAt()) {
		t.Fatalf("updated_at %s precedes created_at %s", current.UpdatedAt(), current.CreatedAt())
	}
	var historyCount int
	if err := pool.QueryRow(ctx, "select count(*) from den_handoff.handoff_revisions where label = $1", label).Scan(&historyCount); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if historyCount != writers {
		t.Fatalf("history count = %d, want %d", historyCount, writers)
	}
}
