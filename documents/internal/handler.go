package documents

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"den-services/shared/api"
)

type DocumentUseCases interface {
	StoreDocument(ctx context.Context, projectID string, req StoreDocumentRequest) (*Document, error)
	GetDocument(ctx context.Context, projectID string, slug string) (*Document, error)
	ListDocuments(ctx context.Context, query ListDocumentsQuery) ([]DocumentSummary, error)
	SearchDocuments(ctx context.Context, query string, projectID string) ([]DocumentSearchResult, error)
	QueryArchivedDocuments(ctx context.Context, query string, projectID string, docType string, tags []string) ([]DocumentSummary, []DocumentSearchResult, error)
	DeleteDocument(ctx context.Context, projectID string, slug string) (bool, error)
	UpdateVisibility(ctx context.Context, projectID string, slug string, visibility string) (*Document, error)
	ArchivePreflight(ctx context.Context, projectID string, slug string) (ArchivePreflightResult, error)
	GetDocumentDiscussion(ctx context.Context, projectID string, slug string, createIfMissing bool, includeResolved bool, anchor string) (DiscussionDetail, error)
	CommentOnDocument(ctx context.Context, projectID string, slug string, req CommentOnDocumentRequest) (*DiscussionComment, *DiscussionThread, error)
	CreateDiscussionComment(ctx context.Context, threadID int64, req CreateCommentRequest) (*DiscussionComment, error)
	CreateDiscussionThread(ctx context.Context, req CreateThreadRequest) (*DiscussionThread, error)
	ListDiscussionThreads(ctx context.Context, query ListThreadsQuery) ([]DiscussionThread, error)
	GetDiscussionThread(ctx context.Context, id int64, includeComments bool) (*DiscussionThread, []DiscussionComment, error)
	UpdateDiscussionThread(ctx context.Context, id int64, req UpdateThreadRequest) (*DiscussionThread, error)
}

type Handler struct {
	service DocumentUseCases
}

func NewHandler(service DocumentUseCases) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects/{project_id}/documents", h.storeDocument)
	mux.HandleFunc("GET /v1/projects/{project_id}/documents", h.listProjectDocuments)
	mux.HandleFunc("GET /v1/projects/{project_id}/documents/search", h.searchProjectDocuments)
	mux.HandleFunc("GET /v1/projects/{project_id}/documents/archived", h.queryProjectArchived)
	mux.HandleFunc("GET /v1/projects/{project_id}/documents/archived/search", h.searchProjectArchived)
	mux.HandleFunc("GET /v1/projects/{project_id}/documents/{slug}", h.getDocument)
	mux.HandleFunc("DELETE /v1/projects/{project_id}/documents/{slug}", h.deleteDocument)
	mux.HandleFunc("PATCH /v1/projects/{project_id}/documents/{slug}/visibility", h.updateVisibility)
	mux.HandleFunc("POST /v1/projects/{project_id}/documents/{slug}/archive-preflight", h.archivePreflight)
	mux.HandleFunc("GET /v1/projects/{project_id}/documents/{slug}/discussion", h.getDocumentDiscussion)
	mux.HandleFunc("POST /v1/projects/{project_id}/documents/{slug}/discussion/comments", h.commentOnDocument)
	mux.HandleFunc("GET /v1/projects/{project_id}/documents/{slug}/discussion/threads", h.listDocumentThreads)

	mux.HandleFunc("GET /v1/documents", h.listDocuments)
	mux.HandleFunc("GET /v1/documents/search", h.searchDocuments)
	mux.HandleFunc("GET /v1/documents/archived", h.queryArchived)
	mux.HandleFunc("GET /v1/documents/archived/search", h.searchArchived)

	mux.HandleFunc("POST /v1/discussion-threads", h.createDiscussionThread)
	mux.HandleFunc("GET /v1/discussion-threads", h.listDiscussionThreads)
	mux.HandleFunc("GET /v1/discussion-threads/{thread_id}", h.getDiscussionThread)
	mux.HandleFunc("PATCH /v1/discussion-threads/{thread_id}", h.updateDiscussionThread)
	mux.HandleFunc("POST /v1/discussion-threads/{thread_id}/comments", h.createDiscussionComment)
}

