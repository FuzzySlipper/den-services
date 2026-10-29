package ops

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func newTestBackup(t *testing.T, directory string, clock *fixedClock) (*Backup, *Service) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "ops.sqlite3")
	service, store := newTestService(t, databasePath, clock)
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	config := BackupConfig{Directory: directory, At: "05:30", Location: location, Keep: 2, JobID: "ops-sqlite-export", HostID: "den-services"}
	return NewBackup(store, service, config, databasePath, DefaultsConfig{HeartbeatStaleAfter: 15 * time.Minute, RunListLimit: 50}, clock.Now), service
}

func TestBackupExportsRestoreTestsAndRecordsItself(t *testing.T) {
	clock := &fixedClock{now: time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC)}
	directory := filepath.Join(t.TempDir(), "daily")
	backup, service := newTestBackup(t, directory, clock)
	registerBorgJob(t, service)
	request, raw := fixture(t, "borg-backup-success.json")
	report(t, service, request, raw)
	if err := backup.Register(context.Background()); err != nil {
		t.Fatal(err)
	}

	result, err := backup.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v (%v)", err, result)
	}
	if result["state"] != "success" {
		t.Fatalf("report = %v", result)
	}
	if _, err := os.Stat(filepath.Join(directory, "ops-20261029T123000Z.sqlite3")); err != nil {
		t.Fatalf("export missing: %v", err)
	}
	restore := result["restore_test"].(map[string]any)
	checked := strings.Join(restore["checked"].([]string), "; ")
	if restore["state"] != "success" || !strings.Contains(checked, "integrity_check ok") || !strings.Contains(checked, "2 jobs, 1 runs") {
		t.Fatalf("restore test = %v", restore)
	}
	leftovers, _ := filepath.Glob(filepath.Join(directory, ".*"))
	if len(leftovers) != 0 {
		t.Fatalf("scratch files left behind: %v", leftovers)
	}

	detail, err := service.JobDetail(context.Background(), "ops-sqlite-export", 10)
	if err != nil {
		t.Fatal(err)
	}
	if detail.View.Status != StatusOK || detail.View.Runs.LastSuccess == nil || detail.View.Runs.LastSuccess.ArchiveName != "ops-20261029T123000Z.sqlite3" {
		t.Fatalf("backup job = %+v", detail.View)
	}
	if detail.View.Runs.LastRestoreTest == nil || detail.View.Runs.LastRestoreTest.State != VerificationSuccess {
		t.Fatalf("restore test not recorded: %+v", detail.View.Runs.LastRestoreTest)
	}
}

func TestBackupKeepsNewestExports(t *testing.T) {
	clock := &fixedClock{now: time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC)}
	directory := filepath.Join(t.TempDir(), "daily")
	backup, _ := newTestBackup(t, directory, clock)
	if err := backup.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	var lastReport map[string]any
	for day := 0; day < 3; day++ {
		clock.now = clock.now.Add(24 * time.Hour)
		var err error
		if lastReport, err = backup.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(directory)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	want := []string{"ops-20261031T123000Z.sqlite3", "ops-20261101T123000Z.sqlite3"}
	if !slices.Equal(names, want) || !slices.Equal(lastReport["pruned"].([]string), []string{"ops-20261030T123000Z.sqlite3"}) {
		t.Fatalf("exports = %v, pruned = %v", names, lastReport["pruned"])
	}
}

func TestBackupFailureIsRecordedNotHidden(t *testing.T) {
	clock := &fixedClock{now: time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC)}
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, service := newTestBackup(t, blocker, clock)
	if err := backup.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := backup.Run(context.Background())
	if err == nil || result["state"] != "failed" {
		t.Fatalf("Run = %v, %v", result, err)
	}
	detail, err := service.JobDetail(context.Background(), "ops-sqlite-export", 10)
	if err != nil {
		t.Fatal(err)
	}
	if detail.View.Status != StatusFailed || detail.View.Runs.LastSuccess != nil {
		t.Fatalf("failed backup = %+v", detail.View)
	}
}

func TestNextDailyAtUsesLocalClockAcrossDST(t *testing.T) {
	location, _ := time.LoadLocation("America/Los_Angeles")
	for _, test := range []struct{ now, want time.Time }{
		// 04:00 PDT: later today.
		{time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC), time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC)},
		// Exactly 05:30 PDT: tomorrow.
		{time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC), time.Date(2026, 10, 30, 12, 30, 0, 0, time.UTC)},
		// Saturday night before the switch to PST: Sunday 05:30 PST.
		{time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 13, 30, 0, 0, time.UTC)},
	} {
		if got := nextDailyAt(test.now, "05:30", location); !got.Equal(test.want) {
			t.Fatalf("nextDailyAt(%v) = %v, want %v", test.now, got, test.want)
		}
	}
}

func TestBackupConfigIsOptionalAndValidated(t *testing.T) {
	disabled, err := backupConfigFile{}.parse()
	if err != nil || disabled.Enabled() {
		t.Fatalf("empty backup section = %+v, %v", disabled, err)
	}
	base := backupConfigFile{Directory: "/tmp/x", At: "05:30", Timezone: "America/Los_Angeles", Keep: 3, JobID: "ops-sqlite-export", HostID: "den-services"}
	for _, broken := range []func(*backupConfigFile){
		func(f *backupConfigFile) { f.At = "5.30" },
		func(f *backupConfigFile) { f.Timezone = "" },
		func(f *backupConfigFile) { f.Timezone = "Mars/Base" },
		func(f *backupConfigFile) { f.Keep = 0 },
		func(f *backupConfigFile) { f.JobID = "" },
	} {
		file := base
		broken(&file)
		if _, err := file.parse(); err == nil {
			t.Fatalf("broken backup config %+v accepted", file)
		}
	}
}

func TestBackupKeepsVerifiedExportsWhenARestoreTestFails(t *testing.T) {
	clock := &fixedClock{now: time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC)}
	directory := filepath.Join(t.TempDir(), "daily")
	backup, service := newTestBackup(t, directory, clock)
	backup.config.Keep = 1
	if err := backup.Register(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, err := backup.Run(context.Background()); err != nil || result["state"] != "success" {
		t.Fatalf("first export = %v, %v", result, err)
	}
	backup.verify = func(_ context.Context, export string, runID string) map[string]any {
		return map[string]any{"kind": "restore_test", "run_id": runID, "at": clock.now, "archive": filepath.Base(export), "state": "failed", "detail": "simulated unreadable export"}
	}
	clock.now = clock.now.Add(24 * time.Hour)
	result, err := backup.Run(context.Background())
	if err != nil || result["state"] != "failed" || result["pruned"] != nil {
		t.Fatalf("second export = %v, %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "ops-20261029T123000Z.sqlite3")); err != nil {
		t.Fatalf("the verified export was pruned after a failed restore test: %v", err)
	}
	detail, err := service.JobDetail(context.Background(), "ops-sqlite-export", 10)
	if err != nil {
		t.Fatal(err)
	}
	if detail.View.Status != StatusFailed || detail.View.Runs.LastSuccess == nil || detail.View.Runs.LastSuccess.ArchiveName != "ops-20261029T123000Z.sqlite3" {
		t.Fatalf("job = %+v", detail.View)
	}
}
