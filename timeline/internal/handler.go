package timeline

import (
	"net/http"
	"strconv"

	"den-services/shared/api"
)

type Handler struct {
	service *Service
	config  *Config
}

func NewHandler(service *Service, config *Config) *Handler {
	return &Handler{
		service: service,
		config:  config,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/timeline/channels/{channel_id}/items", h.channelItems)
	mux.HandleFunc("GET /v1/timeline/projects/{project_id}/items", h.projectItems)
	mux.HandleFunc("GET /v1/timeline/channels/{channel_id}/stream", h.channelStream)
	mux.HandleFunc("GET /v1/timeline/projects/{project_id}/stream", h.projectStream)
}

func (h *Handler) channelItems(w http.ResponseWriter, r *http.Request) {
	scope, err := channelScopeFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	h.writeItems(w, r, scope)
}

func (h *Handler) projectItems(w http.ResponseWriter, r *http.Request) {
	scope, err := projectScopeFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	h.writeItems(w, r, scope)
}

func (h *Handler) channelStream(w http.ResponseWriter, r *http.Request) {
	scope, err := channelScopeFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	h.writeStream(w, r, scope)
}

func (h *Handler) projectStream(w http.ResponseWriter, r *http.Request) {
	scope, err := projectScopeFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	h.writeStream(w, r, scope)
}

func (h *Handler) writeItems(w http.ResponseWriter, r *http.Request, scope TimelineScope) {
	limit, err := h.parseLimit(r)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	response, err := h.service.ListItems(r.Context(), scope, r.URL.Query().Get("after"), limit, includeDebug(r))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) writeStream(w http.ResponseWriter, r *http.Request, scope TimelineScope) {
	limit, err := h.parseLimit(r)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	streamTimeline(w, r, h.service, h.config, streamQuery{
		Scope:        scope,
		After:        r.URL.Query().Get("after"),
		Limit:        limit,
		IncludeDebug: includeDebug(r),
	})
}

func (h *Handler) parseLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return h.config.DefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrInvalidLimit
	}
	if limit <= 0 || limit > h.config.MaxLimit {
		return 0, ErrInvalidLimit
	}
	return limit, nil
}

func channelScopeFromRequest(r *http.Request) (TimelineScope, error) {
	channelID, err := parseRequiredInt64(r.PathValue("channel_id"))
	if err != nil {
		return TimelineScope{}, err
	}
	return NewChannelScope(channelID)
}

func projectScopeFromRequest(r *http.Request) (TimelineScope, error) {
	return NewProjectScope(r.PathValue("project_id"))
}

func includeDebug(r *http.Request) bool {
	return r.URL.Query().Get("include_debug") == "true"
}
