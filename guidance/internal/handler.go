package guidance

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"den-services/shared/api"
)

type GuidanceUseCases interface {
	CheckStore(ctx context.Context) error
	AddEntry(ctx context.Context, projectID string, req AddEntryRequest) (*Entry, error)
	ListEntries(ctx context.Context, projectID string, includeGlobal bool) ([]Entry, error)
	DeleteEntry(ctx context.Context, projectID string, entryID int64) error
	Resolve(ctx context.Context, query ResolveQuery) (GuidancePacket, error)
	DocumentReferences(ctx context.Context, documentProjectID string, documentSlug string) ([]DocumentReference, error)
}

type Handler struct {
	service GuidanceUseCases
}

func NewHandler(service GuidanceUseCases) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/projects/{project_id}/agent-guidance", h.resolve)
	mux.HandleFunc("GET /v1/projects/{project_id}/agent-guidance/entries", h.listEntries)
	mux.HandleFunc("POST /v1/projects/{project_id}/agent-guidance/entries", h.addEntry)
	mux.HandleFunc("DELETE /v1/projects/{project_id}/agent-guidance/entries/{entry_id}", h.deleteEntry)
	mux.HandleFunc("GET /v1/guidance/document-references", h.documentReferences)
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	maxBytes, err := optionalInt(query.Get("max_bytes"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	packet, err := h.service.Resolve(r.Context(), ResolveQuery{
		ProjectID:      r.PathValue("project_id"),
		IncludeContent: boolQuery(r, "include_content", true),
		MaxBytes:       maxBytes,
		Audience:       splitCSV(query.Get("audience")),
		IncludeHidden:  boolQuery(r, "include_hidden", false),
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toPacketResponse(packet))
}

func (h *Handler) listEntries(w http.ResponseWriter, r *http.Request) {
	entries, err := h.service.ListEntries(r.Context(), r.PathValue("project_id"), boolQuery(r, "include_global", false))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, EntryListResponse{Entries: toEntryResponses(entries), Count: len(entries)})
}

func (h *Handler) addEntry(w http.ResponseWriter, r *http.Request) {
	var req AddEntryRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	entry, err := h.service.AddEntry(r.Context(), r.PathValue("project_id"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toEntryResponse(*entry))
}

func (h *Handler) deleteEntry(w http.ResponseWriter, r *http.Request) {
	entryID, err := strconv.ParseInt(r.PathValue("entry_id"), 10, 64)
	if err != nil {
		api.WriteServiceError(w, validationFailed(err))
		return
	}
	if err := h.service.DeleteEntry(r.Context(), r.PathValue("project_id"), entryID); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, DeleteResponse{Deleted: true, Message: "Agent guidance entry deleted."})
}

func (h *Handler) documentReferences(w http.ResponseWriter, r *http.Request) {
	refs, err := h.service.DocumentReferences(r.Context(), r.URL.Query().Get("document_project_id"), r.URL.Query().Get("document_slug"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	responses := toDocumentReferenceResponses(refs)
	api.WriteJSON(w, http.StatusOK, DocumentReferencesResponse{References: responses, ReferencedBy: responses, Count: len(responses)})
}

func boolQuery(r *http.Request, key string, defaultValue bool) bool {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return defaultValue
	}
	return value == "1" || value == "true" || value == "yes"
}

func optionalInt(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, validationFailed(err)
	}
	return value, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
