package artifacts

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	storageBackendFilesystem = "filesystem"
	artifactRefScheme        = "den-artifact://"
)

var (
	ErrArtifactNotFound      = errors.New("artifact not found")           //nolint:gochecknoglobals
	ErrArtifactDeleted       = errors.New("artifact deleted")             //nolint:gochecknoglobals
	ErrInvalidArtifact       = errors.New("invalid artifact")             //nolint:gochecknoglobals
	ErrMissingLogicalName    = errors.New("logical_name is required")     //nolint:gochecknoglobals
	ErrMissingContent        = errors.New("artifact content is required") //nolint:gochecknoglobals
	ErrUnsupportedMediaType  = errors.New("unsupported media type")       //nolint:gochecknoglobals
	ErrArtifactTooLarge      = errors.New("artifact is too large")        //nolint:gochecknoglobals
	ErrArtifactTooManyPixels = errors.New("artifact has too many pixels") //nolint:gochecknoglobals
)

type ServiceError struct {
	err    error
	code   string
	status int
}

func NewServiceError(err error, code string, status int) *ServiceError {
	return &ServiceError{err: err, code: code, status: status}
}

func (e *ServiceError) Error() string {
	return e.err.Error()
}

func (e *ServiceError) Unwrap() error {
	return e.err
}

func (e *ServiceError) Code() string {
	return e.code
}

func (e *ServiceError) HTTPStatus() int {
	return e.status
}

func notFound(id string) error {
	return NewServiceError(fmt.Errorf("%w: %s", ErrArtifactNotFound, id), "artifact_not_found", http.StatusNotFound)
}

func deleted(id string) error {
	return NewServiceError(fmt.Errorf("%w: %s", ErrArtifactDeleted, id), "artifact_deleted", http.StatusNotFound)
}

func badRequest(err error) error {
	return NewServiceError(err, "bad_request", http.StatusBadRequest)
}

func unsupportedMediaType(err error) error {
	return NewServiceError(err, "unsupported_media_type", http.StatusUnsupportedMediaType)
}

type Artifact struct {
	artifactID     string
	projectID      string
	taskID         *int64
	reviewRoundID  *int64
	findingID      *int64
	ownerKind      string
	ownerID        string
	logicalName    string
	mimeType       string
	byteCount      int64
	sha256         string
	width          *int
	height         *int
	sensitive      bool
	storageBackend string
	storageKey     string
	createdBy      string
	createdAt      time.Time
	updatedAt      time.Time
	expiresAt      *time.Time
	deletedAt      *time.Time
	deletionReason *string
}

type NewArtifactParams struct {
	ArtifactID     string
	ProjectID      string
	TaskID         *int64
	ReviewRoundID  *int64
	FindingID      *int64
	OwnerKind      string
	OwnerID        string
	LogicalName    string
	MimeType       string
	ByteCount      int64
	SHA256         string
	Width          *int
	Height         *int
	Sensitive      bool
	StorageBackend string
	StorageKey     string
	CreatedBy      string
	CreatedAt      time.Time
	ExpiresAt      *time.Time
}

type ArtifactRefScope struct {
	ArtifactID  string
	ProjectID   string
	TaskID      int64
	LogicalName string
}

func ParseArtifactRef(rawRef string) (ArtifactRefScope, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawRef))
	if err != nil {
		return ArtifactRefScope{}, fmt.Errorf("%w: parsing artifact ref: %v", ErrInvalidArtifact, err)
	}
	if parsed.Scheme != "den-artifact" {
		return ArtifactRefScope{}, fmt.Errorf("%w: artifact ref scheme must be den-artifact", ErrInvalidArtifact)
	}
	if parsed.Host == "" {
		return ArtifactRefScope{}, fmt.Errorf("%w: artifact ref host is required", ErrInvalidArtifact)
	}
	if strings.HasPrefix(parsed.Host, "art_") && parsed.Path == "" {
		return ArtifactRefScope{ArtifactID: parsed.Host}, nil
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 4 || segments[0] != "tasks" || segments[2] != "artifacts" {
		return ArtifactRefScope{}, fmt.Errorf("%w: scoped artifact ref must be den-artifact://<project>/tasks/<task_id>/artifacts/<logical_name>", ErrInvalidArtifact)
	}
	taskID, err := strconv.ParseInt(segments[1], 10, 64)
	if err != nil {
		return ArtifactRefScope{}, fmt.Errorf("%w: task id must be an integer", ErrInvalidArtifact)
	}
	logicalName, err := url.PathUnescape(strings.Join(segments[3:], "/"))
	if err != nil {
		return ArtifactRefScope{}, fmt.Errorf("%w: logical name is invalid", ErrInvalidArtifact)
	}
	if strings.TrimSpace(logicalName) == "" {
		return ArtifactRefScope{}, fmt.Errorf("%w: logical name is required", ErrInvalidArtifact)
	}
	return ArtifactRefScope{
		ProjectID:   parsed.Host,
		TaskID:      taskID,
		LogicalName: path.Base(logicalName),
	}, nil
}

