package artifacts

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"testing"
	"time"
)

func TestArtifactServiceUploadReadAndTombstone(t *testing.T) {
	store := newMemoryArtifactStore()
	blobs := newMemoryBlobStore()
	service := NewArtifactService(store, blobs, testServiceConfig(), fixedClock)

	taskID := int64(3476)
	artifact, err := service.Create(context.Background(), CreateArtifactRequest{
		ProjectID:   "den-services",
		TaskID:      &taskID,
		LogicalName: "pixel.png",
		Sensitive:   true,
		CreatedBy:   "codex",
	}, UploadContent{Reader: bytes.NewReader(tinyPNG(t)), Name: "pixel.png"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if artifact.ByteCount() != int64(len(tinyPNG(t))) {
		t.Fatalf("ByteCount() = %d", artifact.ByteCount())
	}
	if artifact.MimeType() != "image/png" {
		t.Fatalf("MimeType() = %s", artifact.MimeType())
	}
	if artifact.Width() == nil || *artifact.Width() != 1 || artifact.Height() == nil || *artifact.Height() != 1 {
		t.Fatalf("dimensions = %v x %v", artifact.Width(), artifact.Height())
	}
	if artifact.Ref() != "den-artifact://"+artifact.ArtifactID() {
		t.Fatalf("Ref() = %s", artifact.Ref())
	}
	if artifact.ScopedRef() == "" {
		t.Fatal("ScopedRef() is empty")
	}

	metadata, err := service.GetMetadata(context.Background(), artifact.ArtifactID())
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if metadata.SHA256() != artifact.SHA256() {
		t.Fatalf("metadata SHA = %s, want %s", metadata.SHA256(), artifact.SHA256())
	}

	content, err := service.OpenContent(context.Background(), artifact.ArtifactID())
	if err != nil {
		t.Fatalf("OpenContent() error = %v", err)
	}
	defer content.Body.Close()
	readBack, err := io.ReadAll(content.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(readBack, tinyPNG(t)) {
		t.Fatal("read content does not match upload")
	}

	deleted, err := service.Delete(context.Background(), artifact.ArtifactID())
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deleted.DeletedAt() == nil {
		t.Fatal("DeletedAt() is nil")
	}
	_, err = service.GetMetadata(context.Background(), artifact.ArtifactID())
	if !errors.Is(err, ErrArtifactDeleted) {
		t.Fatalf("GetMetadata() after delete error = %v, want %v", err, ErrArtifactDeleted)
	}
}

func TestArtifactServiceDuplicateHashReusesBlobKey(t *testing.T) {
	store := newMemoryArtifactStore()
	blobs := newMemoryBlobStore()
	service := NewArtifactService(store, blobs, testServiceConfig(), fixedClock)

	first, err := service.Create(context.Background(), CreateArtifactRequest{LogicalName: "first.png"}, UploadContent{Reader: bytes.NewReader(tinyPNG(t))})
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	second, err := service.Create(context.Background(), CreateArtifactRequest{LogicalName: "second.png"}, UploadContent{Reader: bytes.NewReader(tinyPNG(t))})
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if first.ArtifactID() == second.ArtifactID() {
		t.Fatal("duplicate upload reused metadata artifact id")
	}
	if first.StorageKey() != second.StorageKey() {
		t.Fatalf("StorageKey() = %s and %s, want same", first.StorageKey(), second.StorageKey())
	}
	if blobs.saveCount(first.StorageKey()) != 1 {
		t.Fatalf("blob save count = %d, want 1", blobs.saveCount(first.StorageKey()))
	}
}

func TestArtifactServiceRejectsNonImageContent(t *testing.T) {
	service := NewArtifactService(newMemoryArtifactStore(), newMemoryBlobStore(), testServiceConfig(), fixedClock)

	_, err := service.Create(context.Background(), CreateArtifactRequest{LogicalName: "note.txt"}, UploadContent{Reader: bytes.NewBufferString("hello")})
	if !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("Create() error = %v, want %v", err, ErrUnsupportedMediaType)
	}
}

func testServiceConfig() *Config {
	return &Config{
		Storage: StorageConfig{
			Backend:   storageBackendFilesystem,
			RootPath:  "/var/lib/den/artifacts",
			KeyPrefix: "sha256",
		},
		Limits: LimitConfig{
			MaxBytesPerArtifact: 1024 * 1024,
			MaxPixelsPerImage:   16,
		},
		Retention: RetentionConfig{
			TemporaryTTL: time.Hour,
		},
	}
}

func fixedClock() time.Time {
	return time.Date(2026, 6, 27, 1, 0, 0, 0, time.UTC)
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decoding png: %v", err)
	}
	return data
}

type memoryArtifactStore struct {
	artifacts map[string]*Artifact
}

func newMemoryArtifactStore() *memoryArtifactStore {
	return &memoryArtifactStore{artifacts: make(map[string]*Artifact)}
}

func (s *memoryArtifactStore) CreateArtifact(_ context.Context, artifact *Artifact) (*Artifact, error) {
	s.artifacts[artifact.ArtifactID()] = artifact
	return artifact, nil
}

func (s *memoryArtifactStore) GetArtifact(_ context.Context, artifactID string) (*Artifact, error) {
	artifact, ok := s.artifacts[artifactID]
	if !ok {
		return nil, notFound(artifactID)
	}
	return artifact, nil
}

func (s *memoryArtifactStore) TombstoneArtifact(_ context.Context, artifactID string, reason string, at time.Time) (*Artifact, error) {
	artifact, ok := s.artifacts[artifactID]
	if !ok {
		return nil, notFound(artifactID)
	}
	deletedAt := at.UTC()
	deletionReason := reason
	tombstoned, err := rehydrateArtifact(NewArtifactParams{
		ArtifactID:     artifact.ArtifactID(),
		ProjectID:      artifact.ProjectID(),
		TaskID:         artifact.TaskID(),
		ReviewRoundID:  artifact.ReviewRoundID(),
		FindingID:      artifact.FindingID(),
		OwnerKind:      artifact.OwnerKind(),
		OwnerID:        artifact.OwnerID(),
		LogicalName:    artifact.LogicalName(),
		MimeType:       artifact.MimeType(),
		ByteCount:      artifact.ByteCount(),
		SHA256:         artifact.SHA256(),
		Width:          artifact.Width(),
		Height:         artifact.Height(),
		Sensitive:      artifact.Sensitive(),
		StorageBackend: artifact.StorageBackend(),
		StorageKey:     artifact.StorageKey(),
		CreatedBy:      artifact.CreatedBy(),
		CreatedAt:      artifact.CreatedAt(),
		ExpiresAt:      artifact.ExpiresAt(),
	}, deletedAt, &deletedAt, &deletionReason)
	if err != nil {
		return nil, err
	}
	s.artifacts[artifactID] = tombstoned
	return tombstoned, nil
}

type memoryBlobStore struct {
	blobs map[string][]byte
	saves map[string]int
}

func newMemoryBlobStore() *memoryBlobStore {
	return &memoryBlobStore{
		blobs: make(map[string][]byte),
		saves: make(map[string]int),
	}
}

func (s *memoryBlobStore) Save(_ context.Context, storageKey string, data []byte) error {
	if _, ok := s.blobs[storageKey]; ok {
		return nil
	}
	s.blobs[storageKey] = append([]byte(nil), data...)
	s.saves[storageKey]++
	return nil
}

func (s *memoryBlobStore) Open(_ context.Context, storageKey string) (io.ReadCloser, error) {
	data, ok := s.blobs[storageKey]
	if !ok {
		return nil, notFound(storageKey)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *memoryBlobStore) saveCount(storageKey string) int {
	return s.saves[storageKey]
}

var (
	_ ArtifactStore = (*memoryArtifactStore)(nil)
	_ BlobStore     = (*memoryBlobStore)(nil)
)