func (h *Handler) storeDocument(w http.ResponseWriter, r *http.Request) {
	var req StoreDocumentRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	doc, err := h.service.StoreDocument(r.Context(), r.PathValue("project_id"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toDocumentResponse(doc))
}

func (h *Handler) getDocument(w http.ResponseWriter, r *http.Request) {
	doc, err := h.service.GetDocument(r.Context(), r.PathValue("project_id"), r.PathValue("slug"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toDocumentResponse(doc))
}

func (h *Handler) listProjectDocuments(w http.ResponseWriter, r *http.Request) {
	query, err := documentQueryFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	query.ProjectID = r.PathValue("project_id")
	h.writeDocuments(w, r, query)
}

func (h *Handler) listDocuments(w http.ResponseWriter, r *http.Request) {
	query, err := documentQueryFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	h.writeDocuments(w, r, query)
}

func (h *Handler) writeDocuments(w http.ResponseWriter, r *http.Request, query ListDocumentsQuery) {
	docs, err := h.service.ListDocuments(r.Context(), query)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toSummaryResponses(docs))
}

func (h *Handler) searchProjectDocuments(w http.ResponseWriter, r *http.Request) {
	h.writeSearch(w, r, r.PathValue("project_id"), VisibilityNormal)
}

func (h *Handler) searchDocuments(w http.ResponseWriter, r *http.Request) {
	h.writeSearch(w, r, "", VisibilityNormal)
}

func (h *Handler) writeSearch(w http.ResponseWriter, r *http.Request, projectID string, visibility string) {
	var results []DocumentSearchResult
	var err error
	if visibility == VisibilityArchived {
		_, results, err = h.service.QueryArchivedDocuments(r.Context(), r.URL.Query().Get("query"), projectID, "", nil)
	} else {
		results, err = h.service.SearchDocuments(r.Context(), r.URL.Query().Get("query"), projectID)
	}
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toSearchResponses(results))
}

func (h *Handler) queryProjectArchived(w http.ResponseWriter, r *http.Request) {
	h.writeArchived(w, r, r.PathValue("project_id"), false)
}

func (h *Handler) queryArchived(w http.ResponseWriter, r *http.Request) {
	h.writeArchived(w, r, "", false)
}

func (h *Handler) searchProjectArchived(w http.ResponseWriter, r *http.Request) {
	h.writeArchived(w, r, r.PathValue("project_id"), true)
}

func (h *Handler) searchArchived(w http.ResponseWriter, r *http.Request) {
	h.writeArchived(w, r, "", true)
}

func (h *Handler) writeArchived(w http.ResponseWriter, r *http.Request, projectID string, requireQuery bool) {
	query := r.URL.Query().Get("query")
	if requireQuery && strings.TrimSpace(query) == "" {
		api.WriteServiceError(w, validationFailed(ErrSearchQueryEmpty))
		return
	}
	docs, results, err := h.service.QueryArchivedDocuments(r.Context(), query, projectID, r.URL.Query().Get("doc_type"), splitCSV(r.URL.Query().Get("tags")))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, ArchivedDocumentsResponse{Documents: toSummaryResponses(docs), Results: toSearchResponses(results)})
}

