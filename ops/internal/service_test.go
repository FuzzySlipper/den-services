package ops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

func newTestService(t *testing.T, path string, clock *fixedClock) (*Service, *Store) {
	t.Helper()
	store, err := OpenStore(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewService(store, clock.Now, DefaultsConfig{HeartbeatStaleAfter: 15 * time.Minute, RunListLimit: 50}), store
}

func registerBorgJob(t *testing.T, service *Service) {
	t.Helper()
	_, err := service.RegisterJob(context.Background(), "den-agents-git", RegisterJobRequest{
		HostID: "den-agents", DisplayName: "den-agents Git projects", Engine: "borg", Kind: "backup",
		Source: "den-agents:/home/agent,/data,/opt,/usr/local/src", Scope: "Git repositories and working trees",
		Target:    "den-nas:/mnt/storage/backup/den-agents/borg",
		Schedule:  &ScheduleRequest{Timezone: "America/Los_Angeles", Times: []string{"07:00"}, Grace: "3h"},
		Retention: "30 daily, 12 weekly, 12 monthly, 3 yearly", RecoveryPointer: "/mnt/storage/backup/den-agents/recovery/",
	})
	if err != nil {
		t.Fatalf("RegisterJob: %v", err)
	}
}

func fixture(t *testing.T, name string) (RunReportRequest, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var request RunReportRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	return request, raw
}

func report(t *testing.T, service *Service, request RunReportRequest, raw []byte) (ReportOutcome, *Run, JobView) {
	t.Helper()
	outcome, run, view, err := service.ReportRun(context.Background(), request.JobID, request.RunID, request, raw)
	if err != nil {
		t.Fatalf("ReportRun %s: %v", request.RunID, err)
	}
	return outcome, run, view
}

func TestRealBorgReportIsStoredWithArchiveEvidence(t *testing.T) {
	clock := &fixedClock{now: utc("2026-10-29T05:00:00Z")}
	service, _ := newTestService(t, filepath.Join(t.TempDir(), "ops.sqlite3"), clock)
	registerBorgJob(t, service)
	request, raw := fixture(t, "borg-backup-success.json")

	outcome, run, view := report(t, service, request, raw)
	if outcome != OutcomeCreated || run.State != RunSuccess || !run.Complete {
		t.Fatalf("outcome/state/complete = %s/%s/%v", outcome, run.State, run.Complete)
	}
	if run.ArchiveID != "e7f64bce379d2b12d5227bffdc6ae7aa8d91ca4c82542f2ac9ea07901c3120f0" || run.ArchiveName != "den-agents-git-2026-10-29T04-24-43" {
		t.Fatalf("archive = %s %s", run.ArchiveID, run.ArchiveName)
	}
	if run.Stats.DeduplicatedBytes == nil || *run.Stats.DeduplicatedBytes != 132660210 || run.DurationSeconds == nil {
		t.Fatalf("stats = %+v duration %v", run.Stats, run.DurationSeconds)
	}
	if run.ManifestPath == "" || run.RunDirectory == "" || len(run.Notices) != 2 {
		t.Fatalf("pointers/notices missing: %+v", run)
	}
	if view.Status != StatusOK || view.Runs.LastSuccess == nil || view.Runs.LastArchive == nil {
		t.Fatalf("view = %s %s", view.Status, view.StatusReason)
	}
	stored, err := service.Run(context.Background(), "den-agents-git", request.RunID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(stored.RawReport) != string(raw) {
		t.Fatalf("raw report was not kept verbatim")
	}
}

func TestDuplicateAndLateReportsAreIdempotent(t *testing.T) {
	clock := &fixedClock{now: utc("2026-10-29T04:28:00Z")}
	service, _ := newTestService(t, filepath.Join(t.TempDir(), "ops.sqlite3"), clock)
	registerBorgJob(t, service)
	heartbeat, heartbeatRaw := fixture(t, "borg-backup-prune-heartbeat.json")
	final, finalRaw := fixture(t, "borg-backup-success.json")

	// Borg reports "success" while pruning; without finished_at it is still running.
	outcome, run, view := report(t, service, heartbeat, heartbeatRaw)
	if outcome != OutcomeCreated || run.State != RunRunning || view.Status != StatusRunning {
		t.Fatalf("heartbeat: %s %s %s", outcome, run.State, view.Status)
	}
	if outcome, _, _ = report(t, service, final, finalRaw); outcome != OutcomeUpdated {
		t.Fatalf("terminal: %s", outcome)
	}
	if outcome, _, _ = report(t, service, final, finalRaw); outcome != OutcomeDuplicate {
		t.Fatalf("redelivery: %s", outcome)
	}
	outcome, run, view = report(t, service, heartbeat, heartbeatRaw)
	if outcome != OutcomeIgnoredTerminal || run.State != RunSuccess || view.Status != StatusOK {
		t.Fatalf("late heartbeat: %s %s %s", outcome, run.State, view.Status)
	}
	runs, err := service.Runs(context.Background(), "den-agents-git", 10)
	if err != nil || len(runs) != 1 || runs[0].ReportCount != 2 {
		t.Fatalf("runs = %d (count %d), err %v", len(runs), runs[0].ReportCount, err)
	}
}

func TestFailurePreservesPriorSuccess(t *testing.T) {
	clock := &fixedClock{now: utc("2026-10-29T18:00:00Z")}
	service, _ := newTestService(t, filepath.Join(t.TempDir(), "ops.sqlite3"), clock)
	registerBorgJob(t, service)
	success, successRaw := fixture(t, "borg-backup-success.json")
	report(t, service, success, successRaw)

	started, finished, code := utc("2026-10-29T14:00:02Z"), utc("2026-10-29T14:03:00Z"), 2
	failure := RunReportRequest{
		JobID: "den-agents-git", RunID: "20261029T070002-99", Host: "den-agents", Engine: "borg", Kind: "backup",
		State: "failed", StartedAt: &started, FinishedAt: &finished, ExitCode: &code,
		Error: "Failed to create/acquire the lock /run/lock/snapraid-maintenance.lock",
	}
	_, run, view := report(t, service, failure, []byte(`{}`))
	if run.Complete || view.Status != StatusFailed {
		t.Fatalf("failure: complete=%v status=%s", run.Complete, view.Status)
	}
	if view.Runs.LastSuccess == nil || view.Runs.LastSuccess.RunID != success.RunID {
		t.Fatalf("last success lost: %+v", view.Runs.LastSuccess)
	}
	if view.Runs.LastAttempt.RunID != failure.RunID {
		t.Fatalf("last attempt = %s", view.Runs.LastAttempt.RunID)
	}
	errorsView, err := service.Errors(context.Background(), 10)
	if err != nil || len(errorsView.Problems) != 1 || len(errorsView.ProblemRuns) != 1 {
		t.Fatalf("errors view = %+v, %v", errorsView, err)
	}
}

func TestMaintenanceReportRecordsVerificationAndRestoreTest(t *testing.T) {
	clock := &fixedClock{now: utc("2026-10-31T17:00:00Z")}
	service, _ := newTestService(t, filepath.Join(t.TempDir(), "ops.sqlite3"), clock)
	registerBorgJob(t, service)
	request, raw := fixture(t, "borg-maintenance.json")
	report(t, service, request, raw)

	detail, err := service.JobDetail(context.Background(), "den-agents-git", 10)
	if err != nil {
		t.Fatalf("JobDetail: %v", err)
	}
	check, restore := detail.View.Runs.LastVerification, detail.View.Runs.LastRestoreTest
	if check == nil || check.CheckType != "repository-and-latest-archive" || check.FullDataRead == nil || !*check.FullDataRead {
		t.Fatalf("verification = %+v", check)
	}
	if restore == nil || restore.Kind != VerificationRestoreTest || restore.Archive != "den-agents-git-2026-10-29T04-24-43" || restore.EvidencePath == "" {
		t.Fatalf("restore test = %+v", restore)
	}
	// A maintenance run is not a backup and must not count as a restore point.
	if detail.View.Runs.LastArchive != nil {
		t.Fatalf("maintenance produced an archive: %+v", detail.View.Runs.LastArchive)
	}
}

func TestStandaloneVerificationIsIdempotent(t *testing.T) {
	clock := &fixedClock{now: utc("2026-10-29T05:00:00Z")}
	service, _ := newTestService(t, filepath.Join(t.TempDir(), "ops.sqlite3"), clock)
	registerBorgJob(t, service)
	raw, err := os.ReadFile(filepath.Join("testdata", "borg-maintenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var maintenance struct {
		RestoreTest VerificationReport `json:"restore_test"`
	}
	if err := json.Unmarshal(raw, &maintenance); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, _, err := service.ReportVerification(ctx, "den-agents-git", "restore-20261029T043010Z", maintenance.RestoreTest, raw)
	if err != nil || first != OutcomeCreated {
		t.Fatalf("first = %s, %v", first, err)
	}
	second, _, err := service.ReportVerification(ctx, "den-agents-git", "restore-20261029T043010Z", maintenance.RestoreTest, raw)
	if err != nil || second != OutcomeDuplicate {
		t.Fatalf("second = %s, %v", second, err)
	}
	if _, _, err := service.ReportVerification(ctx, "nope", "x", maintenance.RestoreTest, raw); err == nil {
		t.Fatalf("verification for an unknown job was accepted")
	}
}

func TestReportForUnregisteredJobIsKept(t *testing.T) {
	clock := &fixedClock{now: utc("2026-10-29T05:00:00Z")}
	service, _ := newTestService(t, filepath.Join(t.TempDir(), "ops.sqlite3"), clock)
	request, raw := fixture(t, "borg-backup-success.json")
	_, _, view := report(t, service, request, raw)
	if view.Job.Registered || view.Job.HostID != "den-agents" || view.Freshness != FreshnessUnscheduled {
		t.Fatalf("implicit job = %+v", view.Job)
	}
	// Registering afterwards adds the schedule and keeps the run.
	registerBorgJob(t, service)
	detail, err := service.JobDetail(context.Background(), "den-agents-git", 10)
	if err != nil || !detail.View.Job.Registered || len(detail.Runs) != 1 {
		t.Fatalf("after register: %+v %v", detail.View.Job, err)
	}
}

func TestRejectsInvalidReports(t *testing.T) {
	clock := &fixedClock{now: utc("2026-10-29T05:00:00Z")}
	service, _ := newTestService(t, filepath.Join(t.TempDir(), "ops.sqlite3"), clock)
	started, finished := utc("2026-10-29T04:00:00Z"), utc("2026-10-29T04:10:00Z")
	cases := []struct {
		name    string
		jobID   string
		runID   string
		request RunReportRequest
	}{
		{"bad state", "j", "r", RunReportRequest{State: "great", StartedAt: &started}},
		{"missing start", "j", "r", RunReportRequest{State: "running"}},
		{"finished but running", "j", "r", RunReportRequest{State: "running", StartedAt: &started, FinishedAt: &finished}},
		{"body id mismatch", "j", "r", RunReportRequest{RunID: "other", State: "running", StartedAt: &started}},
		{"bad job id", "../etc", "r", RunReportRequest{State: "running", StartedAt: &started}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := service.ReportRun(context.Background(), tc.jobID, tc.runID, tc.request, []byte(`{}`)); err == nil {
				t.Fatalf("accepted %+v", tc.request)
			}
		})
	}
}

func TestLedgerSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "ops.sqlite3")
	clock := &fixedClock{now: utc("2026-10-29T05:00:00Z")}
	ctx := context.Background()
	first, firstStore := newTestService(t, path, clock)
	registerBorgJob(t, first)
	request, raw := fixture(t, "borg-backup-success.json")
	report(t, first, request, raw)
	if _, err := first.RegisterHost(ctx, "den-win11", RegisterHostRequest{Role: "windows test box"}); err != nil {
		t.Fatal(err)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	second, _ := newTestService(t, path, clock)
	summary, err := second.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if len(summary.Jobs) != 1 || summary.Jobs[0].Status != StatusOK || summary.Jobs[0].Runs.LastSuccess == nil {
		t.Fatalf("jobs after reopen = %+v", summary.Jobs)
	}
	if summary.Jobs[0].Job.Schedule == nil || summary.Jobs[0].Job.Schedule.Timezone() != "America/Los_Angeles" {
		t.Fatalf("schedule lost: %+v", summary.Jobs[0].Job.Schedule)
	}
	// A registered host with no job is shown as not configured, not as healthy.
	if len(summary.Hosts) != 2 || summary.Hosts[1].Host.HostID != "den-win11" || summary.Hosts[1].Configured || summary.Hosts[1].Status != StatusUnknown {
		t.Fatalf("hosts = %+v", summary.Hosts)
	}
	if summary.Status != StatusOK {
		t.Fatalf("overall = %s", summary.Status)
	}
}
