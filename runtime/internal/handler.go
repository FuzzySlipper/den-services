package runtime

import (
	"net/http"

	"den-services/shared/api"
	"den-services/shared/identity"
)

type Handler struct {
	service *RuntimeService
}

func NewHandler(service *RuntimeService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/runtime/instances", h.registerInstance)
	mux.HandleFunc("GET /v1/runtime/instances", h.listInstances)
	mux.HandleFunc("GET /v1/runtime/instances/{id}", h.getInstance)
	mux.HandleFunc("POST /v1/runtime/instances/{id}/heartbeat", h.heartbeat)
	mux.HandleFunc("POST /v1/runtime/subscriptions", h.createSubscription)
	mux.HandleFunc("GET /v1/runtime/subscriptions/{id}/stream", h.stream)
}

func (h *Handler) registerInstance(w http.ResponseWriter, r *http.Request) {
	var req RegisterInstanceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	instance, err := h.service.Register(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toRuntimeInstanceResponse(instance))
}

func (h *Handler) listInstances(w http.ResponseWriter, r *http.Request) {
	var state *RuntimeState
	if rawState := r.URL.Query().Get("state"); rawState != "" {
		parsed := RuntimeState(rawState)
		state = &parsed
	}
	instances, err := h.service.List(r.Context(), state)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	responses := make([]RuntimeInstanceResponse, 0, len(instances))
	for _, instance := range instances {
		responses = append(responses, toRuntimeInstanceResponse(instance))
	}
	api.WriteJSON(w, http.StatusOK, responses)
}

func (h *Handler) getInstance(w http.ResponseWriter, r *http.Request) {
	instance, err := h.service.Get(r.Context(), identity.AgentInstanceID(r.PathValue("id")))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toRuntimeInstanceResponse(instance))
}

func (h *Handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	instance, err := h.service.Heartbeat(r.Context(), identity.AgentInstanceID(r.PathValue("id")))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toRuntimeInstanceResponse(instance))
}

func (h *Handler) createSubscription(w http.ResponseWriter, r *http.Request) {
	var req CreateSubscriptionRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	subscription, err := h.service.CreateSubscription(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toSubscriptionResponse(subscription))
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	subscriptionID, err := parseRequiredInt64(r.PathValue("id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidSubscription))
		return
	}
	after := int64(0)
	if rawAfter := r.URL.Query().Get("after"); rawAfter != "" {
		after, err = parseRequiredInt64(rawAfter)
		if err != nil {
			api.WriteServiceError(w, badRequest(ErrInvalidSubscription))
			return
		}
	}
	subscription, err := h.service.Stream(r.Context(), subscriptionID, after)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, StreamResponse{
		SubscriptionID: subscription.SubscriptionID(),
		After:          after,
		CursorPosition: subscription.CursorPosition(),
		Events:         []StreamEvent{},
	})
}
