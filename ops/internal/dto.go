package ops

import (
	"encoding/json"
	"time"
)

// RegisterHostRequest is the body of PUT /v1/ops/hosts/{host_id}.
type RegisterHostRequest struct {
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Notes       string `json:"notes"`
}

func (r RegisterHostRequest) Validate() error { return nil }

type ScheduleRequest struct {
	Timezone string   `json:"timezone"`
	Times    []string `json:"times"`
	Weekdays []string `json:"weekdays"`
	// Grace is a Go duration such as "3h": how long after a start a run may begin.
	Grace string `json:"grace"`
}

// RegisterJobRequest is the body of PUT /v1/ops/jobs/{job_id}. It declares an
// expectation so a job that never reports is still visible.
type RegisterJobRequest struct {
	HostID              string           `json:"host_id"`
	DisplayName         string           `json:"display_name"`
	Engine              string           `json:"engine"`
	Kind                string           `json:"kind"`
	Source              string           `json:"source"`
	Scope               string           `json:"scope"`
	Target              string           `json:"target"`
	Schedule            *ScheduleRequest `json:"schedule"`
	HeartbeatStaleAfter string           `json:"heartbeat_stale_after"`
	Retention           string           `json:"retention"`
	RecoveryPointer     string           `json:"recovery_pointer"`
	Notes               string           `json:"notes"`
	Enabled             *bool            `json:"enabled"`
}

func (r RegisterJobRequest) Validate() error {
	if err := validateID("host_id", r.HostID); err != nil {
		return err
	}
	if r.Engine == "" || r.Kind == "" {
		return validationFailed("engine and kind are required")
	}
	return nil
}

type StatsReport struct {
	OriginalSize     *int64 `json:"original_size"`
	CompressedSize   *int64 `json:"compressed_size"`
	DeduplicatedSize *int64 `json:"deduplicated_size"`
	NFiles           *int64 `json:"nfiles"`
}

type ArchiveReport struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Duration *float64     `json:"duration"`
	Stats    *StatsReport `json:"stats"`
}

// VerificationReport is a repository check or restore test. It is accepted on
// its own (PUT .../verifications/{id}) or embedded in a run report as
// "verification" or "restore_test", matching the den-agents Borg reports.
type VerificationReport struct {
	// Kind is "restore_test", "verification", or a check name such as
	// "repository-and-latest-archive" (which implies a verification).
	Kind                string     `json:"kind"`
	Type                string     `json:"type"`
	RunID               string     `json:"run_id"`
	State               string     `json:"state"`
	At                  *time.Time `json:"at"`
	Scope               string     `json:"scope"`
	Archive             string     `json:"archive"`
	FullDataRead        *bool      `json:"full_data_read"`
	EvidencePath        string     `json:"evidence_path"`
	RestoredTo          string     `json:"restored_to"`
	Checked             []string   `json:"checked"`
	FilesCompared       []string   `json:"files_compared"`
	RepositoriesChecked []string   `json:"repositories_checked"`
}

// RunReportRequest is one lifecycle report for a run. Unknown fields are
// accepted and kept verbatim in the stored report, so a reporter can send the
// den-agents Borg latest.json/result.json files unchanged.
//
// A report is terminal only when finished_at is set; a report without it is a
// heartbeat whatever its state says.
type RunReportRequest struct {
	SchemaVersion     int                 `json:"schema_version"`
	JobID             string              `json:"job_id"`
	RunID             string              `json:"run_id"`
	Host              string              `json:"host"`
	Engine            string              `json:"engine"`
	Runner            string              `json:"runner"`
	Kind              string              `json:"kind"`
	State             string              `json:"state"`
	Phase             string              `json:"phase"`
	StartedAt         *time.Time          `json:"started_at"`
	UpdatedAt         *time.Time          `json:"updated_at"`
	FinishedAt        *time.Time          `json:"finished_at"`
	ExitCode          *int                `json:"exit_code"`
	Error             string              `json:"error"`
	Reason            string              `json:"reason"`
	Warnings          []string            `json:"warnings"`
	SourceNotices     []string            `json:"source_notices"`
	Target            string              `json:"target"`
	Archive           *ArchiveReport      `json:"archive"`
	Stats             *StatsReport        `json:"stats"`
	BackupComplete    *bool               `json:"backup_complete"`
	DurationSeconds   *float64            `json:"duration_seconds"`
	LogPath           string              `json:"log_path"`
	Manifest          string              `json:"manifest"`
	LocalRunDirectory string              `json:"local_run_directory"`
	RecoveryPointer   string              `json:"recovery_pointer"`
	Verification      *VerificationReport `json:"verification"`
	RestoreTest       *VerificationReport `json:"restore_test"`
}

