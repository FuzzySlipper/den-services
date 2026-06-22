package docpublish

import (
	"net/http"

	"den-services/shared/api"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/blog/publications/preview", h.preview)
	mux.HandleFunc("POST /v1/blog/publications", h.publish)
	mux.HandleFunc("GET /v1/blog/publications/{id}", h.get)
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	var req PublicationRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	response, err := h.service.Preview(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	var req PublicationRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	response, err := h.service.Publish(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, response)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	record, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, record)
}
