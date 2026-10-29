package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"den-services/shared/api"
)

type OpsUseCases interface {
	RegisterHost(ctx context.Context, hostID string, request RegisterHostRequest) (*Host, error)
	Hosts(ctx context.Context) ([]HostView, error)
	RegisterJob(ctx context.Context, jobID string, request RegisterJobRequest) (JobView, error)
	ReportRun(ctx context.Context, jobID string, runID string, request RunReportRequest, raw []byte) (ReportOutcome, *Run, JobView, error)
	ReportVerification(ctx context.Context, jobID string, verificationID string, report VerificationReport, raw []byte) (ReportOutcome, *Verification, error)
	Summary(ctx context.Context) (Summary, error)
	JobDetail(ctx context.Context, jobID string, limit int) (JobDetail, error)
	Runs(ctx context.Context, jobID string, limit int) ([]*Run, error)
	Run(ctx context.Context, jobID string, runID string) (*Run, error)
	Verifications(ctx context.Context, jobID string, limit int) ([]*Verification, error)
	Errors(ctx context.Context, limit int) (ErrorsView, error)
}

type Handler struct {
	service      OpsUseCases
	defaultLimit int
}

func NewHandler(service OpsUseCases, defaultLimit int) *Handler {
	return &Handler{service: service, defaultLimit: defaultLimit}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/ops/summary", h.summary)
	mux.HandleFunc("GET /v1/ops/errors", h.errors)
	mux.HandleFunc("GET /v1/ops/hosts", h.hosts)
	mux.HandleFunc("PUT /v1/ops/hosts/{host_id}", h.registerHost)
	mux.HandleFunc("GET /v1/ops/jobs", h.jobs)
	mux.HandleFunc("GET /v1/ops/jobs/{job_id}", h.jobDetail)
	mux.HandleFunc("PUT /v1/ops/jobs/{job_id}", h.registerJob)
	mux.HandleFunc("GET /v1/ops/jobs/{job_id}/runs", h.runs)
	mux.HandleFunc("GET /v1/ops/jobs/{job_id}/runs/{run_id}", h.run)
	mux.HandleFunc("PUT /v1/ops/jobs/{job_id}/runs/{run_id}", h.reportRun)
	mux.HandleFunc("GET /v1/ops/jobs/{job_id}/verifications", h.verifications)
	mux.HandleFunc("PUT /v1/ops/jobs/{job_id}/verifications/{verification_id}", h.reportVerification)
	mux.HandleFunc("POST /v1/ops/reports", h.postReport)
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.service.Summary(r.Context())
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	counts := map[string]int{}
	for status, count := range summary.Counts {
		counts[string(status)] = count
	}
	api.WriteJSON(w, http.StatusOK, SummaryResponse{
		GeneratedAt: summary.GeneratedAt, Status: string(summary.Status), Counts: counts,
		Hosts: toHostResponses(summary.Hosts), Jobs: toJobStatusResponses(summary.Jobs),
	})
}

func (h *Handler) errors(w http.ResponseWriter, r *http.Request) {
	limit, err := h.limit(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	view, err := h.service.Errors(r.Context(), limit)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	problems := make([]ProblemResponse, 0, len(view.Problems))
	for _, problem := range view.Problems {
		problems = append(problems, ProblemResponse{
			JobID: problem.Job.JobID, HostID: problem.Job.HostID, Status: string(problem.Status), Reason: problem.StatusReason,
		})
	}
	api.WriteJSON(w, http.StatusOK, ErrorsResponse{
		GeneratedAt: view.GeneratedAt, Problems: problems, ProblemRuns: toRunResponses(view.ProblemRuns),
		FailedVerifications: toVerificationResponses(view.FailedVerifications),
	})
}

func (h *Handler) hosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := h.service.Hosts(r.Context())
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, HostsResponse{Hosts: toHostResponses(hosts)})
}

func (h *Handler) registerHost(w http.ResponseWriter, r *http.Request) {
	var request RegisterHostRequest
	if err := api.DecodeJSON(r, &request); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	host, err := h.service.RegisterHost(r.Context(), r.PathValue("host_id"), request)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, HostStatusResponse{
		HostID: host.HostID, DisplayName: host.DisplayName, Role: host.Role, Notes: host.Notes, Registered: true,
	})
}

