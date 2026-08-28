package guidance

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestStorePostgresKnowledgeBindingsCRUD(t *testing.T) {
	databaseURL := os.Getenv("DEN_GUIDANCE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_GUIDANCE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)
	binding, err := NewKnowledgeBinding(KnowledgeBindingParams{TargetRef: "guidance-postgres-binding", ScopeKind: ScopeCapability, ScopeRef: "store-test", ReadPolicy: ReadPolicyOnDemand, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateBinding(ctx, binding)
	if err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}
	defer store.DeleteBinding(ctx, created.ID)
	duplicate, err := store.CreateBinding(ctx, binding)
	if !errors.Is(err, ErrDuplicateBinding) || duplicate != nil {
		t.Fatalf("CreateBinding duplicate = %#v, %v; want ErrDuplicateBinding", duplicate, err)
	}
	if _, err := store.GetBinding(ctx, created.ID); err != nil {
		t.Fatalf("GetBinding: %v", err)
	}
	listed, err := store.ListBindings(ctx, BindingListQuery{Limit: 50})
	if err != nil || len(listed.Bindings) == 0 {
		t.Fatalf("ListBindings: %#v, %v", listed, err)
	}
	unbounded, err := store.ListBindings(ctx, BindingListQuery{})
	if err != nil || len(unbounded.Bindings) == 0 {
		t.Fatalf("ListBindings(unbounded): %#v, %v", unbounded, err)
	}
	created.ReadPolicy = ReadPolicyMustRead
	if _, err := store.UpdateBinding(ctx, created); err != nil {
		t.Fatalf("UpdateBinding: %v", err)
	}
	if deleted, err := store.DeleteBinding(ctx, created.ID); err != nil || !deleted {
		t.Fatalf("DeleteBinding: %t, %v", deleted, err)
	}
}
