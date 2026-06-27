package artifacts

import (
	"context"
	"os"
	"testing"
	"time"

	"den-services/shared/postgres"
)

func TestPostgresStoreCreateGetAndTombstone(t *testing.T) {
	databaseURL := os.Getenv("DEN_ARTIFACTS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEN_ARTIFACTS_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer pool.Close()

	store := NewStore(pool)
	artifactID := "art_store_test_" + time.Now().UTC().Format("20060102150405000000000")
	artifact, err := NewArtifact(NewArtifactParams{
		ArtifactID:     artifactID,
		LogicalName:    "pixel.png",
		MimeType:       "image/png",
		ByteCount:      68,
		SHA256:         "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		StorageBackend: storageBackendFilesystem,
		StorageKey:     "sha256/01/23/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:      fixedClock(),
	})
	if err != nil {
		t.Fatalf("NewArtifact() error = %v", err)
	}
	created, err := store.CreateArtifact(ctx, artifact)
	if err != nil {
		t.Fatalf("CreateArtifact() error = %v", err)
	}
	read, err := store.GetArtifact(ctx, created.ArtifactID())
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	if read.SHA256() != artifact.SHA256() {
		t.Fatalf("SHA256() = %s", read.SHA256())
	}
	deleted, err := store.TombstoneArtifact(ctx, created.ArtifactID(), "test", fixedClock())
	if err != nil {
		t.Fatalf("TombstoneArtifact() error = %v", err)
	}
	if deleted.DeletedAt() == nil {
		t.Fatal("DeletedAt() is nil")
	}
}
