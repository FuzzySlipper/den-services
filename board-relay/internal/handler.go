package boardrelay

import (
	"context"
	"net/http"

	"den-services/shared/api"
)

type RelayUseCases interface {
	Sync(ctx context.Context, projectID string) (SyncReceipt, error)
	SetVisibility(ctx context.Context, request VisibilityRequest) error
}

type Handler struct{ service RelayUseCases }

func NewHandler(service RelayUseCases) *Handler { return &Handler{service: service} }

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects/{project_id}/board/github-sync", h.sync)
	mux.HandleFunc("PATCH /v1/board/github-visibility", h.setVisibility)
}

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	receipt, err := h.service.Sync(r.Context(), r.PathValue("project_id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, receipt)
}

func (h *Handler) setVisibility(w http.ResponseWriter, r *http.Request) {
	var request VisibilityRequest
	if err := api.DecodeJSON(r, &request); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.SetVisibility(r.Context(), request); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