func NewArtifact(params NewArtifactParams) (*Artifact, error) {
	if strings.TrimSpace(params.ArtifactID) == "" {
		return nil, fmt.Errorf("%w: artifact_id is required", ErrInvalidArtifact)
	}
	if strings.TrimSpace(params.LogicalName) == "" {
		return nil, ErrMissingLogicalName
	}
	if !validImageMime(params.MimeType) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedMediaType, params.MimeType)
	}
	if params.ByteCount <= 0 {
		return nil, fmt.Errorf("%w: byte_count must be positive", ErrInvalidArtifact)
	}
	if !validSHA256(params.SHA256) {
		return nil, fmt.Errorf("%w: sha256 must be 64 lowercase hex characters", ErrInvalidArtifact)
	}
	if strings.TrimSpace(params.StorageBackend) == "" || strings.TrimSpace(params.StorageKey) == "" {
		return nil, fmt.Errorf("%w: storage backend and key are required", ErrInvalidArtifact)
	}
	if params.Width != nil && *params.Width <= 0 {
		return nil, fmt.Errorf("%w: width must be positive", ErrInvalidArtifact)
	}
	if params.Height != nil && *params.Height <= 0 {
		return nil, fmt.Errorf("%w: height must be positive", ErrInvalidArtifact)
	}
	return &Artifact{
		artifactID:     strings.TrimSpace(params.ArtifactID),
		projectID:      strings.TrimSpace(params.ProjectID),
		taskID:         params.TaskID,
		reviewRoundID:  params.ReviewRoundID,
		findingID:      params.FindingID,
		ownerKind:      strings.TrimSpace(params.OwnerKind),
		ownerID:        strings.TrimSpace(params.OwnerID),
		logicalName:    strings.TrimSpace(params.LogicalName),
		mimeType:       strings.TrimSpace(params.MimeType),
		byteCount:      params.ByteCount,
		sha256:         strings.TrimSpace(params.SHA256),
		width:          params.Width,
		height:         params.Height,
		sensitive:      params.Sensitive,
		storageBackend: strings.TrimSpace(params.StorageBackend),
		storageKey:     strings.TrimSpace(params.StorageKey),
		createdBy:      strings.TrimSpace(params.CreatedBy),
		createdAt:      params.CreatedAt.UTC(),
		updatedAt:      params.CreatedAt.UTC(),
		expiresAt:      params.ExpiresAt,
	}, nil
}

func rehydrateArtifact(params NewArtifactParams, updatedAt time.Time, deletedAt *time.Time, deletionReason *string) (*Artifact, error) {
	artifact, err := NewArtifact(params)
	if err != nil {
		return nil, err
	}
	artifact.updatedAt = updatedAt.UTC()
	artifact.deletedAt = deletedAt
	artifact.deletionReason = deletionReason
	return artifact, nil
}

func NewArtifactID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generating artifact id: %w", err)
	}
	return "art_" + hex.EncodeToString(bytes[:]), nil
}

func StorageKeyForSHA256(hash string) string {
	return path.Join("sha256", hash[0:2], hash[2:4], hash)
}

func (a *Artifact) ArtifactID() string {
	return a.artifactID
}

func (a *Artifact) Ref() string {
	return artifactRefScheme + a.artifactID
}

func (a *Artifact) ScopedRef() string {
	if a.projectID == "" || a.taskID == nil {
		return ""
	}
	cleanName := path.Base(a.logicalName)
	if cleanName == "." || cleanName == "/" {
		cleanName = a.artifactID
	}
	return fmt.Sprintf("%s%s/tasks/%d/artifacts/%s", artifactRefScheme, a.projectID, *a.taskID, cleanName)
}

func (a *Artifact) ProjectID() string {
	return a.projectID
}

func (a *Artifact) TaskID() *int64 {
	return a.taskID
}

func (a *Artifact) ReviewRoundID() *int64 {
	return a.reviewRoundID
}

func (a *Artifact) FindingID() *int64 {
	return a.findingID
}

func (a *Artifact) OwnerKind() string {
	return a.ownerKind
}

func (a *Artifact) OwnerID() string {
	return a.ownerID
}

func (a *Artifact) LogicalName() string {
	return a.logicalName
}

func (a *Artifact) MimeType() string {
	return a.mimeType
}

func (a *Artifact) ByteCount() int64 {
	return a.byteCount
}

func (a *Artifact) SHA256() string {
	return a.sha256
}

func (a *Artifact) Width() *int {
	return a.width
}

func (a *Artifact) Height() *int {
	return a.height
}

func (a *Artifact) Sensitive() bool {
	return a.sensitive
}

func (a *Artifact) StorageBackend() string {
	return a.storageBackend
}

func (a *Artifact) StorageKey() string {
	return a.storageKey
}

func (a *Artifact) CreatedBy() string {
	return a.createdBy
}

func (a *Artifact) CreatedAt() time.Time {
	return a.createdAt
}

func (a *Artifact) UpdatedAt() time.Time {
	return a.updatedAt
}

func (a *Artifact) ExpiresAt() *time.Time {
	return a.expiresAt
}

func (a *Artifact) DeletedAt() *time.Time {
	return a.deletedAt
}

func (a *Artifact) DeletionReason() *string {
	return a.deletionReason
}

func validImageMime(mimeType string) bool {
	return strings.HasPrefix(strings.TrimSpace(mimeType), "image/")
}

func validSHA256(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, char := range hash {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
