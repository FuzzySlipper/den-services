package review

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"den-services/shared/api"
)

type ReviewUseCases interface {
	CreateRound(ctx context.Context, projectID string, taskID int64, req CreateReviewRoundRequest) (*ReviewRound, error)
	CreateRoundForTask(ctx context.Context, taskID int64, req CreateReviewRoundRequest) (*ReviewRound, error)
	RequestReview(ctx context.Context, projectID string, taskID int64, req CreateReviewRoundRequest) (*ReviewPacket, error)
	ListRounds(ctx context.Context, projectID string, taskID int64) ([]*ReviewRound, error)
	ListRoundsForTask(ctx context.Context, taskID int64) ([]*ReviewRound, error)
	CreateFinding(ctx context.Context, roundID int64, req CreateReviewFindingRequest) (*ReviewFinding, error)
	ListFindings(ctx context.Context, projectID string, taskID int64, query ListFindingsQuery) ([]*ReviewFinding, error)
	ListFindingsForTask(ctx context.Context, taskID int64, query ListFindingsQuery) ([]*ReviewFinding, error)
	SetVerdict(ctx context.Context, roundID int64, req SetReviewVerdictRequest) (*ReviewRound, error)
	RespondToFinding(ctx context.Context, findingID int64, req RespondToFindingRequest) (*ReviewFinding, error)
	SetFindingStatus(ctx context.Context, findingID int64, req SetFindingStatusRequest) (*ReviewFinding, error)
	SplitFindingsToFollowUp(ctx context.Context, projectID string, taskID int64, req SplitFindingsRequest) (SplitFindingsResponse, error)
	PostReviewFindings(ctx context.Context, projectID string, taskID int64, req PostReviewFindingsRequest) (*ReviewPacket, error)
	ValidatePacketMarkdown(ctx context.Context, projectID string, taskID int64, markdown string) (*ReviewPacket, error)
	PostPacketMarkdown(ctx context.Context, projectID string, taskID int64, req PostPacketMarkdownRequest) (*ReviewPacket, error)
	WorkflowSummary(ctx context.Context, projectID string, taskID int64) (WorkflowSummary, error)
	WorkflowSummaryForTask(ctx context.Context, taskID int64) (WorkflowSummary, error)
	RegisterGitHubCheckGate(ctx context.Context, projectID string, taskID int64, req RegisterGitHubCheckGateRequest) (*GitHubCheckGate, error)
	GetGitHubCheckGate(ctx context.Context, projectID string, taskID int64, commitSHA string) (*GitHubCheckGate, error)
}

type Handler struct {
	service ReviewUseCases
}

func NewHandler(service ReviewUseCases) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/review/rounds", h.createRound)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/review/request", h.requestReview)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/{task_id}/review/rounds", h.listRounds)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/{task_id}/review/findings", h.listFindings)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/review/findings/split-follow-up", h.splitFindings)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/review/findings/post", h.postReviewFindings)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/{task_id}/review/workflow-summary", h.workflowSummary)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/review/packets/validate", h.validatePacket)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/review/packets", h.postPacket)
	mux.HandleFunc("POST /v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates", h.registerGitHubCheckGate)
	mux.HandleFunc("GET /v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates/{commit_sha}", h.getGitHubCheckGate)
	mux.HandleFunc("POST /v1/tasks/{task_id}/review/rounds", h.createRoundForTask)
	mux.HandleFunc("GET /v1/tasks/{task_id}/review/rounds", h.listRoundsForTask)
	mux.HandleFunc("GET /v1/tasks/{task_id}/review/findings", h.listFindingsForTask)
	mux.HandleFunc("GET /v1/tasks/{task_id}/review/workflow-summary", h.workflowSummaryForTask)
	mux.HandleFunc("POST /v1/review/rounds/{review_round_id}/findings", h.createFinding)
	mux.HandleFunc("POST /v1/review/rounds/{review_round_id}/verdict", h.setVerdict)
	mux.HandleFunc("POST /v1/review/findings/{finding_id}/response", h.respondToFinding)
	mux.HandleFunc("POST /v1/review/findings/{finding_id}/status", h.setFindingStatus)
}

func (h *Handler) createRound(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	var req CreateReviewRoundRequest
	if !decode(w, r, &req) {
		return
	}
	round, err := h.service.CreateRound(r.Context(), r.PathValue("project_id"), taskID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toRoundResponse(round))
}

func (h *Handler) createRoundForTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	var req CreateReviewRoundRequest
	if !decode(w, r, &req) {
		return
	}
	round, err := h.service.CreateRoundForTask(r.Context(), taskID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toRoundResponse(round))
}

func (h *Handler) requestReview(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	var req CreateReviewRoundRequest
	if !decode(w, r, &req) {
		return
	}
	packet, err := h.service.RequestReview(r.Context(), r.PathValue("project_id"), taskID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toPacketResponse(packet))
}

func (h *Handler) listRounds(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	rounds, err := h.service.ListRounds(r.Context(), r.PathValue("project_id"), taskID)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toRoundResponses(rounds))
}

func (h *Handler) listRoundsForTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	rounds, err := h.service.ListRoundsForTask(r.Context(), taskID)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toRoundResponses(rounds))
}

