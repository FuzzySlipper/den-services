package delivery

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"den-services/shared/identity"
	"den-services/shared/postgres"
)

func TestPostgresStoreCreateClaimAndLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DEN_DELIVERY_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_DELIVERY_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	intent, err := NewDeliveryIntent(testIdentity(), "op:42:planner:"+time.Now().UTC().Format("20060102150405.000000000"), 5*time.Minute, nil, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewDeliveryIntent() error = %v", err)
	}
	created, err := store.Create(ctx, intent)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	claimed, err := store.ClaimPending(ctx, created.ID(), "claim-token", testIdentity(), time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimPending() error = %v", err)
	}
	if claimed.State() != IntentStateClaimed {
		t.Fatalf("claimed state = %s, want %s", claimed.State(), IntentStateClaimed)
	}
	running, err := store.ReportEvent(ctx, created.ID(), "claim-token", "running", []byte("{}"), time.Now().UTC())
	if err != nil {
		t.Fatalf("ReportEvent(running) error = %v", err)
	}
	if running.State() != IntentStateRunning {
		t.Fatalf("running state = %s, want %s", running.State(), IntentStateRunning)
	}
}

func TestPostgresStoreConcurrentClaim(t *testing.T) {
	databaseURL := os.Getenv("DEN_DELIVERY_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_DELIVERY_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	intent, err := NewDeliveryIntent(testIdentity(), "op:42:planner:concurrent-"+time.Now().UTC().Format("20060102150405.000000000"), 5*time.Minute, nil, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewDeliveryIntent() error = %v", err)
	}
	created, err := store.Create(ctx, intent)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, token := range []string{"claim-one", "claim-two"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			_, err := store.ClaimPending(ctx, created.ID(), token, identity.AgentIdentity{Profile: "planner", InstanceID: identity.AgentInstanceID("planner@" + token)}, time.Now().UTC())
			errs <- err
		}(token)
	}
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		conflicts++
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
}
