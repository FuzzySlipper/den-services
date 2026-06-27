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
	MimeType      string `json:"mime_type"`
	Sensitive     bool   `json:"sensitive,omitempty"`
	CreatedBy     string `json:"created_by,omitempty"`
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

type NotImplementedResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
