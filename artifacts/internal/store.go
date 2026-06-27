package artifacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) CreateArtifact(ctx context.Context, artifact *Artifact) (*Artifact, error) {
	created, err := scanArtifact(s.pool.QueryRow(ctx, createArtifactSQL,
		artifact.ArtifactID(),
		emptyToNil(artifact.ProjectID()),
		artifact.TaskID(),
		artifact.ReviewRoundID(),
		artifact.FindingID(),
		emptyToNil(artifact.OwnerKind()),
		emptyToNil(artifact.OwnerID()),
		artifact.LogicalName(),
		artifact.MimeType(),
		artifact.ByteCount(),
		artifact.SHA256(),
		artifact.Width(),
		artifact.Height(),
		artifact.Sensitive(),
		artifact.StorageBackend(),
		artifact.StorageKey(),
		emptyToNil(artifact.CreatedBy()),
		artifact.CreatedAt(),
		artifact.UpdatedAt(),
		artifact.ExpiresAt(),
	))
	if err != nil {
		return nil, fmt.Errorf("creating artifact metadata: %w", err)
	}
	return created, nil
}

func (s *Store) GetArtifact(ctx context.Context, artifactID string) (*Artifact, error) {
	artifact, err := scanArtifact(s.pool.QueryRow(ctx, getArtifactSQL, artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(artifactID)
	}
	if err != nil {
		return nil, fmt.Errorf("getting artifact %s: %w", artifactID, err)
	}
	return artifact, nil
}

func (s *Store) TombstoneArtifact(ctx context.Context, artifactID string, reason string, at time.Time) (*Artifact, error) {
	artifact, err := scanArtifact(s.pool.QueryRow(ctx, tombstoneArtifactSQL, at, reason, artifactID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(artifactID)
	}
	if err != nil {
		return nil, fmt.Errorf("tombstoning artifact %s: %w", artifactID, err)
	}
	return artifact, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanArtifact(row rowScanner) (*Artifact, error) {
	var artifactID string
	var projectID *string
	var taskID *int64
	var reviewRoundID *int64
	var findingID *int64
	var ownerKind *string
	var ownerID *string
	var logicalName string
	var mimeType string
	var byteCount int64
	var sha256Value string
	var width *int
	var height *int
	var sensitive bool
	var storageBackend string
	var storageKey string
	var createdBy *string
	var createdAt time.Time
	var updatedAt time.Time
	var expiresAt *time.Time
	var deletedAt *time.Time
	var deletionReason *string
	if err := row.Scan(
		&artifactID,
		&projectID,
		&taskID,
		&reviewRoundID,
		&findingID,
		&ownerKind,
		&ownerID,
		&logicalName,
		&mimeType,
		&byteCount,
		&sha256Value,
		&width,
		&height,
		&sensitive,
		&storageBackend,
		&storageKey,
		&createdBy,
		&createdAt,
		&updatedAt,
		&expiresAt,
		&deletedAt,
		&deletionReason,
	); err != nil {
		return nil, err
	}
	return rehydrateArtifact(NewArtifactParams{
		ArtifactID:     artifactID,
		ProjectID:      stringValue(projectID),
		TaskID:         taskID,
		ReviewRoundID:  reviewRoundID,
		FindingID:      findingID,
		OwnerKind:      stringValue(ownerKind),
		OwnerID:        stringValue(ownerID),
		LogicalName:    logicalName,
		MimeType:       mimeType,
		ByteCount:      byteCount,
		SHA256:         sha256Value,
		Width:          width,
		Height:         height,
		Sensitive:      sensitive,
		StorageBackend: storageBackend,
		StorageKey:     storageKey,
		CreatedBy:      stringValue(createdBy),
		CreatedAt:      createdAt,
		ExpiresAt:      expiresAt,
	}, updatedAt, deletedAt, deletionReason)
}

func emptyToNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

const artifactColumns = `
artifact_id, project_id, task_id, review_round_id, finding_id, owner_kind, owner_id,
logical_name, mime_type, byte_count, sha256, width, height, sensitive, storage_backend,
storage_key, created_by, created_at, updated_at, expires_at, deleted_at, deletion_reason`

const createArtifactSQL = `
insert into den_artifacts.artifacts (
    artifact_id, project_id, task_id, review_round_id, finding_id, owner_kind, owner_id,
    logical_name, mime_type, byte_count, sha256, width, height, sensitive, storage_backend,
    storage_key, created_by, created_at, updated_at, expires_at
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
returning ` + artifactColumns

const getArtifactSQL = `
select ` + artifactColumns + `
from den_artifacts.artifacts
where artifact_id = $1`

const tombstoneArtifactSQL = `
update den_artifacts.artifacts
set deleted_at = coalesce(deleted_at, $1),
    deletion_reason = coalesce(deletion_reason, $2),
    updated_at = $1
where artifact_id = $3
returning ` + artifactColumns
