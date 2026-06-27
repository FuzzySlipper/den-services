package artifacts

import (
	"net/http"

	"den-services/shared/api"
)

type Handler struct{}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/artifacts", h.create)
	mux.HandleFunc("GET /v1/artifacts/{artifact_id}/metadata", h.metadata)
	mux.HandleFunc("GET /v1/artifacts/{artifact_id}/content", h.content)
	mux.HandleFunc("GET /v1/artifacts/{artifact_id}/thumbnail", h.thumbnail)
	mux.HandleFunc("DELETE /v1/artifacts/{artifact_id}", h.delete)
}

func (h *Handler) create(w http.ResponseWriter, _ *http.Request) {
	writeNotImplemented(w, "artifact upload is not implemented in the scaffold")
}

func (h *Handler) metadata(w http.ResponseWriter, _ *http.Request) {
	writeNotImplemented(w, "artifact metadata read is not implemented in the scaffold")
}

func (h *Handler) content(w http.ResponseWriter, _ *http.Request) {
	writeNotImplemented(w, "artifact content read is not implemented in the scaffold")
}

func (h *Handler) thumbnail(w http.ResponseWriter, _ *http.Request) {
	writeNotImplemented(w, "artifact thumbnail read is not implemented in the scaffold")
}

func (h *Handler) delete(w http.ResponseWriter, _ *http.Request) {
	writeNotImplemented(w, "artifact tombstone/delete is not implemented in the scaffold")
}

func writeNotImplemented(w http.ResponseWriter, message string) {
	api.WriteJSON(w, http.StatusNotImplemented, NotImplementedResponse{
		Code:    "not_implemented",
		Message: message,
	})
}
