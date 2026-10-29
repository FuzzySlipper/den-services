package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// LedgerStore is the persistence the Ops service needs.
type LedgerStore interface {
	UpsertHost(ctx context.Context, host *Host) error
	ListHosts(ctx context.Context) ([]*Host, error)
	UpsertJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, jobID string) (*Job, error)
	ListJobs(ctx context.Context) ([]*Job, error)
	GetRun(ctx context.Context, jobID string, runID string) (*Run, error)
	SaveReport(ctx context.Context, implicitJob *Job, run *Run, verifications []*Verification) error
	ListRuns(ctx context.Context, jobID string, limit int) ([]*Run, error)
	ListProblemRuns(ctx context.Context, limit int) ([]*Run, error)
	LoadJobRuns(ctx context.Context, jobID string) (JobRuns, error)
	GetVerification(ctx context.Context, jobID string, verificationID string) (*Verification, error)
	ListVerifications(ctx context.Context, jobID string, limit int) ([]*Verification, error)
	ListFailedVerifications(ctx context.Context, limit int) ([]*Verification, error)
}

// Service records backup observations and judges them. Nothing here starts,
// retries or wakes anything: a stored report is only ever read.
type Service struct {
	store    LedgerStore
	now      func() time.Time
	defaults DefaultsConfig
	// writes serializes read-decide-write report handling in this process.
	writes sync.Mutex
}

func NewService(store LedgerStore, now func() time.Time, defaults DefaultsConfig) *Service {
	return &Service{store: store, now: now, defaults: defaults}
}

type HostView struct {
	Host       *Host
	Registered bool
	JobCount   int
	Configured bool
	Status     JobStatus
}

type Summary struct {
	GeneratedAt time.Time
	Status      JobStatus
	Counts      map[JobStatus]int
	Hosts       []HostView
	Jobs        []JobView
}

type JobDetail struct {
	View          JobView
	Runs          []*Run
	Verifications []*Verification
}

type ErrorsView struct {
	GeneratedAt         time.Time
	Problems            []JobView
	ProblemRuns         []*Run
	FailedVerifications []*Verification
}