func (h *Handler) createFinding(w http.ResponseWriter, r *http.Request) {
	roundID, ok := pathInt64(w, r, "review_round_id")
	if !ok {
		return
	}
	var req CreateReviewFindingRequest
	if !decode(w, r, &req) {
		return
	}
	finding, err := h.service.CreateFinding(r.Context(), roundID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toFindingResponse(finding))
}

func (h *Handler) listFindings(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	query, err := findingsQuery(r)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	findings, err := h.service.ListFindings(r.Context(), r.PathValue("project_id"), taskID, query)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toFindingResponses(findings))
}

func (h *Handler) listFindingsForTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	query, err := findingsQuery(r)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	findings, err := h.service.ListFindingsForTask(r.Context(), taskID, query)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toFindingResponses(findings))
}

func (h *Handler) setVerdict(w http.ResponseWriter, r *http.Request) {
	roundID, ok := pathInt64(w, r, "review_round_id")
	if !ok {
		return
	}
	var req SetReviewVerdictRequest
	if !decode(w, r, &req) {
		return
	}
	round, err := h.service.SetVerdict(r.Context(), roundID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toRoundResponse(round))
}

func (h *Handler) respondToFinding(w http.ResponseWriter, r *http.Request) {
	findingID, ok := pathInt64(w, r, "finding_id")
	if !ok {
		return
	}
	var req RespondToFindingRequest
	if !decode(w, r, &req) {
		return
	}
	finding, err := h.service.RespondToFinding(r.Context(), findingID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toFindingResponse(finding))
}

func (h *Handler) setFindingStatus(w http.ResponseWriter, r *http.Request) {
	findingID, ok := pathInt64(w, r, "finding_id")
	if !ok {
		return
	}
	var req SetFindingStatusRequest
	if !decode(w, r, &req) {
		return
	}
	finding, err := h.service.SetFindingStatus(r.Context(), findingID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toFindingResponse(finding))
}

func (h *Handler) splitFindings(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	var req SplitFindingsRequest
	if !decode(w, r, &req) {
		return
	}
	result, err := h.service.SplitFindingsToFollowUp(r.Context(), r.PathValue("project_id"), taskID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) postReviewFindings(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	var req PostReviewFindingsRequest
	if !decode(w, r, &req) {
		return
	}
	packet, err := h.service.PostReviewFindings(r.Context(), r.PathValue("project_id"), taskID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toPacketResponse(packet))
}

func (h *Handler) validatePacket(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	var req PostPacketMarkdownRequest
	if !decode(w, r, &req) {
		return
	}
	packet, err := h.service.ValidatePacketMarkdown(r.Context(), r.PathValue("project_id"), taskID, req.Markdown)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toPacketResponse(packet))
}

func (h *Handler) postPacket(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	var req PostPacketMarkdownRequest
	if !decode(w, r, &req) {
		return
	}
	packet, err := h.service.PostPacketMarkdown(r.Context(), r.PathValue("project_id"), taskID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toPacketResponse(packet))
}

func (h *Handler) workflowSummary(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	summary, err := h.service.WorkflowSummary(r.Context(), r.PathValue("project_id"), taskID)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toWorkflowSummaryResponse(summary))
}

func (h *Handler) workflowSummaryForTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	summary, err := h.service.WorkflowSummaryForTask(r.Context(), taskID)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toWorkflowSummaryResponse(summary))
}

func (h *Handler) registerGitHubCheckGate(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	var req RegisterGitHubCheckGateRequest
	if !decode(w, r, &req) {
		return
	}
	gate, err := h.service.RegisterGitHubCheckGate(r.Context(), r.PathValue("project_id"), taskID, req)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toGitHubCheckGateResponse(gate))
}

func (h *Handler) getGitHubCheckGate(w http.ResponseWriter, r *http.Request) {
	taskID, ok := h.taskID(w, r)
	if !ok {
		return
	}
	gate, err := h.service.GetGitHubCheckGate(r.Context(), r.PathValue("project_id"), taskID, r.PathValue("commit_sha"))
	if err != nil {
		writeReviewError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toGitHubCheckGateResponse(gate))
}

func (h *Handler) taskID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return pathInt64(w, r, "task_id")
}

func decode[T any](w http.ResponseWriter, r *http.Request, target *T) bool {
	if err := api.DecodeJSON(r, target); err != nil {
		writeReviewError(w, err)
		return false
	}
	return true
}

func pathInt64(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeReviewError(w, badRequest(err))
		return 0, false
	}
	return id, true
}

func findingsQuery(r *http.Request) (ListFindingsQuery, error) {
	query := r.URL.Query()
	var result ListFindingsQuery
	if raw := strings.TrimSpace(query.Get("review_round_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return result, badRequest(err)
		}
		result.ReviewRoundID = &id
	}
	if raw := strings.TrimSpace(query.Get("status")); raw != "" {
		result.Statuses = splitCSV(raw)
	}
	if raw := strings.TrimSpace(query.Get("resolved")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return result, badRequest(err)
		}
		result.Resolved = &value
	}
	return result, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func writeReviewError(w http.ResponseWriter, err error) {
	var serviceError *ServiceError
	if errors.As(err, &serviceError) && serviceError.Field() != "" {
		status := serviceError.HTTPStatus()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(struct {
			Error ValidationIssue `json:"error"`
		}{Error: ValidationIssue{
			Code: serviceError.Code(), Field: serviceError.Field(), Message: serviceError.Error(), DocsRef: serviceError.DocsRef(),
		}})
		return
	}
	api.WriteServiceError(w, err)
}
