package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"
)

type ArtifactStore interface {
	CreateArtifact(ctx context.Context, artifact *Artifact) (*Artifact, error)
	GetArtifact(ctx context.Context, artifactID string) (*Artifact, error)
	GetArtifactByScope(ctx context.Context, projectID string, taskID int64, logicalName string) (*Artifact, error)
	TombstoneArtifact(ctx context.Context, artifactID string, reason string, at time.Time) (*Artifact, error)
}

type ArtifactService struct {
	store  ArtifactStore
	blobs  BlobStore
	config *Config
	clock  func() time.Time
}

type UploadContent struct {
	Reader io.Reader
	Name   string
}

type ArtifactContent struct {
	Artifact *Artifact
	Body     io.ReadCloser
}

func NewArtifactService(store ArtifactStore, blobs BlobStore, cfg *Config, clock func() time.Time) *ArtifactService {
	return &ArtifactService{
		store:  store,
		blobs:  blobs,
		config: cfg,
		clock:  clock,
	}
}

func (s *ArtifactService) Create(ctx context.Context, req CreateArtifactRequest, content UploadContent) (*Artifact, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	data, err := s.readLimited(content.Reader)
	if err != nil {
		return nil, err
	}
	mimeType := normalizeMime(req.MimeType, data)
	if !validImageMime(mimeType) {
		return nil, unsupportedMediaType(fmt.Errorf("%w: %s", ErrUnsupportedMediaType, mimeType))
	}
	width, height, err := imageDimensions(data)
	if err != nil {
		return nil, unsupportedMediaType(fmt.Errorf("%w: image dimensions unavailable", ErrUnsupportedMediaType))
	}
	if int64(width*height) > s.config.Limits.MaxPixelsPerImage {
		return nil, NewServiceError(ErrArtifactTooManyPixels, "artifact_too_many_pixels", http.StatusRequestEntityTooLarge)
	}
	hashBytes := sha256.Sum256(data)
	hash := hex.EncodeToString(hashBytes[:])
	storageKey := StorageKeyForSHA256(hash)
	if err := s.blobs.Save(ctx, storageKey, data); err != nil {
		return nil, err
	}
	artifactID, err := NewArtifactID()
	if err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	artifact, err := NewArtifact(NewArtifactParams{
		ArtifactID:     artifactID,
		ProjectID:      req.ProjectID,
		TaskID:         req.TaskID,
		ReviewRoundID:  req.ReviewRoundID,
		FindingID:      req.FindingID,
		OwnerKind:      req.OwnerKind,
		OwnerID:        req.OwnerID,
		LogicalName:    logicalName(req.LogicalName, content.Name),
		MimeType:       mimeType,
		ByteCount:      int64(len(data)),
		SHA256:         hash,
		Width:          &width,
		Height:         &height,
		Sensitive:      req.Sensitive,
		StorageBackend: storageBackendFilesystem,
		StorageKey:     storageKey,
		CreatedBy:      req.CreatedBy,
		CreatedAt:      now,
		ExpiresAt:      expiresAt(req.Temporary, s.config.Retention, now),
	})
	if err != nil {
		return nil, err
	}
	created, err := s.store.CreateArtifact(ctx, artifact)
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *ArtifactService) GetMetadata(ctx context.Context, artifactID string) (*Artifact, error) {
	artifact, err := s.store.GetArtifact(ctx, strings.TrimSpace(artifactID))
	if err != nil {
		return nil, err
	}
	if artifact.DeletedAt() != nil {
		return nil, deleted(artifactID)
	}
	return artifact, nil
}

func (s *ArtifactService) ResolveRef(ctx context.Context, rawRef string) (*Artifact, error) {
	scope, err := ParseArtifactRef(rawRef)
	if err != nil {
		return nil, badRequest(err)
	}
	if scope.ArtifactID != "" {
		return s.GetMetadata(ctx, scope.ArtifactID)
	}
	artifact, err := s.store.GetArtifactByScope(ctx, scope.ProjectID, scope.TaskID, scope.LogicalName)
	if err != nil {
		return nil, err
	}
	if artifact.DeletedAt() != nil {
		return nil, deleted(artifact.ArtifactID())
	}
	return artifact, nil
}

func (s *ArtifactService) OpenContent(ctx context.Context, artifactID string) (*ArtifactContent, error) {
	artifact, err := s.GetMetadata(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	body, err := s.blobs.Open(ctx, artifact.StorageKey())
	if err != nil {
		return nil, err
	}
	return &ArtifactContent{Artifact: artifact, Body: body}, nil
}

func (s *ArtifactService) Delete(ctx context.Context, artifactID string) (*Artifact, error) {
	artifact, err := s.store.TombstoneArtifact(ctx, strings.TrimSpace(artifactID), "api_delete", s.clock().UTC())
	if err != nil {
		return nil, err
	}
	return artifact, nil
}

func (s *ArtifactService) readLimited(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, badRequest(ErrMissingContent)
	}
	limited := io.LimitReader(reader, s.config.Limits.MaxBytesPerArtifact+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("reading artifact content: %w", err)
	}
	if len(data) == 0 {
		return nil, badRequest(ErrMissingContent)
	}
	if int64(len(data)) > s.config.Limits.MaxBytesPerArtifact {
		return nil, NewServiceError(ErrArtifactTooLarge, "artifact_too_large", http.StatusRequestEntityTooLarge)
	}
	return data, nil
}

func normalizeMime(requested string, data []byte) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested
	}
	return http.DetectContentType(data)
}

func imageDimensions(data []byte) (int, int, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return config.Width, config.Height, nil
}

func logicalName(requested string, fallback string) string {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "artifact"
}

func expiresAt(temporary bool, cfg RetentionConfig, now time.Time) *time.Time {
	if temporary {
		value := now.Add(cfg.TemporaryTTL).UTC()
		return &value
	}
	if cfg.DefaultTTL > 0 {
		value := now.Add(cfg.DefaultTTL).UTC()
		return &value
	}
	return nil
}
