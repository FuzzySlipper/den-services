package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func resticTestSource() opsCollectSource {
	return opsCollectSource{
		Type: "restic-offsite", StateDir: "/var/lib/den-offsite-backup", JobID: "den-nas-offsite",
		MaintenanceJob: "den-nas-offsite-maintenance", HostID: "den-nas", SSH: []string{"ssh", "target"},
	}
}

func resticReport(started time.Time, fields map[string]any) map[string]any {
	report := map[string]any{
		"job_id": "den-nas-offsite", "kind": "run", "state": "success", "phase": "retention", "backup_complete": true,
		"warnings": []any{}, "started_at": ts(started), "updated_at": ts(started.Add(time.Minute)), "finished_at": ts(started.Add(time.Minute)),
		"snapshot_id": "a9cc2a0673cc7c802ac6bdcd66914ddbbe3594231761314935b6c0bb03809be3",
		"backup": map[string]any{"exit_code": 0, "summary": map[string]any{
			"total_bytes_processed": 29510421643.0, "data_added_packed": 1830183.0, "total_files_processed": 7183.0,
		}},
		"retention":        map[string]any{"exit_code": 0},
		"git_repositories": []any{map[string]any{"repository": "den-wiki-content.git", "refs": []any{"a refs/heads/main", "b HEAD"}}},
	}
	for key, value := range fields {
		if value == nil {
			delete(report, key)
		} else {
			report[key] = value
		}
	}
	return report
}

func resticBundleOf(t *testing.T, files map[string]any) resticBundle {
	t.Helper()
	bundle := resticBundle{Files: map[string]json.RawMessage{}, Errors: map[string]string{}}
	for name, value := range files {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		bundle.Files[name] = data
	}
	return bundle
}

func TestResticRunsKeepOneIdentityAndMaintenanceNeverFailsACapture(t *testing.T) {
	// Saturday 2026-11-07 is in the first week: a report without read_data implies --read-data.
	saturday := time.Date(2026, 11, 7, 15, 0, 4, 0, time.UTC)
	friday := saturday.Add(-24 * time.Hour)
	failed := saturday.Add(-48 * time.Hour)
	saturdayReport := resticReport(saturday, map[string]any{
		"phase":    "check",
		"warnings": []any{"Repository check failed; inspect the check log."},
		"state":    "warning",
		"prune":    map[string]any{"exit_code": 0},
		"check":    map[string]any{"exit_code": 1},
	})
	bundle := resticBundleOf(t, map[string]any{
		"runs/20261107T150004Z/report.json": saturdayReport,
		"latest.json":                       saturdayReport, // a copy without run_id: matched by started_at
		"runs/20261106T150004Z/report.json": resticReport(friday, nil),
		"runs/20261105T150004Z/report.json": resticReport(failed, map[string]any{
			"state": "error", "backup_complete": false, "snapshot_id": nil, "retention": nil, "backup": nil, "git_repositories": nil,
			"warnings": []any{"Source mount or required directories unavailable: /mnt/storage/pictures"},
		}),
		// last-success from before the lookback: no run directory, so the
		// run_id it carries keeps its identity.
		"last-success.json": resticReport(friday.Add(-20*24*time.Hour), map[string]any{"run_id": "20261017T150004Z"}),
		"bucket-status.json": map[string]any{
			"checked_at": "2026-10-30T10:40:57Z", "bucket": "den-nas-emergency-20261030",
			"all_uploaded_version_bytes": 27593636449.0, "visible_current_files": 1570.0,
		},
	})
	registration := json.RawMessage(`{"host_id":"den-nas","engine":"restic","kind":"backup","notes":"Runner: den-offsite-backup.timer."}`)
	writes, problems := resticWrites(resticTestSource(), bundle, registration)
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	bodies := writesByPath(t, writes)

	backup := bodies["/v1/ops/jobs/den-nas-offsite/runs/20261107T150004Z"]
	if backup["state"] != "success" || backup["backup_complete"] != true || backup["run_id"] != "20261107T150004Z" || backup["kind"] != "backup" {
		t.Fatalf("Saturday capture = %v", backup)
	}
	if warnings := backup["warnings"].([]any); len(warnings) != 0 {
		t.Fatalf("a check failure leaked into the capture: %v", warnings)
	}
	stats := backup["stats"].(map[string]any)
	if stats["deduplicated_size"] != 1830183.0 || stats["original_size"] != 29510421643.0 || stats["nfiles"] != 7183.0 {
		t.Fatalf("stats = %v", stats)
	}
	if archive := backup["archive"].(map[string]any); archive["name"] != "a9cc2a06" {
		t.Fatalf("archive = %v", archive)
	}

	maintenance := bodies["/v1/ops/jobs/den-nas-offsite-maintenance/runs/20261107T150004Z"]
	if maintenance["state"] != "failed" || maintenance["error"] != "check exited 1" {
		t.Fatalf("maintenance = %v", maintenance)
	}
	for _, job := range []string{"den-nas-offsite", "den-nas-offsite-maintenance"} {
		check := bodies["/v1/ops/jobs/"+job+"/verifications/20261107T150004Z:check"]
		if check["state"] != "failed" || check["full_data_read"] != true || !strings.Contains(check["scope"].(string), "inferred") {
			t.Fatalf("%s check = %v", job, check)
		}
	}
	if _, ok := bodies["/v1/ops/jobs/den-nas-offsite-maintenance/runs/20261106T150004Z"]; ok {
		t.Fatal("a weekday run without prune or check became a maintenance run")
	}

	missingMount := bodies["/v1/ops/jobs/den-nas-offsite/runs/20261105T150004Z"]
	if missingMount["state"] != "failed" || !strings.Contains(missingMount["error"].(string), "Source mount") {
		t.Fatalf("missing mount run = %v", missingMount)
	}
	if _, ok := bodies["/v1/ops/jobs/den-nas-offsite/runs/20261017T150004Z"]; !ok {
		t.Fatalf("last success outside the run window was not kept: %v", sortedKeys(bodies))
	}
	notes := bodies["/v1/ops/jobs/den-nas-offsite"]["notes"].(string)
	if !strings.HasPrefix(notes, "Runner: den-offsite-backup.timer.") || !strings.Contains(notes, "27.59 GB") || !strings.Contains(notes, "2026-10-30T10:40:57Z") {
		t.Fatalf("notes = %q", notes)
	}
	if len(bodies) != 8 {
		t.Fatalf("writes = %v", sortedKeys(bodies))
	}

	// Replaying the same files gives byte-identical writes, so the cache and
	// Ops both see duplicates.
	again, _ := resticWrites(resticTestSource(), bundle, registration)
	for index := range writes {
		if writes[index].Path != again[index].Path || string(writes[index].Body) != string(again[index].Body) {
			t.Fatalf("replay differs at %s", writes[index].Path)
		}
	}
}

