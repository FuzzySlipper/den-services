package projects

import (
	"context"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestPostgresStoreScopeLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DEN_PROJECTS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_PROJECTS_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	id := "projects-store-test-" + time.Now().UTC().Format("20060102150405000000000")
	scope, err := NewScope(NewScopeParams{
		ID:            id,
		Name:          "Projects Store Test",
		Kind:          KindAssistant,
		Visibility:    VisibilityNormal,
		Owner:         "codex",
		RootPath:      "/tmp/projects-store-test",
		RepositoryURL: "https://example.test/projects-store-test.git",
		Description:   "store smoke",
		SettingsJSON:  []byte(`{"test":true}`),
		CreatedAt:     fixedClock(),
		UpdatedAt:     fixedClock(),
	})
	if err != nil {
		t.Fatalf("NewScope() error = %v", err)
	}
	created, err := store.CreateScope(ctx, scope)
	if err != nil {
		t.Fatalf("CreateScope() error = %v", err)
	}
	read, err := store.GetScope(ctx, created.ID())
	if err != nil {
		t.Fatalf("GetScope() error = %v", err)
	}
	if read.Kind() != KindAssistant || read.RootPath() != "/tmp/projects-store-test" || read.RepositoryURL() != "https://example.test/projects-store-test.git" {
		t.Fatalf("read scope = %+v", read)
	}
	empty := ""
	updated, err := store.UpdateScope(ctx, created.ID(), ScopePatch{
		RootPath:      &empty,
		RepositoryURL: &empty,
	}, fixedClock().Add(time.Minute))
	if err != nil {
		t.Fatalf("UpdateScope() error = %v", err)
	}
	if updated.RootPath() != "" || updated.RepositoryURL() != "" {
		t.Fatalf("updated paths = root_path:%q repository_url:%q", updated.RootPath(), updated.RepositoryURL())
	}
	archived, err := store.UpdateVisibility(ctx, created.ID(), VisibilityArchived, fixedClock().Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateVisibility() error = %v", err)
	}
	if archived.Visibility() != VisibilityArchived {
		t.Fatalf("archived visibility = %s", archived.Visibility())
	}
	deleted, err := store.DeleteScope(ctx, created.ID())
	if err != nil {
		t.Fatalf("DeleteScope() error = %v", err)
	}
	if deleted.ID() != created.ID() {
		t.Fatalf("deleted id = %s, want %s", deleted.ID(), created.ID())
	}
	if _, err := store.GetScope(ctx, created.ID()); err == nil {
		t.Fatal("GetScope(deleted) error = nil")
	}
}
