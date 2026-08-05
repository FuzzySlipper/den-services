package handoff

import (
	"context"
	"errors"
	"net/http"

	"den-services/shared/api"
)

type HandoffUseCases interface {
	Set(ctx context.Context, request SetHandoffRequest) (*Handoff, error)
	Get(ctx context.Context, label string) (*Handoff, error)
}

type Handler struct{ service HandoffUseCases }

func NewHandler(service HandoffUseCases) *Handler { return &Handler{service: service} }

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/handoffs", h.set)
	mux.HandleFunc("GET /v1/handoffs", h.get)
}

func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	var request SetHandoffRequest
	if err := api.DecodeJSON(r, &request); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	value, err := h.service.Set(r.Context(), request)
	if err != nil {
		api.WriteServiceError(w, mapStoreError(err, request.Label))
		return
	}
	api.WriteJSON(w, http.StatusOK, toSetHandoffResponse(value))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	label := r.URL.Query().Get("label")
	value, err := h.service.Get(r.Context(), label)
	if err != nil {
		api.WriteServiceError(w, mapStoreError(err, label))
		return
	}
	api.WriteJSON(w, http.StatusOK, toHandoffResponse(value))
}

func mapStoreError(err error, label string) error {
	if errors.Is(err, ErrHandoffNotFound) {
		return handoffNotFound(label)
	}
	return err
}
