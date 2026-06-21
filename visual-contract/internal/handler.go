package visualcontract

import (
	"net/http"

	"den-services/shared/api"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /visual-contracts/validate", h.validateContract)
	mux.HandleFunc("POST /visual-contracts/compare", h.compareContracts)
	mux.HandleFunc("POST /visual-contracts/overlays", h.overlays)
	mux.HandleFunc("POST /visual-contracts/from-web-evidence", h.fromWebEvidence)
}

func (h *Handler) validateContract(w http.ResponseWriter, r *http.Request) {
	var req ValidateRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	response, err := h.service.Validate(r.Context(), &req.Contract)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) compareContracts(w http.ResponseWriter, r *http.Request) {
	var req CompareRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	report, err := h.service.Compare(r.Context(), &req.Reference, &req.Candidate)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, report)
}

func (h *Handler) overlays(w http.ResponseWriter, r *http.Request) {
	var req OverlayRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	response, err := h.service.Overlays(r.Context(), &req.Reference, req.Candidate, req.Report)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) fromWebEvidence(w http.ResponseWriter, r *http.Request) {
	var req WebEvidenceRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	contract, err := h.service.FromWebEvidence(r.Context(), &req.Evidence)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, contract)
}
