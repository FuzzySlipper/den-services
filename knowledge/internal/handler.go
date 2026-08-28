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
	DeleteEntry(ctx context.Context, slug string) error
	GetEntry(ctx context.Context, slug string, includeArchived bool) (*Entry, error)
	ListEntries(ctx context.Context, query ListQuery) ([]EntrySummary, error)
	SearchEntries(ctx context.Context, query SearchQuery) ([]SearchResult, error)
	Guide(ctx context.Context, query GuideQuery) (GuideResponse, error)
	ListRevisions(ctx context.Context, slug string) ([]RevisionSummary, error)
	EntryCard(ctx context.Context, slug string, includeArchived bool) (EntryCardResponse, error)
	BatchCards(ctx context.Context, slugs []string, includeArchived bool, offset int) (CardsResponse, error)
	ReadEntry(ctx context.Context, slug string, view string, sectionID string, knownRevision int, knownDigest string, includeArchived bool) (ReadResponse, error)
	ReplaceLinks(ctx context.Context, slug string, requests []EntryLinkRequest) error
	StoreMap(ctx context.Context, request StoreKnowledgeMapRequest) (*KnowledgeMap, error)
	GetMap(ctx context.Context, slug string) (KnowledgeMapResponse, error)
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
	mux.HandleFunc("DELETE /v1/knowledge/entries/{slug}", h.deleteEntry)
	mux.HandleFunc("GET /v1/knowledge/entries/{slug}/revisions", h.listRevisions)
	mux.HandleFunc("GET /v1/knowledge/entries/{slug}/card", h.entryCard)
	mux.HandleFunc("POST /v1/knowledge/entries/cards", h.batchCards)
	mux.HandleFunc("GET /v1/knowledge/entries/{slug}/read", h.readEntry)
	mux.HandleFunc("PUT /v1/knowledge/entries/{slug}/links", h.replaceLinks)
	mux.HandleFunc("POST /v1/knowledge/maps", h.storeMap)
	mux.HandleFunc("GET /v1/knowledge/maps/{slug}", h.getMap)
	mux.HandleFunc("POST /v1/knowledge/search", h.searchEntries)
	mux.HandleFunc("POST /v1/knowledge/guide", h.guide)
}

func (h *Handler) entryCard(w http.ResponseWriter, r *http.Request) {
	card, err := h.service.EntryCard(r.Context(), r.PathValue("slug"), boolQuery(r, "include_archived"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, card)
}

func (h *Handler) batchCards(w http.ResponseWriter, r *http.Request) {
	var request CardsRequest
	if err := api.DecodeJSON(r, &request); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	offset, err := optionalInt(r.URL.Query().Get("offset"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	result, err := h.service.BatchCards(r.Context(), request.Slugs, request.IncludeArchived, offset)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) readEntry(w http.ResponseWriter, r *http.Request) {
	knownRevision, err := optionalInt(r.URL.Query().Get("known_revision"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "outline"
	}
	result, err := h.service.ReadEntry(r.Context(), r.PathValue("slug"), view, r.URL.Query().Get("section"), knownRevision, r.URL.Query().Get("known_digest"), boolQuery(r, "include_archived"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) replaceLinks(w http.ResponseWriter, r *http.Request) {
	var request ReplaceLinksRequest
	if err := api.DecodeJSON(r, &request); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := h.service.ReplaceLinks(r.Context(), r.PathValue("slug"), request.Links); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) storeMap(w http.ResponseWriter, r *http.Request) {
	var request StoreKnowledgeMapRequest
	if err := api.DecodeJSON(r, &request); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	knowledgeMap, err := h.service.StoreMap(r.Context(), request)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, KnowledgeMapResponse{Slug: knowledgeMap.Slug, Title: knowledgeMap.Title, Summary: knowledgeMap.Summary, UpdatedAt: knowledgeMap.UpdatedAt})
}

func (h *Handler) getMap(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetMap(r.Context(), r.PathValue("slug"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) deleteEntry(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if err := h.service.DeleteEntry(r.Context(), slug); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, DeleteEntryResponse{Deleted: true, Slug: slug})
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
