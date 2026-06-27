package artifacts

import "time"

type CreateArtifactRequest struct {
	ProjectID     string `json:"project_id,omitempty"`
	TaskID        *int64 `json:"task_id,omitempty"`
	ReviewRoundID *int64 `json:"review_round_id,omitempty"`
	FindingID     *int64 `json:"finding_id,omitempty"`
	OwnerKind     string `json:"owner_kind,omitempty"`
	OwnerID       string `json:"owner_id,omitempty"`
	LogicalName   string `json:"logical_name"`
	MimeType      string `json:"mime_type,omitempty"`
	Sensitive     bool   `json:"sensitive,omitempty"`
	CreatedBy     string `json:"created_by,omitempty"`
	Temporary     bool   `json:"temporary,omitempty"`
}

func (r CreateArtifactRequest) Validate() error {
	if r.LogicalName == "" {
		return badRequest(ErrMissingLogicalName)
	}
	return nil
}

type CreateArtifactResponse struct {
	ArtifactMetadataResponse
	ScopedRef string `json:"scoped_ref,omitempty"`
}

type ArtifactMetadataResponse struct {
	ArtifactID     string     `json:"artifact_id"`
	ArtifactRef    string     `json:"artifact_ref"`
	ProjectID      string     `json:"project_id,omitempty"`
	TaskID         *int64     `json:"task_id,omitempty"`
	ReviewRoundID  *int64     `json:"review_round_id,omitempty"`
	FindingID      *int64     `json:"finding_id,omitempty"`
	OwnerKind      string     `json:"owner_kind,omitempty"`
	OwnerID        string     `json:"owner_id,omitempty"`
	LogicalName    string     `json:"logical_name"`
	MimeType       string     `json:"mime_type"`
	ByteCount      int64      `json:"byte_count"`
	SHA256         string     `json:"sha256"`
	Width          *int       `json:"width,omitempty"`
	Height         *int       `json:"height,omitempty"`
	Sensitive      bool       `json:"sensitive"`
	StorageBackend string     `json:"storage_backend"`
	StorageKey     string     `json:"storage_key"`
	CreatedBy      string     `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

func toCreateArtifactResponse(artifact *Artifact) CreateArtifactResponse {
	return CreateArtifactResponse{
		ArtifactMetadataResponse: toArtifactMetadataResponse(artifact),
		ScopedRef:                artifact.ScopedRef(),
	}
}

func toArtifactMetadataResponse(artifact *Artifact) ArtifactMetadataResponse {
	return ArtifactMetadataResponse{
		ArtifactID:     artifact.ArtifactID(),
		ArtifactRef:    artifact.Ref(),
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
		DeletedAt:      artifact.DeletedAt(),
	}
}