func TestResticExplicitCheckScopeHeartbeatsAndRestoreFailures(t *testing.T) {
	// A metadata check recorded by the runner on a first-week day is not
	// reported as a full read.
	started := time.Date(2026, 11, 7, 15, 0, 0, 0, time.UTC)
	running := resticReport(started, map[string]any{"state": "running", "phase": "backup", "finished_at": nil, "backup_complete": false})
	explicit := resticReport(started.Add(-7*24*time.Hour), map[string]any{
		"run_id": "20261031T150000Z", "prune": map[string]any{"exit_code": 0},
		"check": map[string]any{"exit_code": 0, "read_data": false},
	})
	bundle := resticBundleOf(t, map[string]any{
		"runs/20261107T150000Z/report.json": running,
		"runs/20261031T150000Z/report.json": explicit,
		"restore-verification.json": map[string]any{
			"tested_at": "2026-10-30T10:44:30Z", "state": "success", "snapshot_id": "10b6ae8a",
			"files":            []any{map[string]any{"source": "/mnt/storage/pictures/a.jpg", "matches_source": false}},
			"git_repositories": []any{map[string]any{"repository": "den-wiki-content.git", "fsck_exit_code": 0, "metadata_applied": true}},
		},
	})
	bundle.Errors["maintenance-verification.json"] = "PermissionError: denied"
	writes, problems := resticWrites(resticTestSource(), bundle, nil)
	if len(problems) != 1 || !strings.Contains(problems[0], "maintenance-verification.json") {
		t.Fatalf("problems = %v", problems)
	}
	bodies := writesByPath(t, writes)
	heartbeat := bodies["/v1/ops/jobs/den-nas-offsite/runs/20261107T150000Z"]
	if heartbeat["state"] != "running" || heartbeat["finished_at"] != nil {
		t.Fatalf("heartbeat = %v", heartbeat)
	}
	check := bodies["/v1/ops/jobs/den-nas-offsite/verifications/20261031T150000Z:check"]
	if check["full_data_read"] != false || strings.Contains(check["scope"].(string), "inferred") || check["state"] != "success" {
		t.Fatalf("explicit check = %v", check)
	}
	restore := bodies["/v1/ops/jobs/den-nas-offsite/verifications/restore-test-20261030T104430Z"]
	if restore["state"] != "failed" || restore["kind"] != "restore_test" || restore["full_data_read"] != false {
		t.Fatalf("restore test = %v", restore)
	}
	if _, ok := bodies["/v1/ops/jobs/den-nas-offsite"]; ok {
		t.Fatal("registered the job without a bucket observation")
	}
}

func TestResticSourceReadsStatusFilesOnlyAndReplacesTheConfiguredRegistration(t *testing.T) {
	now := time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC)
	env := testCollectEnv(now)
	var command string
	env.remote = func(_ context.Context, ssh []string, remote string) ([]byte, error) {
		command = remote
		bundle := resticBundleOf(t, map[string]any{"bucket-status.json": map[string]any{
			"checked_at": "2026-10-30T10:40:57Z", "bucket": "b", "all_uploaded_version_bytes": 1e9, "visible_current_files": 3.0,
		}})
		return json.Marshal(bundle)
	}
	config := opsCollectConfig{
		Jobs:    map[string]json.RawMessage{"den-nas-offsite": json.RawMessage(`{"host_id":"den-nas","engine":"restic","kind":"backup","notes":"Static."}`)},
		Sources: []opsCollectSource{resticTestSource()},
	}
	if err := config.Sources[0].validate(); err != nil {
		t.Fatal(err)
	}
	writes, failures := collectOpsEvidence(context.Background(), config, env)
	if len(failures) != 0 {
		t.Fatalf("failures = %v", failures)
	}
	if strings.Contains(command, "settings.json") || strings.Contains(command, "credentials") ||
		!strings.Contains(command, "/var/lib/den-offsite-backup") || !strings.Contains(command, compactUTC(now.Add(-env.lookback))) {
		t.Fatalf("remote command = %s", command)
	}
	bodies := writesByPath(t, writes)
	if notes := bodies["/v1/ops/jobs/den-nas-offsite"]["notes"].(string); !strings.HasPrefix(notes, "Static. B2 bucket b held 1.00 GB") {
		t.Fatalf("notes = %q", notes)
	}
}
