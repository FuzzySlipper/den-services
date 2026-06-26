package handler

import (
	"net/http"

	"den-services/shared/api"

	"den-services/visual-inspect/internal/schema"
	"den-services/visual-inspect/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(service *service.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/visual-inspect/evaluate", h.evaluate)
	mux.HandleFunc("POST /v1/visual-inspect/describe", h.describe)
}

func (h *Handler) evaluate(w http.ResponseWriter, r *http.Request) {
	var req schema.EvaluateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	response, err := h.service.Evaluate(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) describe(w http.ResponseWriter, r *http.Request) {
	var req schema.DescribeRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	response, err := h.service.Describe(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, response)
}