type ScheduleResponse struct {
	Timezone     string   `json:"timezone"`
	Times        []string `json:"times"`
	Weekdays     []string `json:"weekdays"`
	GraceSeconds int64    `json:"grace_seconds"`
}

type RunResponse struct {
	JobID             string     `json:"job_id"`
	RunID             string     `json:"run_id"`
	Host              string     `json:"host"`
	Kind              string     `json:"kind"`
	Engine            string     `json:"engine"`
	Runner            string     `json:"runner"`
	State             string     `json:"state"`
	Terminal          bool       `json:"terminal"`
	Phase             string     `json:"phase"`
	StartedAt         time.Time  `json:"started_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	FinishedAt        *time.Time `json:"finished_at"`
	ExitCode          *int       `json:"exit_code"`
	Error             string     `json:"error"`
	Reason            string     `json:"reason"`
	Warnings          []string   `json:"warnings"`
	Notices           []string   `json:"notices"`
	Target            string     `json:"target"`
	ArchiveID         string     `json:"archive_id"`
	ArchiveName       string     `json:"archive_name"`
	OriginalBytes     *int64     `json:"original_bytes"`
	CompressedBytes   *int64     `json:"compressed_bytes"`
	DeduplicatedBytes *int64     `json:"deduplicated_bytes"`
	FileCount         *int64     `json:"file_count"`
	DurationSeconds   *float64   `json:"duration_seconds"`
	Complete          bool       `json:"complete"`
	LogPath           string     `json:"log_path"`
	ManifestPath      string     `json:"manifest_path"`
	RunDirectory      string     `json:"run_directory"`
	RecoveryPointer   string     `json:"recovery_pointer"`
	ReceivedAt        time.Time  `json:"received_at"`
	ReportCount       int64      `json:"report_count"`
}

type RunDetailResponse struct {
	Run RunResponse `json:"run"`
	// Report is the newest stored report body exactly as the reporter sent it.
	Report json.RawMessage `json:"report"`
}

type ReportRunResponse struct {
	Outcome string            `json:"outcome"`
	Run     RunResponse       `json:"run"`
	Job     JobStatusResponse `json:"job"`
}

type VerificationResponse struct {
	JobID          string    `json:"job_id"`
	VerificationID string    `json:"verification_id"`
	RunID          string    `json:"run_id"`
	Kind           string    `json:"kind"`
	CheckType      string    `json:"check_type"`
	State          string    `json:"state"`
	At             time.Time `json:"at"`
	Scope          string    `json:"scope"`
	Archive        string    `json:"archive"`
	FullDataRead   *bool     `json:"full_data_read"`
	EvidencePath   string    `json:"evidence_path"`
	Checked        []string  `json:"checked"`
	ReceivedAt     time.Time `json:"received_at"`
}

type JobStatusResponse struct {
	JobID                      string                `json:"job_id"`
	HostID                     string                `json:"host_id"`
	DisplayName                string                `json:"display_name"`
	Engine                     string                `json:"engine"`
	Kind                       string                `json:"kind"`
	Source                     string                `json:"source"`
	Scope                      string                `json:"scope"`
	Target                     string                `json:"target"`
	Schedule                   *ScheduleResponse     `json:"schedule"`
	HeartbeatStaleAfterSeconds int64                 `json:"heartbeat_stale_after_seconds"`
	Retention                  string                `json:"retention"`
	RecoveryPointer            string                `json:"recovery_pointer"`
	Notes                      string                `json:"notes"`
	Registered                 bool                  `json:"registered"`
	Enabled                    bool                  `json:"enabled"`
	Status                     string                `json:"status"`
	StatusReason               string                `json:"status_reason"`
	Freshness                  string                `json:"freshness"`
	ExpectedBy                 *time.Time            `json:"expected_by"`
	NextDueAt                  *time.Time            `json:"next_due_at"`
	EvaluatedAt                time.Time             `json:"evaluated_at"`
	CurrentRun                 *RunResponse          `json:"current_run"`
	HeartbeatState             *string               `json:"heartbeat_state"`
	HeartbeatAgeSeconds        *int64                `json:"heartbeat_age_seconds"`
	LastAttempt                *RunResponse          `json:"last_attempt"`
	LastFinished               *RunResponse          `json:"last_finished"`
	LastSuccess                *RunResponse          `json:"last_success"`
	LastArchive                *RunResponse          `json:"last_archive"`
	LastVerification           *VerificationResponse `json:"last_verification"`
	LastRestoreTest            *VerificationResponse `json:"last_restore_test"`
}

type JobDetailResponse struct {
	Job           JobStatusResponse      `json:"job"`
	Runs          []RunResponse          `json:"runs"`
	Verifications []VerificationResponse `json:"verifications"`
}

type HostStatusResponse struct {
	HostID      string `json:"host_id"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Notes       string `json:"notes"`
	Registered  bool   `json:"registered"`
	// Coverage is "configured" when the host has an enabled job, else "not_configured".
	Coverage string `json:"coverage"`
	JobCount int    `json:"job_count"`
	Status   string `json:"status"`
}

type SummaryResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Status      string               `json:"status"`
	Counts      map[string]int       `json:"counts"`
	Hosts       []HostStatusResponse `json:"hosts"`
	Jobs        []JobStatusResponse  `json:"jobs"`
}

type JobsResponse struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Jobs        []JobStatusResponse `json:"jobs"`
}

type HostsResponse struct {
	Hosts []HostStatusResponse `json:"hosts"`
}

type RunsResponse struct {
	Runs []RunResponse `json:"runs"`
}

type VerificationsResponse struct {
	Verifications []VerificationResponse `json:"verifications"`
}

type VerificationReportResponse struct {
	Outcome      string               `json:"outcome"`
	Verification VerificationResponse `json:"verification"`
}

type ProblemResponse struct {
	JobID  string `json:"job_id"`
	HostID string `json:"host_id"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type ErrorsResponse struct {
	GeneratedAt         time.Time              `json:"generated_at"`
	Problems            []ProblemResponse      `json:"problems"`
	ProblemRuns         []RunResponse          `json:"problem_runs"`
	FailedVerifications []VerificationResponse `json:"failed_verifications"`
}

func toRunResponse(run *Run) RunResponse {
	return RunResponse{
		JobID: run.JobID, RunID: run.RunID, Host: run.Host, Kind: run.Kind, Engine: run.Engine,
		Runner: run.Runner, State: string(run.State), Terminal: run.IsTerminal(), Phase: run.Phase,
		StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt, FinishedAt: run.FinishedAt, ExitCode: run.ExitCode,
		Error: run.Error, Reason: run.Reason, Warnings: nonNil(run.Warnings), Notices: nonNil(run.Notices),
		Target: run.Target, ArchiveID: run.ArchiveID, ArchiveName: run.ArchiveName,
		OriginalBytes: run.Stats.OriginalBytes, CompressedBytes: run.Stats.CompressedBytes,
		DeduplicatedBytes: run.Stats.DeduplicatedBytes, FileCount: run.Stats.FileCount,
		DurationSeconds: run.DurationSeconds, Complete: run.Complete, LogPath: run.LogPath,
		ManifestPath: run.ManifestPath, RunDirectory: run.RunDirectory, RecoveryPointer: run.RecoveryPointer,
		ReceivedAt: run.ReceivedAt, ReportCount: run.ReportCount,
	}
}

func toOptionalRunResponse(run *Run) *RunResponse {
	if run == nil {
		return nil
	}
	response := toRunResponse(run)
	return &response
}

func toRunResponses(runs []*Run) []RunResponse {
	responses := make([]RunResponse, 0, len(runs))
	for _, run := range runs {
		responses = append(responses, toRunResponse(run))
	}
	return responses
}

func toRunDetailResponse(run *Run) RunDetailResponse {
	report := json.RawMessage(run.RawReport)
	if !json.Valid(report) {
		report = json.RawMessage("null")
	}
	return RunDetailResponse{Run: toRunResponse(run), Report: report}
}

func toVerificationResponse(verification *Verification) VerificationResponse {
	return VerificationResponse{
		JobID: verification.JobID, VerificationID: verification.VerificationID, RunID: verification.RunID,
		Kind: string(verification.Kind), CheckType: verification.CheckType, State: string(verification.State),
		At: verification.At, Scope: verification.Scope, Archive: verification.Archive,
		FullDataRead: verification.FullDataRead, EvidencePath: verification.EvidencePath,
		Checked: nonNil(verification.Checked), ReceivedAt: verification.ReceivedAt,
	}
}

func toOptionalVerificationResponse(verification *Verification) *VerificationResponse {
	if verification == nil {
		return nil
	}
	response := toVerificationResponse(verification)
	return &response
}

func toVerificationResponses(verifications []*Verification) []VerificationResponse {
	responses := make([]VerificationResponse, 0, len(verifications))
	for _, verification := range verifications {
		responses = append(responses, toVerificationResponse(verification))
	}
	return responses
}

func toJobStatusResponse(view JobView) JobStatusResponse {
	job := view.Job
	response := JobStatusResponse{
		JobID: job.JobID, HostID: job.HostID, DisplayName: job.DisplayName, Engine: job.Engine, Kind: job.Kind,
		Source: job.Source, Scope: job.Scope, Target: job.Target,
		HeartbeatStaleAfterSeconds: int64(job.HeartbeatStaleAfter / time.Second),
		Retention:                  job.Retention, RecoveryPointer: job.RecoveryPointer, Notes: job.Notes,
		Registered: job.Registered, Enabled: job.Enabled,
		Status: string(view.Status), StatusReason: view.StatusReason, Freshness: string(view.Freshness),
		ExpectedBy: view.ExpectedBy, NextDueAt: view.NextDueAt, EvaluatedAt: view.EvaluatedAt,
		CurrentRun:       toOptionalRunResponse(view.CurrentRun),
		LastAttempt:      toOptionalRunResponse(view.Runs.LastAttempt),
		LastFinished:     toOptionalRunResponse(view.Runs.LastFinished),
		LastSuccess:      toOptionalRunResponse(view.Runs.LastSuccess),
		LastArchive:      toOptionalRunResponse(view.Runs.LastArchive),
		LastVerification: toOptionalVerificationResponse(view.Runs.LastVerification),
		LastRestoreTest:  toOptionalVerificationResponse(view.Runs.LastRestoreTest),
	}
	if job.Schedule != nil {
		response.Schedule = &ScheduleResponse{
			Timezone: job.Schedule.Timezone(), Times: job.Schedule.Times(), Weekdays: job.Schedule.Weekdays(),
			GraceSeconds: int64(job.Schedule.Grace() / time.Second),
		}
	}
	if view.Heartbeat != nil {
		state := string(*view.Heartbeat)
		age := int64(view.EvaluatedAt.Sub(view.CurrentRun.UpdatedAt) / time.Second)
		response.HeartbeatState, response.HeartbeatAgeSeconds = &state, &age
	}
	return response
}

func toJobStatusResponses(views []JobView) []JobStatusResponse {
	responses := make([]JobStatusResponse, 0, len(views))
	for _, view := range views {
		responses = append(responses, toJobStatusResponse(view))
	}
	return responses
}
