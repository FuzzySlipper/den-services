package knowledge

import (
	"context"
	"net/http"
	"strconv"

	"den-services/shared/api"
)

type KnowledgeUseCases interface {
	CheckStore(ctx context.Context) error
	StoreEntry(ctx context.Context, req StoreEntryRequest) (*Entry, error)
	GetEntry(ctx context.Context, slug string, includeArchived bool) (*Entry, error)
	ListEntries(ctx context.Context, query ListQuery) ([]EntrySummary, error)
	SearchEntries(ctx context.Context, query SearchQuery) ([]SearchResult, error)
	Guide(ctx context.Context, query GuideQuery) (GuideResponse, error)
	ListRevisions(ctx context.Context, slug string) ([]RevisionSummary, error)
}

type Handler struct {
	service KnowledgeUseCases
}

func NewHandler(service KnowledgeUseCases) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/knowledge/entries", h.storeEntry)
	mux.HandleFunc("GET /v1/knowledge/entries", h.listEntries)
	mux.HandleFunc("GET /v1/knowledge/entries/{slug}", h.getEntry)
	mux.HandleFunc("GET /v1/knowledge/entries/{slug}/revisions", h.listRevisions)
	mux.HandleFunc("POST /v1/knowledge/search", h.searchEntries)
	mux.HandleFunc("POST /v1/knowledge/guide", h.guide)
}

func (h *Handler) storeEntry(w http.ResponseWriter, r *http.Request) {
	var req StoreEntryRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	entry, err := h.service.StoreEntry(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toEntryResponse(entry))
}

func (h *Handler) getEntry(w http.ResponseWriter, r *http.Request) {
	entry, err := h.service.GetEntry(r.Context(), r.PathValue("slug"), boolQuery(r, "include_archived"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toEntryResponse(entry))
}

func (h *Handler) listEntries(w http.ResponseWriter, r *http.Request) {
	query, err := listQueryFromRequest(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	entries, err := h.service.ListEntries(r.Context(), query)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, ListResponse{Items: toSummaryResponses(entries), Count: len(entries)})
}

func (h *Handler) searchEntries(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	results, err := h.service.SearchEntries(r.Context(), SearchQuery{
		Query:             req.Query,
		RequiredTags:      req.RequiredTags,
		AnyTags:           req.AnyTags,
		Kind:              req.Kind,
		Audience:          req.Audience,
		Status:            req.Status,
		IncludeDeprecated: req.IncludeDeprecated,
		IncludeUnreviewed: req.IncludeUnreviewed,
		IncludeArchived:   req.IncludeArchived,
		Limit:             req.Limit,
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, SearchResultsResponse{Results: toSearchResponses(results), Count: len(results)})
}

func (h *Handler) guide(w http.ResponseWriter, r *http.Request) {
	var req GuideRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	includeFollowUps := true
	if req.IncludeFollowUps != nil {
		includeFollowUps = *req.IncludeFollowUps
	}
	result, err := h.service.Guide(r.Context(), GuideQuery{
		Question:          req.Question,
		RequiredTags:      req.RequiredTags,
		AnyTags:           req.AnyTags,
		Audience:          req.Audience,
		ContextBudget:     req.ContextBudget,
		IncludeFollowUps:  includeFollowUps,
		IncludeDeprecated: req.IncludeDeprecated,
		IncludeUnreviewed: req.IncludeUnreviewed,
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) listRevisions(w http.ResponseWriter, r *http.Request) {
	revisions, err := h.service.ListRevisions(r.Context(), r.PathValue("slug"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, RevisionsResponse{Revisions: toRevisionResponses(revisions), Count: len(revisions)})
}

func listQueryFromRequest(r *http.Request) (ListQuery, error) {
	limit, err := optionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		return ListQuery{}, err
	}
	offset, err := optionalInt(r.URL.Query().Get("offset"))
	if err != nil {
		return ListQuery{}, err
	}
	return ListQuery{
		Kind:              r.URL.Query().Get("kind"),
		Status:            r.URL.Query().Get("status"),
		RequiredTags:      splitCSV(r.URL.Query().Get("required_tags")),
		AnyTags:           splitCSV(r.URL.Query().Get("any_tags")),
		Audience:          splitCSV(r.URL.Query().Get("audience")),
		IncludeDeprecated: boolQuery(r, "include_deprecated"),
		IncludeUnreviewed: boolQuery(r, "include_unreviewed"),
		IncludeArchived:   boolQuery(r, "include_archived"),
		Limit:             limit,
		Offset:            offset,
	}, nil
}

func boolQuery(r *http.Request, key string) bool {
	value := r.URL.Query().Get(key)
	return value == "1" || value == "true" || value == "yes"
}

func optionalInt(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, badRequest(err)
	}
	return value, nil
}