func (h *Handler) jobs(w http.ResponseWriter, r *http.Request) {
	summary, err := h.service.Summary(r.Context())
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, JobsResponse{GeneratedAt: summary.GeneratedAt, Jobs: toJobStatusResponses(summary.Jobs)})
}

func (h *Handler) jobDetail(w http.ResponseWriter, r *http.Request) {
	limit, err := h.limit(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	detail, err := h.service.JobDetail(r.Context(), r.PathValue("job_id"), limit)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, JobDetailResponse{
		Job: toJobStatusResponse(detail.View), Runs: toRunResponses(detail.Runs),
		Verifications: toVerificationResponses(detail.Verifications),
	})
}

func (h *Handler) registerJob(w http.ResponseWriter, r *http.Request) {
	var request RegisterJobRequest
	if err := api.DecodeJSON(r, &request); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	view, err := h.service.RegisterJob(r.Context(), r.PathValue("job_id"), request)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toJobStatusResponse(view))
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	limit, err := h.limit(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	runs, err := h.service.Runs(r.Context(), r.PathValue("job_id"), limit)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, RunsResponse{Runs: toRunResponses(runs)})
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	run, err := h.service.Run(r.Context(), r.PathValue("job_id"), r.PathValue("run_id"))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toRunDetailResponse(run))
}

func (h *Handler) reportRun(w http.ResponseWriter, r *http.Request) {
	request, raw, err := decodeReport[RunReportRequest](w, r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	h.writeRunReport(w, r, r.PathValue("job_id"), r.PathValue("run_id"), request, raw)
}

// postReport takes job_id and run_id from the report body, so a host can send
// a report file without building the URL from it.
func (h *Handler) postReport(w http.ResponseWriter, r *http.Request) {
	request, raw, err := decodeReport[RunReportRequest](w, r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	h.writeRunReport(w, r, request.JobID, request.RunID, request, raw)
}

func (h *Handler) writeRunReport(w http.ResponseWriter, r *http.Request, jobID string, runID string, request RunReportRequest, raw []byte) {
	outcome, run, view, err := h.service.ReportRun(r.Context(), jobID, runID, request, raw)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, ReportRunResponse{Outcome: string(outcome), Run: toRunResponse(run), Job: toJobStatusResponse(view)})
}

func (h *Handler) verifications(w http.ResponseWriter, r *http.Request) {
	limit, err := h.limit(r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	verifications, err := h.service.Verifications(r.Context(), r.PathValue("job_id"), limit)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, VerificationsResponse{Verifications: toVerificationResponses(verifications)})
}

func (h *Handler) reportVerification(w http.ResponseWriter, r *http.Request) {
	report, raw, err := decodeReport[VerificationReport](w, r)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	outcome, verification, err := h.service.ReportVerification(r.Context(), r.PathValue("job_id"), r.PathValue("verification_id"), report, raw)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, VerificationReportResponse{Outcome: string(outcome), Verification: toVerificationResponse(verification)})
}

func (h *Handler) limit(r *http.Request) (int, error) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return h.defaultLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > MaxListLimit {
		return 0, validationFailed("limit must be between 1 and %d", MaxListLimit)
	}
	return limit, nil
}

// decodeReport reads a report body leniently: reporter files carry extra
// fields Ops does not model, and those stay in the returned raw bytes.
func decodeReport[T any](w http.ResponseWriter, r *http.Request) (T, []byte, error) {
	var target T
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxReportBodyBytes))
	if err != nil {
		return target, nil, fmt.Errorf("%w: reading report: %w", api.ErrBadRequest, err)
	}
	if err := json.Unmarshal(raw, &target); err != nil {
		return target, nil, fmt.Errorf("%w: decoding report json: %w", api.ErrBadRequest, err)
	}
	return target, raw, nil
}

func toHostResponses(hosts []HostView) []HostStatusResponse {
	responses := make([]HostStatusResponse, 0, len(hosts))
	for _, host := range hosts {
		coverage := "not_configured"
		if host.Configured {
			coverage = "configured"
		}
		responses = append(responses, HostStatusResponse{
			HostID: host.Host.HostID, DisplayName: host.Host.DisplayName, Role: host.Host.Role, Notes: host.Host.Notes,
			Registered: host.Registered, Coverage: coverage, JobCount: host.JobCount, Status: string(host.Status),
		})
	}
	return responses
}
