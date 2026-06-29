package projects

import (
	"context"
	"net/http"

	"den-services/shared/api"
)

type ScopeUseCases interface {
	CreateProject(ctx context.Context, req CreateProjectRequest) (*Scope, error)
	CreateSpace(ctx context.Context, req CreateSpaceRequest) (*Scope, error)
	GetScope(ctx context.Context, id string) (*Scope, error)
	ListProjects(ctx context.Context, includeHidden bool, includeArchived bool) ([]*Scope, error)
	ListSpaces(ctx context.Context, kind string, includeHidden bool, includeArchived bool) ([]*Scope, error)
	UpdateProject(ctx context.Context, id string, req UpdateProjectRequest) (*Scope, error)
	UpdateVisibility(ctx context.Context, id string, visibility string) (*Scope, error)
	ArchiveSpace(ctx context.Context, id string) (*Scope, error)
	AssertWritable(ctx context.Context, id string, allowArchived bool) (*Scope, error)
}

type Handler struct {
	service ScopeUseCases
}

func NewHandler(service ScopeUseCases) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects", h.createProject)
	mux.HandleFunc("GET /v1/projects", h.listProjects)
	mux.HandleFunc("GET /v1/projects/{id}", h.getProject)
	mux.HandleFunc("PATCH /v1/projects/{id}", h.updateProject)

	mux.HandleFunc("POST /v1/spaces", h.createSpace)
	mux.HandleFunc("GET /v1/spaces", h.listSpaces)
	mux.HandleFunc("GET /v1/spaces/{id}", h.getSpace)
	mux.HandleFunc("PATCH /v1/spaces/{id}/visibility", h.updateSpaceVisibility)
	mux.HandleFunc("POST /v1/spaces/{id}/archive", h.archiveSpace)

	mux.HandleFunc("GET /v1/scopes/{id}", h.getScope)
	mux.HandleFunc("POST /v1/scopes/{id}/assert-writable", h.assertWritable)
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	scope, err := h.service.CreateProject(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toScopeResponse(scope))
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	scopes, err := h.service.ListProjects(r.Context(), boolQuery(r, "include_hidden"), boolQuery(r, "include_archived"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toScopeResponses(scopes))
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request) {
	h.writeScope(w, r, r.PathValue("id"))
}

func (h *Handler) updateProject(w http.ResponseWriter, r *http.Request) {
	var req UpdateProjectRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	scope, err := h.service.UpdateProject(r.Context(), r.PathValue("id"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toScopeResponse(scope))
}

func (h *Handler) createSpace(w http.ResponseWriter, r *http.Request) {
	var req CreateSpaceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	scope, err := h.service.CreateSpace(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toScopeResponse(scope))
}

func (h *Handler) listSpaces(w http.ResponseWriter, r *http.Request) {
	scopes, err := h.service.ListSpaces(
		r.Context(),
		r.URL.Query().Get("kind"),
		boolQuery(r, "include_hidden"),
		boolQuery(r, "include_archived"),
	)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toScopeResponses(scopes))
}

func (h *Handler) getSpace(w http.ResponseWriter, r *http.Request) {
	h.writeScope(w, r, r.PathValue("id"))
}

func (h *Handler) updateSpaceVisibility(w http.ResponseWriter, r *http.Request) {
	var req UpdateVisibilityRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	scope, err := h.service.UpdateVisibility(r.Context(), r.PathValue("id"), req.Visibility)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toScopeResponse(scope))
}

func (h *Handler) archiveSpace(w http.ResponseWriter, r *http.Request) {
	scope, err := h.service.ArchiveSpace(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toScopeResponse(scope))
}

func (h *Handler) getScope(w http.ResponseWriter, r *http.Request) {
	h.writeScope(w, r, r.PathValue("id"))
}

func (h *Handler) assertWritable(w http.ResponseWriter, r *http.Request) {
	var req AssertWritableRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := api.DecodeJSON(r, &req); err != nil {
			api.WriteServiceError(w, err)
			return
		}
	}
	scope, err := h.service.AssertWritable(r.Context(), r.PathValue("id"), req.AllowArchivedScope)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, AssertWritableResponse{
		ID:         scope.ID(),
		Writable:   true,
		Visibility: scope.Visibility(),
	})
}

func (h *Handler) writeScope(w http.ResponseWriter, r *http.Request, id string) {
	scope, err := h.service.GetScope(r.Context(), id)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toScopeResponse(scope))
}

func boolQuery(r *http.Request, key string) bool {
	return r.URL.Query().Get(key) == "true"
}
