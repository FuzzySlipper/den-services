package conversation

import "net/http"

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(_ *http.ServeMux) {
	// Product endpoints are intentionally left to task #2915. This substrate
	// only wires the authenticated conversation service boundary.
}
