package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	"den-services/shared/identity"
	"den-services/shared/postgres"
)

func TestPostgresStoreRuntimeLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DEN_RUNTIME_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_RUNTIME_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	instanceID := identity.AgentInstanceID("runtime-store-test@" + time.Now().UTC().Format("20060102150405.000000000"))
	instance, err := NewRuntimeInstance(instanceID, "runtime-store-test", "den-srv", nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewRuntimeInstance() error = %v", err)
	}
	registered, err := store.RegisterInstance(ctx, instance)
	if err != nil {
		t.Fatalf("RegisterInstance() error = %v", err)
	}
	if registered.State() != RuntimeStateStarting {
		t.Fatalf("registered state = %s, want %s", registered.State(), RuntimeStateStarting)
	}

	heartbeat, err := store.Heartbeat(ctx, instanceID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if heartbeat.State() != RuntimeStateActive {
		t.Fatalf("heartbeat state = %s, want %s", heartbeat.State(), RuntimeStateActive)
	}

	subscription, err := NewChannelSubscription(instanceID, 42, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewChannelSubscription() error = %v", err)
	}
	created, err := store.CreateSubscription(ctx, subscription)
	if err != nil {
		t.Fatalf("CreateSubscription() error = %v", err)
	}
	if created.SubscriptionID() == 0 {
		t.Fatal("created subscription id is zero")
	}
}
