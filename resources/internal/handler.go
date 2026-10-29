package resources

import (
	"context"
	"net/http"
	"strconv"

	"den-services/shared/api"
)

type ResourcesUseCases interface {
	State(ctx context.Context, workLimit int) (StateResponse, error)
	RegisterHost(ctx context.Context, id string, request RegisterHostRequest) (HostResponse, error)
	ReportTelemetry(ctx context.Context, id string, request TelemetryRequest) error
	ReportApps(ctx context.Context, id string, request AppsRequest) error
	PutSettings(ctx context.Context, request SettingsRequest) error
	UpsertResource(ctx context.Context, request ResourceRequest) (ResourceResponse, error)
	Resources(ctx context.Context) ([]ResourceResponse, error)
	Resource(ctx context.Context, id string) (ResourceResponse, error)
	ReportWork(ctx context.Context, request WorkReportRequest) (WorkReceipt, error)
	UpdateWork(ctx context.Context, id string, request WorkUpdateRequest) (WorkResponse, error)
	ListWork(ctx context.Context, filter WorkFilter) ([]WorkResponse, error)
	Work(ctx context.Context, id string) (WorkResponse, error)
}

type Handler struct{ service ResourcesUseCases }

func NewHandler(service ResourcesUseCases) *Handler { return &Handler{service: service} }

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/resources/state", h.state)
	mux.HandleFunc("PUT /v1/resources/hosts/{host_id}", h.registerHost)
	mux.HandleFunc("POST /v1/resources/hosts/{host_id}/telemetry", h.telemetry)
	mux.HandleFunc("POST /v1/resources/hosts/{host_id}/apps", h.apps)
	mux.HandleFunc("PUT /v1/resources/settings", h.settings)
	mux.HandleFunc("GET /v1/resources/directory", h.directory)
	mux.HandleFunc("POST /v1/resources/directory", h.upsertResource)
	mux.HandleFunc("GET /v1/resources/directory/{resource_id}", h.resource)
	mux.HandleFunc("GET /v1/resources/work", h.listWork)
	mux.HandleFunc("POST /v1/resources/work", h.reportWork)
	mux.HandleFunc("GET /v1/resources/work/{work_id}", h.work)
	mux.HandleFunc("POST /v1/resources/work/{work_id}", h.updateWork)
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	limit, err := workLimit(r, "work_limit")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	respond(w, func() (StateResponse, error) { return h.service.State(r.Context(), limit) })
}

func (h *Handler) registerHost(w http.ResponseWriter, r *http.Request) {
	var request RegisterHostRequest
	if decode(w, r, &request) {
		respond(w, func() (HostResponse, error) {
			return h.service.RegisterHost(r.Context(), r.PathValue("host_id"), request)
		})
	}
}

func (h *Handler) telemetry(w http.ResponseWriter, r *http.Request) {
	var request TelemetryRequest
	if decode(w, r, &request) {
		respond(w, func() (OKResponse, error) {
			return OKResponse{OK: true}, h.service.ReportTelemetry(r.Context(), r.PathValue("host_id"), request)
		})
	}
}

func (h *Handler) apps(w http.ResponseWriter, r *http.Request) {
	var request AppsRequest
	if decode(w, r, &request) {
		respond(w, func() (OKResponse, error) {
			return OKResponse{OK: true}, h.service.ReportApps(r.Context(), r.PathValue("host_id"), request)
		})
	}
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	var request SettingsRequest
	if decode(w, r, &request) {
		respond(w, func() (OKResponse, error) { return OKResponse{OK: true}, h.service.PutSettings(r.Context(), request) })
	}
}

func (h *Handler) directory(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (DirectoryResponse, error) {
		found, err := h.service.Resources(r.Context())
		return DirectoryResponse{Resources: found}, err
	})
}

func (h *Handler) upsertResource(w http.ResponseWriter, r *http.Request) {
	var request ResourceRequest
	if decode(w, r, &request) {
		respond(w, func() (ResourceResponse, error) { return h.service.UpsertResource(r.Context(), request) })
	}
}

func (h *Handler) resource(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (ResourceResponse, error) { return h.service.Resource(r.Context(), r.PathValue("resource_id")) })
}

func (h *Handler) listWork(w http.ResponseWriter, r *http.Request) {
	limit, err := workLimit(r, "limit")
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	query := r.URL.Query()
	filter := WorkFilter{
		ActiveOnly: query.Get("active") == "true", Source: query.Get("source"), Host: query.Get("host"), Limit: limit,
	}
	respond(w, func() (WorkListResponse, error) {
		work, err := h.service.ListWork(r.Context(), filter)
		return WorkListResponse{Work: work}, err
	})
}

func (h *Handler) reportWork(w http.ResponseWriter, r *http.Request) {
	var request WorkReportRequest
	if decode(w, r, &request) {
		respond(w, func() (WorkReceipt, error) { return h.service.ReportWork(r.Context(), request) })
	}
}

func (h *Handler) work(w http.ResponseWriter, r *http.Request) {
	respond(w, func() (WorkResponse, error) { return h.service.Work(r.Context(), r.PathValue("work_id")) })
}

func (h *Handler) updateWork(w http.ResponseWriter, r *http.Request) {
	var request WorkUpdateRequest
	if decode(w, r, &request) {
		respond(w, func() (WorkResponse, error) {
			return h.service.UpdateWork(r.Context(), r.PathValue("work_id"), request)
		})
	}
}

func decode[T any](w http.ResponseWriter, r *http.Request, target *T) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	if err := api.DecodeJSON(r, target); err != nil {
		api.WriteServiceError(w, err)
		return false
	}
	return true
}

func respond[T any](w http.ResponseWriter, produce func() (T, error)) {
	payload, err := produce()
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, payload)
}

func workLimit(r *http.Request, name string) (int, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return DefaultWorkLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > MaxWorkLimit {
		return 0, validationFailed("%s must be between 1 and %d", name, MaxWorkLimit)
	}
	return limit, nil
}
