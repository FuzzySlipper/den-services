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
	mux.HandleFunc("GET /v1/observation/agents/overview", h.agentsOverview)
	mux.HandleFunc("GET /v1/observation/agents/{id}/overview", h.agentOverview)
	mux.HandleFunc("GET /v1/observation/active-work", h.activeWork)
	mux.HandleFunc("GET /v1/observation/assignments/{id}/trace", h.assignmentTrace)
	mux.HandleFunc("GET /v1/observation/assignments/{id}/transcript", h.assignmentTranscript)
	mux.HandleFunc("GET /v1/observation/activity-events", h.activityHistory)
	mux.HandleFunc("GET /v1/observation/activity-events/status", h.activityHistoryStatus)
	mux.HandleFunc("POST /v1/observation/lifecycle-events", h.createLifecycleEvent)
	mux.HandleFunc("POST /v1/observation/activity-events", h.createLifecycleEvent)
}

func (h *Handler) lane(w http.ResponseWriter, r *http.Request) {
	events, err := h.service.Lane(r.Context(), r.URL.Query().Get("limit"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toLaneResponse(events))
}

func (h *Handler) agentsOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.service.AgentsOverview(r.Context(), r.URL.Query().Get("limit"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toAgentsOverviewResponse(overview))
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

func (h *Handler) assignmentTrace(w http.ResponseWriter, r *http.Request) {
	trace, err := h.service.AssignmentTrace(r.Context(), r.PathValue("id"), r.URL.Query().Get("limit"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toAssignmentTraceResponse(trace))
}

func (h *Handler) assignmentTranscript(w http.ResponseWriter, r *http.Request) {
	transcript, err := h.service.AssignmentTranscript(r.Context(), r.PathValue("id"), r.URL.Query().Get("limit"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toAssignmentTranscriptResponse(transcript))
}

func (h *Handler) activityHistory(w http.ResponseWriter, r *http.Request) {
	events, err := h.service.ActivityHistory(
		r.Context(),
		r.URL.Query().Get("limit"),
		r.URL.Query().Get("agent_id"),
		r.URL.Query().Get("assignment_id"),
	)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toLaneResponse(events))
}

func (h *Handler) activityHistoryStatus(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, ActivityHistoryStatusResponse{
		Writable:               true,
		PatchSupported:         false,
		ReadRoute:              "/v1/observation/activity-events",
		WriteRoute:             "/v1/observation/activity-events",
		DroppedLegacySemantics: []string{"mutable_patch_updates", "recent_write_failure_diagnostics"},
		ExecutableStateOwner:   "den-core/delivery/runtime",
		ConversationOwner:      "conversation",
		ObservationProjection:  "display_only",
	})
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
