package artifacts

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"den-services/shared/api"
)

const multipartMemoryLimit = int64(1 << 20)

type ArtifactUseCases interface {
	Create(ctx context.Context, req CreateArtifactRequest, content UploadContent) (*Artifact, error)
	GetMetadata(ctx context.Context, artifactID string) (*Artifact, error)
	ResolveRef(ctx context.Context, rawRef string) (*Artifact, error)
	OpenContent(ctx context.Context, artifactID string) (*ArtifactContent, error)
	Delete(ctx context.Context, artifactID string) (*Artifact, error)
}

type Handler struct {
	service ArtifactUseCases
	config  *Config
}

func NewHandler(service ArtifactUseCases, cfg *Config) *Handler {
	return &Handler{service: service, config: cfg}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/artifacts", h.create)
	mux.HandleFunc("GET /v1/artifacts/resolve", h.resolve)
	mux.HandleFunc("GET /v1/artifacts/{artifact_id}/metadata", h.metadata)
	mux.HandleFunc("GET /v1/artifacts/{artifact_id}/content", h.content)
	mux.HandleFunc("DELETE /v1/artifacts/{artifact_id}", h.delete)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	req, file, fileName, closeFile, err := h.parseMultipartUpload(w, r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	defer closeFile()
	artifact, err := h.service.Create(r.Context(), req, UploadContent{Reader: file, Name: fileName})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toCreateArtifactResponse(artifact))
}

func (h *Handler) metadata(w http.ResponseWriter, r *http.Request) {
	artifact, err := h.service.GetMetadata(r.Context(), r.PathValue("artifact_id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toArtifactMetadataResponse(artifact))
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	artifact, err := h.service.ResolveRef(r.Context(), r.URL.Query().Get("ref"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toArtifactMetadataResponse(artifact))
}

func (h *Handler) content(w http.ResponseWriter, r *http.Request) {
	content, err := h.service.OpenContent(r.Context(), r.PathValue("artifact_id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	defer content.Body.Close()
	w.Header().Set("Content-Type", content.Artifact.MimeType())
	w.Header().Set("Content-Length", strconv.FormatInt(content.Artifact.ByteCount(), 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, content.Body); err != nil {
		return
	}
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	artifact, err := h.service.Delete(r.Context(), r.PathValue("artifact_id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toArtifactMetadataResponse(artifact))
}

func (h *Handler) parseMultipartUpload(w http.ResponseWriter, r *http.Request) (CreateArtifactRequest, io.Reader, string, func(), error) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return CreateArtifactRequest{}, nil, "", func() {}, badRequest(ErrMissingContent)
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.config.Limits.MaxBytesPerArtifact+multipartMemoryLimit)
	if err := r.ParseMultipartForm(multipartMemoryLimit); err != nil {
		return CreateArtifactRequest{}, nil, "", func() {}, badRequest(err)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return CreateArtifactRequest{}, nil, "", func() {}, badRequest(ErrMissingContent)
	}
	req, err := requestFromForm(r)
	if err != nil {
		_ = file.Close()
		return CreateArtifactRequest{}, nil, "", func() {}, err
	}
	return req, file, header.Filename, func() {
		_ = file.Close()
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}, nil
}

func requestFromForm(r *http.Request) (CreateArtifactRequest, error) {
	req := CreateArtifactRequest{
		ProjectID:   strings.TrimSpace(r.FormValue("project_id")),
		OwnerKind:   strings.TrimSpace(r.FormValue("owner_kind")),
		OwnerID:     strings.TrimSpace(r.FormValue("owner_id")),
		LogicalName: strings.TrimSpace(r.FormValue("logical_name")),
		MimeType:    strings.TrimSpace(r.FormValue("mime_type")),
		Sensitive:   r.FormValue("sensitive") == "true",
		CreatedBy:   strings.TrimSpace(r.FormValue("created_by")),
		Temporary:   r.FormValue("temporary") == "true",
	}
	taskID, err := optionalInt64FormValue(r, "task_id")
	if err != nil {
		return CreateArtifactRequest{}, err
	}
	reviewRoundID, err := optionalInt64FormValue(r, "review_round_id")
	if err != nil {
		return CreateArtifactRequest{}, err
	}
	findingID, err := optionalInt64FormValue(r, "finding_id")
	if err != nil {
		return CreateArtifactRequest{}, err
	}
	req.TaskID = taskID
	req.ReviewRoundID = reviewRoundID
	req.FindingID = findingID
	return req, nil
}

func optionalInt64FormValue(r *http.Request, key string) (*int64, error) {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, badRequest(err)
	}
	return &parsed, nil
}
