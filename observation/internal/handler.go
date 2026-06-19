package observation

import (
	"net/http"

	"den-services/shared/api"
)

type Handler struct {
	service *ObservationService
}

func NewHandler(service *ObservationService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/observation/lane", h.lane)
	mux.HandleFunc("GET /v1/observation/agents/{id}/overview", h.agentOverview)
	mux.HandleFunc("GET /v1/observation/active-work", h.activeWork)
	mux.HandleFunc("POST /v1/observation/lifecycle-events", h.createLifecycleEvent)
}

func (h *Handler) lane(w http.ResponseWriter, r *http.Request) {
	events, err := h.service.Lane(r.Context(), r.URL.Query().Get("limit"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toLaneResponse(events))
}

func (h *Handler) agentOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.service.AgentOverview(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toAgentOverviewResponse(overview))
}

func (h *Handler) activeWork(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ActiveWork(r.Context())
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toActiveWorkResponse(items))
}

func (h *Handler) createLifecycleEvent(w http.ResponseWriter, r *http.Request) {
	var req CreateLifecycleEventRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	event, err := h.service.AppendLifecycleEvent(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toActivityEventResponse(event))
}
