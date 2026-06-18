package delivery

import (
	"net/http"

	"den-services/shared/api"
)

type Handler struct {
	service *IntentService
}

func NewHandler(service *IntentService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/delivery/intents", h.createIntent)
	mux.HandleFunc("GET /v1/delivery/intents", h.listIntents)
	mux.HandleFunc("GET /v1/delivery/intents/{id}", h.getIntent)
	mux.HandleFunc("POST /v1/delivery/intents/{id}/claim", h.claimIntent)
	mux.HandleFunc("POST /v1/delivery/intents/{id}/events", h.reportEvent)
}

func (h *Handler) createIntent(w http.ResponseWriter, r *http.Request) {
	var req CreateIntentRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	intent, err := h.service.Create(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toIntentResponse(intent))
}

func (h *Handler) listIntents(w http.ResponseWriter, r *http.Request) {
	var state *IntentState
	if rawState := r.URL.Query().Get("state"); rawState != "" {
		parsed := IntentState(rawState)
		state = &parsed
	}
	intents, err := h.service.List(r.Context(), state)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	responses := make([]IntentResponse, 0, len(intents))
	for _, intent := range intents {
		responses = append(responses, toIntentResponse(intent))
	}
	api.WriteJSON(w, http.StatusOK, responses)
}

func (h *Handler) getIntent(w http.ResponseWriter, r *http.Request) {
	id, err := parseRequiredInt64(r.PathValue("id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidIntent))
		return
	}
	intent, err := h.service.Get(r.Context(), id)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toIntentResponse(intent))
}

func (h *Handler) claimIntent(w http.ResponseWriter, r *http.Request) {
	id, err := parseRequiredInt64(r.PathValue("id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidIntent))
		return
	}
	var req ClaimRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	intent, err := h.service.Claim(r.Context(), id, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toIntentResponse(intent))
}

func (h *Handler) reportEvent(w http.ResponseWriter, r *http.Request) {
	id, err := parseRequiredInt64(r.PathValue("id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidIntent))
		return
	}
	var req LifecycleEventRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	intent, err := h.service.ReportEvent(r.Context(), id, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toIntentResponse(intent))
}
