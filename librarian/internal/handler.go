package librarian

import (
	"context"
	"net/http"

	"den-services/shared/api"
)

type LibrarianUseCases interface {
	Query(ctx context.Context, projectID string, req QueryRequest) (QueryResponse, error)
}

type Handler struct {
	service LibrarianUseCases
}

func NewHandler(service LibrarianUseCases) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects/{project_id}/librarian/query", h.queryProject)
	mux.HandleFunc("POST /v1/librarian/query", h.query)
}

func (h *Handler) queryProject(w http.ResponseWriter, r *http.Request) {
	h.writeQuery(w, r, r.PathValue("project_id"))
}

func (h *Handler) query(w http.ResponseWriter, r *http.Request) {
	h.writeQuery(w, r, "")
}

func (h *Handler) writeQuery(w http.ResponseWriter, r *http.Request, projectID string) {
	var req QueryRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	result, err := h.service.Query(r.Context(), projectID, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, result)
}
