package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Ops protects its own ledger: once a day it writes a consistent SQLite
// snapshot (VACUUM INTO), proves the snapshot is usable by opening it as a
// scratch ledger and computing a summary, keeps the newest few, and records
// the result as a run of its own job. A missed day is not caught up; the job
// then shows as overdue like any other.

const (
	backupFilePrefix = "ops-"
	backupFileSuffix = ".sqlite3"
	backupGrace      = "1h"
)

// BackupConfig is the optional backup section of the service config. An
// empty Directory disables backups.
type BackupConfig struct {
	Directory string
	At        string
	Location  *time.Location
	Keep      int
	JobID     string
	HostID    string
}

func (c BackupConfig) Enabled() bool { return c.Directory != "" }

type Backup struct {
	store        *Store
	service      *Service
	config       BackupConfig
	databasePath string
	defaults     DefaultsConfig
	now          func() time.Time
	// verify is the restore test; tests replace it to simulate a failure.
	verify func(ctx context.Context, export string, runID string) map[string]any
}

func NewBackup(store *Store, service *Service, config BackupConfig, databasePath string, defaults DefaultsConfig, now func() time.Time) *Backup {
	backup := &Backup{store: store, service: service, config: config, databasePath: databasePath, defaults: defaults, now: now}
	backup.verify = backup.restoreTest
	return backup
}

// Register records the backup job's expectation, so a backup that never runs
// shows as never_reported or overdue.
func (b *Backup) Register(ctx context.Context) error {
	enabled := true
	_, err := b.service.RegisterJob(ctx, b.config.JobID, RegisterJobRequest{
		HostID:          b.config.HostID,
		DisplayName:     "Ops ledger SQLite export",
		Engine:          "sqlite",
		Kind:            "backup",
		Source:          b.config.HostID + ":" + b.databasePath,
		Scope:           "Consistent VACUUM INTO snapshot of the whole Ops ledger, restore-tested as a scratch ledger",
		Target:          b.config.HostID + ":" + b.config.Directory,
		Schedule:        &ScheduleRequest{Timezone: b.config.Location.String(), Times: []string{b.config.At}, Weekdays: []string{}, Grace: backupGrace},
		Retention:       fmt.Sprintf("newest %d exports", b.config.Keep),
		RecoveryPointer: "den-services docs/ops-operations.md (Restore the ledger)",
		Notes:           "Runs inside the Ops service. The export directory is inside CT106, so the daily Proxmox backup of CT106 carries it to den-nas.",
		Enabled:         &enabled,
	})
	if err != nil {
		return fmt.Errorf("registering %s: %w", b.config.JobID, err)
	}
	return nil
}

// Run writes one export, restore-tests it, prunes old exports and reports
// the outcome. It returns the report it recorded.
func (b *Backup) Run(ctx context.Context) (map[string]any, error) {
	started := b.now().UTC()
	stamp := started.Format("20060102T150405Z")
	runID := "export-" + stamp
	name := backupFilePrefix + stamp + backupFileSuffix
	final := filepath.Join(b.config.Directory, name)
	report := map[string]any{
		"job_id": b.config.JobID, "run_id": runID, "host": b.config.HostID, "engine": "sqlite", "runner": "ops",
		"kind": "backup", "started_at": started, "target": final, "warnings": []string{},
	}
	runErr := b.export(ctx, final)
	if runErr == nil {
		info, err := os.Stat(final)
		if err != nil {
			runErr = err
		} else {
			size := info.Size()
			report["archive"] = map[string]any{"id": name, "name": name}
			report["stats"] = map[string]any{"original_size": size}
			restore := b.verify(ctx, final, runID)
			report["restore_test"] = restore
			// Older exports are only retired once the new one is proven
			// restorable; otherwise they stay as the recovery copies.
			if restore["state"] == "success" {
				if pruned, err := b.prune(); err != nil {
					report["warnings"] = []string{"Pruning old exports failed: " + err.Error()}
				} else if len(pruned) > 0 {
					report["pruned"] = pruned
				}
			}
		}
	}
	finished := b.now().UTC()
	report["finished_at"] = finished
	report["updated_at"] = finished
	switch {
	case runErr != nil:
		report["state"] = "failed"
		report["error"] = runErr.Error()
	case report["restore_test"].(map[string]any)["state"] != "success":
		report["state"] = "failed"
		report["error"] = "the export was written but its restore test failed"
	case len(report["warnings"].([]string)) > 0:
		report["state"] = "warning"
		report["backup_complete"] = true
	default:
		report["state"] = "success"
		report["backup_complete"] = true
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return report, fmt.Errorf("encoding backup report: %w", err)
	}
	var request RunReportRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return report, fmt.Errorf("decoding backup report: %w", err)
	}
	if _, _, _, err := b.service.ReportRun(ctx, b.config.JobID, runID, request, raw); err != nil {
		return report, fmt.Errorf("recording backup run: %w", err)
	}
	if runErr != nil {
		return report, runErr
	}
	return report, nil
}