func (h *Handler) deleteDocument(w http.ResponseWriter, r *http.Request) {
	deleted, err := h.service.DeleteDocument(r.Context(), r.PathValue("project_id"), r.PathValue("slug"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if !deleted {
		api.WriteServiceError(w, documentNotFound(r.PathValue("project_id"), r.PathValue("slug")))
		return
	}
	api.WriteJSON(w, http.StatusOK, SimpleMessageResponse{Message: "Document deleted."})
}

func (h *Handler) updateVisibility(w http.ResponseWriter, r *http.Request) {
	var req UpdateVisibilityRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	doc, err := h.service.UpdateVisibility(r.Context(), r.PathValue("project_id"), r.PathValue("slug"), req.Visibility)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toDocumentResponse(doc))
}

func (h *Handler) archivePreflight(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ArchivePreflight(r.Context(), r.PathValue("project_id"), r.PathValue("slug"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toArchivePreflightResponse(result))
}

func (h *Handler) getDocumentDiscussion(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	detail, err := h.service.GetDocumentDiscussion(
		r.Context(),
		r.PathValue("project_id"),
		r.PathValue("slug"),
		boolQuery(r, "create_if_missing"),
		boolQuery(r, "include_resolved"),
		query.Get("anchor"),
	)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toDiscussionDetailResponse(detail))
}

func (h *Handler) commentOnDocument(w http.ResponseWriter, r *http.Request) {
	var req CommentOnDocumentRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	comment, _, err := h.service.CommentOnDocument(r.Context(), r.PathValue("project_id"), r.PathValue("slug"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toCommentResponse(comment))
}

func (h *Handler) listDocumentThreads(w http.ResponseWriter, r *http.Request) {
	query := ListThreadsQuery{
		TargetType:      TargetTypeDocument,
		TargetProjectID: r.PathValue("project_id"),
		TargetSlug:      r.PathValue("slug"),
		Status:          r.URL.Query().Get("status"),
	}
	h.writeThreads(w, r, query)
}

func (h *Handler) createDiscussionThread(w http.ResponseWriter, r *http.Request) {
	var req CreateThreadRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	thread, err := h.service.CreateDiscussionThread(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toThreadResponse(*thread))
}

func (h *Handler) listDiscussionThreads(w http.ResponseWriter, r *http.Request) {
	limit, err := optionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	query := ListThreadsQuery{
		TargetType:      r.URL.Query().Get("target_type"),
		TargetProjectID: r.URL.Query().Get("target_project_id"),
		TargetSlug:      r.URL.Query().Get("target_slug"),
		Status:          r.URL.Query().Get("status"),
		Limit:           limit,
	}
	h.writeThreads(w, r, query)
}

func (h *Handler) writeThreads(w http.ResponseWriter, r *http.Request, query ListThreadsQuery) {
	threads, err := h.service.ListDiscussionThreads(r.Context(), query)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toThreadResponses(threads))
}

func (h *Handler) getDiscussionThread(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "thread_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	includeComments := r.URL.Query().Get("include_comments") != "false"
	thread, comments, err := h.service.GetDiscussionThread(r.Context(), id, includeComments)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, struct {
		Thread   DiscussionThreadResponse    `json:"thread"`
		Comments []DiscussionCommentResponse `json:"comments,omitempty"`
	}{Thread: toThreadResponse(*thread), Comments: toCommentResponses(comments)})
}

func (h *Handler) updateDiscussionThread(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "thread_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	var req UpdateThreadRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	thread, err := h.service.UpdateDiscussionThread(r.Context(), id, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toThreadResponse(*thread))
}

func (h *Handler) createDiscussionComment(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "thread_id")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	var req CreateCommentRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	comment, err := h.service.CreateDiscussionComment(r.Context(), id, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toCommentResponse(comment))
}

func documentQueryFromRequest(r *http.Request) (ListDocumentsQuery, error) {
	query := r.URL.Query()
	result := ListDocumentsQuery{
		DocType: query.Get("doc_type"),
		Tags:    splitCSV(query.Get("tags")),
	}
	if visibility := strings.TrimSpace(query.Get("visibility")); visibility != "" {
		result.Visibility = visibility
		result.HasVisibility = true
	}
	return result, nil
}

func pathInt64(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, badRequest(err)
	}
	return id, nil
}

func optionalInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, badRequest(err)
	}
	return parsed, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func boolQuery(r *http.Request, key string) bool {
	return r.URL.Query().Get(key) == "true"
}