func (s *Service) RegisterHost(ctx context.Context, hostID string, request RegisterHostRequest) (*Host, error) {
	if err := validateID("host_id", hostID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	host := &Host{HostID: hostID, DisplayName: request.DisplayName, Role: request.Role, Notes: request.Notes, CreatedAt: now, UpdatedAt: now}
	if host.DisplayName == "" {
		host.DisplayName = hostID
	}
	hosts, err := s.store.ListHosts(ctx)
	if err != nil {
		return nil, err
	}
	if index := slices.IndexFunc(hosts, func(existing *Host) bool { return existing.HostID == hostID }); index >= 0 {
		host.CreatedAt = hosts[index].CreatedAt
	}
	if err := s.store.UpsertHost(ctx, host); err != nil {
		return nil, err
	}
	return host, nil
}

func (s *Service) Hosts(ctx context.Context) ([]HostView, error) {
	summary, err := s.Summary(ctx)
	if err != nil {
		return nil, err
	}
	return summary.Hosts, nil
}

func (s *Service) RegisterJob(ctx context.Context, jobID string, request RegisterJobRequest) (JobView, error) {
	if err := validateID("job_id", jobID); err != nil {
		return JobView{}, err
	}
	if err := request.Validate(); err != nil {
		return JobView{}, err
	}
	staleAfter := s.defaults.HeartbeatStaleAfter
	if request.HeartbeatStaleAfter != "" {
		parsed, err := time.ParseDuration(request.HeartbeatStaleAfter)
		if err != nil || parsed <= 0 {
			return JobView{}, validationFailed("heartbeat_stale_after must be a positive duration such as 15m")
		}
		staleAfter = parsed
	}
	var schedule *Schedule
	if request.Schedule != nil {
		grace, err := time.ParseDuration(request.Schedule.Grace)
		if err != nil {
			return JobView{}, validationFailed("schedule.grace must be a duration such as 3h")
		}
		if schedule, err = NewSchedule(request.Schedule.Timezone, request.Schedule.Times, request.Schedule.Weekdays, grace); err != nil {
			return JobView{}, err
		}
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	now := s.now().UTC()
	job := &Job{
		JobID: jobID, HostID: request.HostID, DisplayName: request.DisplayName, Engine: request.Engine,
		Kind: request.Kind, Source: request.Source, Scope: request.Scope, Target: request.Target,
		Schedule: schedule, HeartbeatStaleAfter: staleAfter, Retention: request.Retention,
		RecoveryPointer: request.RecoveryPointer, Notes: request.Notes, Registered: true,
		Enabled: request.Enabled == nil || *request.Enabled, CreatedAt: now, UpdatedAt: now,
	}
	if job.DisplayName == "" {
		job.DisplayName = jobID
	}
	existing, err := s.store.GetJob(ctx, jobID)
	switch {
	case err == nil:
		job.CreatedAt = existing.CreatedAt
	case !errors.Is(err, ErrJobNotFound):
		return JobView{}, err
	}
	if err := s.store.UpsertJob(ctx, job); err != nil {
		return JobView{}, err
	}
	return s.evaluate(ctx, job, now)
}

// ReportRun stores one lifecycle report. Delivery is idempotent by job and
// run ID; see DecideReport for how repeats, replays and late heartbeats land.
func (s *Service) ReportRun(ctx context.Context, jobID string, runID string, request RunReportRequest, raw []byte) (ReportOutcome, *Run, JobView, error) {
	s.writes.Lock()
	defer s.writes.Unlock()
	now := s.now().UTC()
	incoming, err := runFromReport(jobID, runID, request, raw, now)
	if err != nil {
		return "", nil, JobView{}, err
	}
	job, err := s.store.GetJob(ctx, jobID)
	var implicitJob *Job
	switch {
	case errors.Is(err, ErrJobNotFound):
		// Keep reports for jobs nobody registered; they show as unregistered.
		implicitJob = s.implicitJob(jobID, request, now)
		job = implicitJob
	case err != nil:
		return "", nil, JobView{}, err
	}
	if incoming.Host == "" {
		incoming.Host = job.HostID
	}
	existing, err := s.store.GetRun(ctx, jobID, runID)
	if err != nil && !errors.Is(err, ErrRunNotFound) {
		return "", nil, JobView{}, err
	}
	outcome := DecideReport(existing, incoming)
	stored := existing
	if outcome == OutcomeCreated || outcome == OutcomeUpdated {
		incoming.ReportCount = 1
		if existing != nil {
			incoming.ReportCount = existing.ReportCount + 1
		}
		verifications, err := embeddedVerifications(incoming, request, now)
		if err != nil {
			return "", nil, JobView{}, err
		}
		if err := s.store.SaveReport(ctx, implicitJob, incoming, verifications); err != nil {
			return "", nil, JobView{}, err
		}
		stored = incoming
	}
	view, err := s.evaluate(ctx, job, now)
	if err != nil {
		return "", nil, JobView{}, err
	}
	return outcome, stored, view, nil
}

func (s *Service) implicitJob(jobID string, request RunReportRequest, now time.Time) *Job {
	hostID := request.Host
	if validateID("host", hostID) != nil {
		hostID = "unknown"
	}
	return &Job{
		JobID: jobID, HostID: hostID, DisplayName: jobID, Engine: request.Engine, Kind: request.Kind,
		Target: request.Target, HeartbeatStaleAfter: s.defaults.HeartbeatStaleAfter,
		Notes: "created from a report; register this job to declare its schedule", Registered: false,
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
}

func runFromReport(jobID string, runID string, request RunReportRequest, raw []byte, now time.Time) (*Run, error) {
	if err := validateID("job_id", jobID); err != nil {
		return nil, err
	}
	if err := validateID("run_id", runID); err != nil {
		return nil, err
	}
	if (request.JobID != "" && request.JobID != jobID) || (request.RunID != "" && request.RunID != runID) {
		return nil, validationFailed("report job_id/run_id %q/%q do not match %q/%q", request.JobID, request.RunID, jobID, runID)
	}
	state := RunState(request.State)
	if !state.IsValid() {
		return nil, validationFailed("state %q must be one of running, success, warning, failed, skipped", request.State)
	}
	if request.StartedAt == nil {
		return nil, validationFailed("started_at is required")
	}
	if request.FinishedAt != nil && state == RunRunning {
		return nil, validationFailed("a report with finished_at needs a terminal state")
	}
	if request.FinishedAt == nil {
		// Without finished_at the run is still going, even if a phase so far
		// looked successful (Borg reports "success" while pruning).
		state = RunRunning
	}
	run := &Run{
		JobID: jobID, RunID: runID, Host: request.Host, Kind: request.Kind, Engine: request.Engine,
		Runner: request.Runner, State: state, Phase: request.Phase, StartedAt: request.StartedAt.UTC(),
		FinishedAt: utcPointer(request.FinishedAt), ExitCode: request.ExitCode, Error: request.Error,
		Reason: request.Reason, Warnings: request.Warnings, Notices: request.SourceNotices, Target: request.Target,
		LogPath: request.LogPath, ManifestPath: request.Manifest, RunDirectory: request.LocalRunDirectory,
		RecoveryPointer: request.RecoveryPointer, DurationSeconds: request.DurationSeconds,
		RawReport: raw, ReceivedAt: now,
	}
	run.UpdatedAt = run.StartedAt
	switch {
	case request.UpdatedAt != nil:
		run.UpdatedAt = request.UpdatedAt.UTC()
	case run.FinishedAt != nil:
		run.UpdatedAt = *run.FinishedAt
	}
	stats := request.Stats
	if request.Archive != nil {
		run.ArchiveID, run.ArchiveName = request.Archive.ID, request.Archive.Name
		if stats == nil {
			stats = request.Archive.Stats
		}
		if run.DurationSeconds == nil {
			run.DurationSeconds = request.Archive.Duration
		}
	}
	if stats != nil {
		run.Stats = ArchiveStats{
			OriginalBytes: stats.OriginalSize, CompressedBytes: stats.CompressedSize,
			DeduplicatedBytes: stats.DeduplicatedSize, FileCount: stats.NFiles,
		}
	}
	if run.DurationSeconds == nil && run.FinishedAt != nil {
		seconds := run.FinishedAt.Sub(run.StartedAt).Seconds()
		run.DurationSeconds = &seconds
	}
	switch {
	case state == RunSuccess || state == RunWarning:
		run.Complete = state == RunSuccess
		if request.BackupComplete != nil {
			run.Complete = *request.BackupComplete
		}
	default:
		run.Complete = false
	}
	return run, nil
}

func embeddedVerifications(run *Run, request RunReportRequest, now time.Time) ([]*Verification, error) {
	var verifications []*Verification
	embedded := []struct {
		report *VerificationReport
		kind   VerificationKind
	}{{request.Verification, VerificationCheck}, {request.RestoreTest, VerificationRestoreTest}}
	for _, item := range embedded {
		if item.report == nil {
			continue
		}
		report := *item.report
		if report.At == nil {
			report.At = run.FinishedAt
		}
		if report.RunID == "" {
			report.RunID = run.RunID
		}
		embeddedRaw, err := json.Marshal(item.report)
		if err != nil {
			return nil, fmt.Errorf("encoding embedded %s: %w", item.kind, err)
		}
		verification, err := verificationFromReport(run.JobID, run.RunID+":"+string(item.kind), report, embeddedRaw, now)
		if err != nil {
			return nil, fmt.Errorf("embedded %s: %w", item.kind, err)
		}
		verification.Kind = item.kind
		verifications = append(verifications, verification)
	}
	return verifications, nil
}

func verificationFromReport(jobID string, verificationID string, report VerificationReport, raw []byte, now time.Time) (*Verification, error) {
	if err := validateID("verification_id", verificationID); err != nil {
		return nil, err
	}
	state := VerificationState(report.State)
	if !state.IsValid() {
		return nil, validationFailed("verification state %q must be success, warning or failed", report.State)
	}
	if report.At == nil {
		return nil, validationFailed("verification at is required")
	}
	kind, checkType := VerificationCheck, report.Type
	switch report.Kind {
	case string(VerificationRestoreTest):
		kind = VerificationRestoreTest
	case "", string(VerificationCheck):
	default:
		if checkType == "" {
			checkType = report.Kind
		}
	}
	evidence := report.EvidencePath
	if evidence == "" {
		evidence = report.RestoredTo
	}
	checked := slices.Concat(report.Checked, report.RepositoriesChecked, report.FilesCompared)
	return &Verification{
		JobID: jobID, VerificationID: verificationID, RunID: report.RunID, Kind: kind, CheckType: checkType,
		State: state, At: report.At.UTC(), Scope: report.Scope, Archive: report.Archive,
		FullDataRead: report.FullDataRead, EvidencePath: evidence, Checked: checked, RawReport: raw, ReceivedAt: now,
	}, nil
}

// ReportVerification stores a standalone check or restore test. Repeating it
// is harmless; an older copy never replaces a newer one.
func (s *Service) ReportVerification(ctx context.Context, jobID string, verificationID string, report VerificationReport, raw []byte) (ReportOutcome, *Verification, error) {
	if err := validateID("job_id", jobID); err != nil {
		return "", nil, err
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	now := s.now().UTC()
	verification, err := verificationFromReport(jobID, verificationID, report, raw, now)
	if err != nil {
		return "", nil, err
	}
	if _, err := s.store.GetJob(ctx, jobID); err != nil {
		if errors.Is(err, ErrJobNotFound) {
			return "", nil, notFound(err, "job_not_found", jobID)
		}
		return "", nil, err
	}
	existing, err := s.store.GetVerification(ctx, jobID, verificationID)
	outcome := OutcomeCreated
	switch {
	case errors.Is(err, ErrVerificationNotFound):
	case err != nil:
		return "", nil, err
	case verification.At.Before(existing.At):
		return OutcomeIgnoredStale, existing, nil
	case verification.At.Equal(existing.At) && verification.State == existing.State:
		return OutcomeDuplicate, existing, nil
	default:
		outcome = OutcomeUpdated
	}
	if err := s.store.SaveReport(ctx, nil, nil, []*Verification{verification}); err != nil {
		return "", nil, err
	}
	return outcome, verification, nil
}

func (s *Service) Jobs(ctx context.Context) ([]JobView, error) {
	jobs, err := s.store.ListJobs(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	views := make([]JobView, 0, len(jobs))
	for _, job := range jobs {
		view, err := s.evaluate(ctx, job, now)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	views, err := s.Jobs(ctx)
	if err != nil {
		return Summary{}, err
	}
	hosts, err := s.store.ListHosts(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{GeneratedAt: s.now().UTC(), Status: StatusUnknown, Counts: map[JobStatus]int{}, Jobs: views}
	for _, status := range AllJobStatuses() {
		summary.Counts[status] = 0
	}
	byHost := map[string]*HostView{}
	for _, host := range hosts {
		byHost[host.HostID] = &HostView{Host: host, Registered: true, Status: StatusUnknown}
	}
	enabled := 0
	for _, view := range views {
		summary.Counts[view.Status]++
		host, ok := byHost[view.Job.HostID]
		if !ok {
			host = &HostView{Host: &Host{HostID: view.Job.HostID, DisplayName: view.Job.HostID}, Status: StatusUnknown}
			byHost[view.Job.HostID] = host
		}
		host.JobCount++
		if !view.Job.Enabled {
			continue
		}
		if !host.Configured || worse(view.Status, host.Status) {
			host.Status = view.Status
		}
		host.Configured = true
		if enabled == 0 || worse(view.Status, summary.Status) {
			summary.Status = view.Status
		}
		enabled++
	}
	for _, host := range byHost {
		summary.Hosts = append(summary.Hosts, *host)
	}
	slices.SortFunc(summary.Hosts, func(a, b HostView) int { return strings.Compare(a.Host.HostID, b.Host.HostID) })
	return summary, nil
}

func (s *Service) JobDetail(ctx context.Context, jobID string, limit int) (JobDetail, error) {
	job, err := s.getJob(ctx, jobID)
	if err != nil {
		return JobDetail{}, err
	}
	view, err := s.evaluate(ctx, job, s.now().UTC())
	if err != nil {
		return JobDetail{}, err
	}
	runs, err := s.store.ListRuns(ctx, jobID, limit)
	if err != nil {
		return JobDetail{}, err
	}
	verifications, err := s.store.ListVerifications(ctx, jobID, limit)
	if err != nil {
		return JobDetail{}, err
	}
	return JobDetail{View: view, Runs: runs, Verifications: verifications}, nil
}

func (s *Service) Runs(ctx context.Context, jobID string, limit int) ([]*Run, error) {
	if _, err := s.getJob(ctx, jobID); err != nil {
		return nil, err
	}
	return s.store.ListRuns(ctx, jobID, limit)
}

func (s *Service) Run(ctx context.Context, jobID string, runID string) (*Run, error) {
	run, err := s.store.GetRun(ctx, jobID, runID)
	if errors.Is(err, ErrRunNotFound) {
		return nil, notFound(err, "run_not_found", jobID+"/"+runID)
	}
	return run, err
}

func (s *Service) Verifications(ctx context.Context, jobID string, limit int) ([]*Verification, error) {
	if _, err := s.getJob(ctx, jobID); err != nil {
		return nil, err
	}
	return s.store.ListVerifications(ctx, jobID, limit)
}

// Errors lists what needs attention: jobs not currently ok, plus recent failed
// or warning runs and checks that did not pass.
func (s *Service) Errors(ctx context.Context, limit int) (ErrorsView, error) {
	views, err := s.Jobs(ctx)
	if err != nil {
		return ErrorsView{}, err
	}
	result := ErrorsView{GeneratedAt: s.now().UTC()}
	for _, view := range views {
		if view.Status != StatusOK && view.Status != StatusRunning && view.Status != StatusDisabled {
			result.Problems = append(result.Problems, view)
		}
	}
	slices.SortStableFunc(result.Problems, func(a, b JobView) int { return severity(a.Status) - severity(b.Status) })
	if result.ProblemRuns, err = s.store.ListProblemRuns(ctx, limit); err != nil {
		return ErrorsView{}, err
	}
	if result.FailedVerifications, err = s.store.ListFailedVerifications(ctx, limit); err != nil {
		return ErrorsView{}, err
	}
	return result, nil
}

func (s *Service) getJob(ctx context.Context, jobID string) (*Job, error) {
	job, err := s.store.GetJob(ctx, jobID)
	if errors.Is(err, ErrJobNotFound) {
		return nil, notFound(err, "job_not_found", jobID)
	}
	return job, err
}

func (s *Service) evaluate(ctx context.Context, job *Job, now time.Time) (JobView, error) {
	runs, err := s.store.LoadJobRuns(ctx, job.JobID)
	if err != nil {
		return JobView{}, err
	}
	return EvaluateJob(job, runs, now), nil
}

func severity(status JobStatus) int { return slices.Index(AllJobStatuses(), status) }

func worse(candidate JobStatus, current JobStatus) bool {
	return severity(candidate) < severity(current)
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