func (b *Backup) export(ctx context.Context, final string) error {
	if err := os.MkdirAll(b.config.Directory, 0o750); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}
	temporary := filepath.Join(b.config.Directory, "."+filepath.Base(final)+".tmp")
	_ = os.Remove(temporary)
	if err := b.store.ExportTo(ctx, temporary); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := syncFile(temporary); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, final); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("committing export: %w", err)
	}
	return nil
}

// restoreTest copies the export to a scratch file and opens it the way the
// service opens its ledger. Passing means Ops could run from this file.
func (b *Backup) restoreTest(ctx context.Context, export string, runID string) map[string]any {
	at := b.now().UTC()
	result := map[string]any{"kind": "restore_test", "run_id": runID, "at": at, "archive": filepath.Base(export), "full_data_read": true}
	scratchDir, err := os.MkdirTemp(b.config.Directory, ".restore-test-")
	if err != nil {
		return failedCheck(result, err)
	}
	defer os.RemoveAll(scratchDir)
	scratch := filepath.Join(scratchDir, "ops.sqlite3")
	if err := copyFile(export, scratch); err != nil {
		return failedCheck(result, err)
	}
	restored, err := OpenStore(ctx, scratch)
	if err != nil {
		return failedCheck(result, err)
	}
	defer restored.Close()
	check, err := restored.CheckLedger(ctx)
	if err != nil {
		return failedCheck(result, err)
	}
	if check.Integrity != "ok" {
		return failedCheck(result, fmt.Errorf("integrity_check: %s", check.Integrity))
	}
	summary, err := NewService(restored, b.now, b.defaults).Summary(ctx)
	if err != nil {
		return failedCheck(result, err)
	}
	result["state"] = "success"
	result["checked"] = []string{
		"integrity_check ok",
		fmt.Sprintf("%d hosts, %d jobs, %d runs, %d verifications", check.Hosts, check.Jobs, check.Runs, check.Verifications),
		fmt.Sprintf("summary computed from the restored copy: %d jobs, overall %s", len(summary.Jobs), summary.Status),
	}
	result["scope"] = "Copied the export to a scratch file, opened it as an Ops ledger, ran integrity_check and computed a status summary"
	return result
}

// prune keeps the newest Keep exports and removes the rest.
func (b *Backup) prune() ([]string, error) {
	entries, err := os.ReadDir(b.config.Directory)
	if err != nil {
		return nil, fmt.Errorf("listing exports: %w", err)
	}
	var exports []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), backupFilePrefix) && strings.HasSuffix(entry.Name(), backupFileSuffix) {
			exports = append(exports, entry.Name())
		}
	}
	slices.Sort(exports)
	if len(exports) <= b.config.Keep {
		return nil, nil
	}
	removed := exports[:len(exports)-b.config.Keep]
	var errs []error
	for _, name := range removed {
		if err := os.Remove(filepath.Join(b.config.Directory, name)); err != nil {
			errs = append(errs, err)
		}
	}
	return removed, errors.Join(errs...)
}

// Schedule runs the backup at the configured local time every day until ctx
// ends. Failures are recorded as runs and logged; they never stop the service.
func (b *Backup) Schedule(ctx context.Context) {
	for {
		next := nextDailyAt(b.now(), b.config.At, b.config.Location)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if report, err := b.Run(ctx); err != nil {
			slog.Error("ops ledger backup failed", "error", err, "run_id", report["run_id"])
		} else {
			slog.Info("ops ledger backup", "run_id", report["run_id"], "state", report["state"], "target", report["target"])
		}
	}
}

// nextDailyAt returns the next time after now that the clock reads at
// (HH:MM) in location.
func nextDailyAt(now time.Time, at string, location *time.Location) time.Time {
	clock, _ := time.Parse("15:04", at)
	local := now.In(location)
	next := time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
	if !next.After(now) {
		next = time.Date(local.Year(), local.Month(), local.Day()+1, clock.Hour(), clock.Minute(), 0, 0, location)
	}
	return next
}

func failedCheck(result map[string]any, err error) map[string]any {
	result["state"] = "failed"
	result["detail"] = err.Error()
	return result
}

func copyFile(from string, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("opening export: %w", err)
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("creating scratch copy: %w", err)
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return fmt.Errorf("copying export: %w", err)
	}
	return target.Close()
}

func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening export: %w", err)
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing export: %w", err)
	}
	return nil
}
