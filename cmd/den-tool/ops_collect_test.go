package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testCollectEnv(now time.Time) opsCollectEnv {
	cache := &opsCollectCache{Entries: map[string]opsCollectCacheEntry{}}
	return opsCollectEnv{now: now, lookback: 14 * 24 * time.Hour, cache: cache}
}

func writesByPath(t *testing.T, writes []opsCollectWrite) map[string]map[string]any {
	t.Helper()
	bodies := map[string]map[string]any{}
	for _, write := range writes {
		if _, duplicate := bodies[write.Path]; duplicate {
			t.Fatalf("path %s written twice", write.Path)
		}
		var body map[string]any
		if err := json.Unmarshal(write.Body, &body); err != nil {
			t.Fatalf("%s body: %v", write.Path, err)
		}
		bodies[write.Path] = body
	}
	return bodies
}

func ts(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

// borgRunnerFixture writes a den-agents runner state directory: an older
// warning run, a newer success, a failed maintenance run and a restore test.
func borgRunnerFixture(t *testing.T, dir string, jobID string, now time.Time) {
	t.Helper()
	warnStart := now.Add(-26 * time.Hour)
	writeJSONFile(t, filepath.Join(dir, "runs", "r1", "result.json"), map[string]any{
		"job_id": jobID, "run_id": "r1", "kind": "backup", "engine": "borg", "state": "warning",
		"started_at": ts(warnStart), "updated_at": ts(warnStart.Add(time.Hour)), "finished_at": ts(warnStart.Add(time.Hour)),
		"warnings": []string{"file changed while we backed it up"}, "backup_complete": false,
		"archive": map[string]any{"id": "a1", "name": jobID + "-1"}, "exit_code": 1,
	})
	okStart := now.Add(-2 * time.Hour)
	success := map[string]any{
		"job_id": jobID, "run_id": "r2", "kind": "backup", "engine": "borg", "state": "success",
		"started_at": ts(okStart), "updated_at": ts(okStart.Add(10 * time.Minute)), "finished_at": ts(okStart.Add(10 * time.Minute)),
		"warnings": []string{}, "backup_complete": true, "exit_code": 0,
		"archive": map[string]any{"id": "a2", "name": jobID + "-2", "stats": map[string]any{"deduplicated_size": 1024, "original_size": 4096}},
	}
	writeJSONFile(t, filepath.Join(dir, "runs", "r2", "result.json"), success)
	// latest.json is an older heartbeat copy of r2: the finished copy must win.
	heartbeat := map[string]any{}
	for key, value := range success {
		heartbeat[key] = value
	}
	delete(heartbeat, "finished_at")
	heartbeat["state"], heartbeat["updated_at"] = "running", ts(okStart.Add(time.Minute))
	writeJSONFile(t, filepath.Join(dir, "latest.json"), heartbeat)
	restore := map[string]any{
		"job_id": jobID, "kind": "restore_test", "state": "success", "at": ts(now.Add(-90 * time.Minute)),
		"archive": jobID + "-2", "scope": "isolated fixture", "files_compared": []string{"main/source.txt"},
	}
	writeJSONFile(t, filepath.Join(dir, "restore-test.json"), restore)
	maintenanceAt := now.Add(-time.Hour)
	writeJSONFile(t, filepath.Join(dir, "runs", "m1", "result.json"), map[string]any{
		"job_id": jobID, "run_id": "m1", "kind": "maintenance", "engine": "borg", "state": "failed",
		"started_at": ts(maintenanceAt), "finished_at": ts(maintenanceAt.Add(5 * time.Minute)), "exit_code": 2,
		"error":        "Repository check failed: segment 12 is corrupt",
		"verification": map[string]any{"kind": "repository-and-latest-archive", "at": ts(maintenanceAt.Add(5 * time.Minute)), "state": "failed", "full_data_read": false},
		"restore_test": restore,
	})
}

func TestCollectBorgRunnerRoutesMaintenanceAndKeepsFinishedCopy(t *testing.T) {
	now := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	borgRunnerFixture(t, dir, "den-agents-git", now)
	writes, err := collectBorgRunner(opsCollectSource{StateDir: dir, MaintenanceJob: "den-agents-git-maintenance"}, testCollectEnv(now))
	if err != nil {
		t.Fatal(err)
	}
	bodies := writesByPath(t, writes)
	if got := bodies["/v1/ops/jobs/den-agents-git/runs/r2"]["state"]; got != "success" {
		t.Fatalf("r2 state = %v; the finished result must beat the older latest.json heartbeat", got)
	}
	if got := bodies["/v1/ops/jobs/den-agents-git/runs/r1"]["state"]; got != "warning" {
		t.Fatalf("r1 state = %v", got)
	}
	maintenance := bodies["/v1/ops/jobs/den-agents-git-maintenance/runs/m1"]
	if maintenance == nil || maintenance["job_id"] != "den-agents-git-maintenance" {
		t.Fatalf("maintenance run not routed to its own job: %v", maintenance)
	}
	if _, wrong := bodies["/v1/ops/jobs/den-agents-git/runs/m1"]; wrong {
		t.Fatal("maintenance run was reported as a backup attempt")
	}
	check := bodies["/v1/ops/jobs/den-agents-git/verifications/m1:verification"]
	if check == nil || check["state"] != "failed" || check["run_id"] != "m1" {
		t.Fatalf("repository check on the backup job = %v", check)
	}
	restore := bodies["/v1/ops/jobs/den-agents-git/verifications/restore-test-20261029T143000Z"]
	if restore == nil || restore["files_compared"] == nil {
		t.Fatalf("restore test = %v", restore)
	}
}

// thinConfigFixture writes a thin runner state directory where the newest
// capture failed, the NAS copy was deferred and a NAS restore check passed.
func thinConfigFixture(t *testing.T, dir string, now time.Time, mirror map[string]any) {
	t.Helper()
	good := now.Add(-26 * time.Hour)
	writeJSONFile(t, filepath.Join(dir, "reports", "R1-fx-nas.json"), map[string]any{
		"job_id": "fx-nas-config", "run_id": "R1-fx-nas", "host": "fx-nas", "engine": "borg", "kind": "backup", "state": "success",
		"started_at": ts(good), "finished_at": ts(good.Add(time.Minute)), "warnings": []string{},
		"archive":      map[string]any{"id": "c1", "name": "fx-nas-config-R1", "stats": map[string]any{"deduplicated_size": 10, "original_size": 100}},
		"verification": map[string]any{"at": ts(good.Add(time.Minute)), "state": "success", "scope": "local Borg extract", "checked": []string{"files/etc/fstab"}},
	})
	failed := now.Add(-2 * time.Hour)
	failedReport := map[string]any{
		"job_id": "fx-nas-config", "run_id": "R2-fx-nas", "host": "fx-nas", "engine": "borg", "kind": "backup", "state": "failed",
		"started_at": ts(failed), "finished_at": ts(failed.Add(time.Minute)), "error": "ssh: connect to host fx-nas port 22: No route to host",
	}
	writeJSONFile(t, filepath.Join(dir, "reports", "R2-fx-nas.json"), failedReport)
	writeJSONFile(t, filepath.Join(dir, "reports", "fx-nas-latest.json"), failedReport)
	writeJSONFile(t, filepath.Join(dir, "maintenance.json"), map[string]any{
		"at": ts(now.Add(-3 * time.Hour)), "state": "success", "check_exit": 0, "compact_exit": 0, "detail": "",
	})
	writeJSONFile(t, filepath.Join(dir, "latest.json"), map[string]any{
		"schema_version": 1, "at": ts(failed.Add(2 * time.Minute)), "run_id": "R2", "state": "failed",
		"jobs": []any{failedReport}, "nas_mirror": mirror, "report_publication": map[string]any{"state": "success", "detail": ""},
	})
	writeJSONFile(t, filepath.Join(dir, "mirror-verification.json"), map[string]any{
		"kind": "mirror_verification", "at": ts(good.Add(10 * time.Minute)), "state": "success", "generation": "gen-R1",
		"repository": "ssh://target/mnt/storage/backup/host-configs/current/borg", "scope": "NAS repository check",
		"restores": []any{map[string]any{"host": "fx-nas", "archive": "fx-nas-config-R1", "verification": map[string]any{"state": "success"}}},
	})
}

func TestCollectThinConfigSeparatesCaptureLocalRestoreAndNASCopy(t *testing.T) {
	now := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	thinConfigFixture(t, dir, now, map[string]any{"state": "deferred", "reason": "NAS maintenance lock busy", "detail": ""})
	writes, err := collectThinConfig(opsCollectSource{StateDir: dir, NASCopyJob: "fx-nas-copy"}, testCollectEnv(now))
	if err != nil {
		t.Fatal(err)
	}
	bodies := writesByPath(t, writes)
	good := bodies["/v1/ops/jobs/fx-nas-config/runs/R1-fx-nas"]
	restore, _ := good["restore_test"].(map[string]any)
	if good["verification"] != nil || restore["kind"] != "restore_test" || restore["archive"] != "fx-nas-config-R1" {
		t.Fatalf("local restore check = %v", good)
	}
	if bodies["/v1/ops/jobs/fx-nas-config/runs/R2-fx-nas"]["state"] != "failed" {
		t.Fatal("failed capture not reported as failed")
	}
	if check := bodies["/v1/ops/jobs/fx-nas-config/verifications/local-repository-check-20261029T130000Z"]; check["kind"] != "repository-check" {
		t.Fatalf("local repository check = %v", check)
	}
	copyRun := bodies["/v1/ops/jobs/fx-nas-copy/runs/R2"]
	warnings, _ := copyRun["warnings"].([]any)
	if copyRun["state"] != "warning" || copyRun["backup_complete"] != false || len(warnings) != 1 || !strings.Contains(warnings[0].(string), "deferred") {
		t.Fatalf("deferred NAS copy = %v", copyRun)
	}
	nasRestore := bodies["/v1/ops/jobs/fx-nas-copy/verifications/nas-copy-restore-20261028T141000Z"]
	if nasRestore["kind"] != "restore_test" || !strings.HasPrefix(nasRestore["scope"].(string), "NAS copy gen-R1") {
		t.Fatalf("NAS restore = %v", nasRestore)
	}
	if _, ok := bodies["/v1/ops/jobs/fx-nas-config/verifications/nas-copy-restore-20261028T141000Z"]; ok {
		t.Fatal("NAS copy proof must not be filed as the host job's local restore")
	}

	for _, test := range []struct {
		mirror map[string]any
		state  string
	}{
		{map[string]any{"state": "success", "path": "/mnt/storage/backup/host-configs/gen-R2", "bytes": 5.0}, "success"},
		{map[string]any{"state": "failed", "reason": "NAS recovery copy failed", "detail": "Incomplete recovery transfer"}, "failed"},
	} {
		summary := reportFile{fields: map[string]any{"run_id": "R2", "at": ts(now), "nas_mirror": test.mirror}}
		run, ok := thinNASCopyRun(summary)
		if !ok || run["state"] != test.state {
			t.Fatalf("mirror %v -> %v", test.mirror, run)
		}
	}
}

func journalLine(at time.Time, fields map[string]any) string {
	fields["__REALTIME_TIMESTAMP"] = fmt.Sprint(at.UnixMicro())
	data, _ := json.Marshal(fields)
	return string(data)
}

func TestSnapraidInvocationsBecomeMaintenanceRunsAndScrubChecks(t *testing.T) {
	base := time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)
	start := func(at time.Time, unit, id string) string {
		return journalLine(at, map[string]any{"MESSAGE": "Starting " + unit, "MESSAGE_ID": journalStartMessage, "UNIT": unit, "INVOCATION_ID": id})
	}
	say := func(at time.Time, unit, id, text string) string {
		return journalLine(at, map[string]any{"MESSAGE": text, "_SYSTEMD_UNIT": unit, "_SYSTEMD_INVOCATION_ID": id})
	}
	end := func(at time.Time, unit, id, result string, extra map[string]any) string {
		fields := map[string]any{"MESSAGE": "Finished " + unit, "UNIT": unit, "INVOCATION_ID": id, "JOB_RESULT": result}
		for key, value := range extra {
			fields[key] = value
		}
		return journalLine(at, fields)
	}
	lines := []string{
		// Output from an invocation whose start is before the window is ignored.
		say(base.Add(-time.Hour), "snapraid-daily.service", "old", "Sync completed successfully."),
		end(base.Add(-time.Hour), "snapraid-daily.service", "old", "done", nil),
		start(base, "snapraid-weekly.service", "weekly1"),
		say(base, "snapraid-weekly.service", "weekly1", "[2026-10-25T08:00:01-07:00] Starting weekly sync."),
		say(base.Add(time.Hour), "snapraid-weekly.service", "weekly1", "Starting scrub: 25% of the array"),
		say(base.Add(5*time.Hour), "snapraid-weekly.service", "weekly1", "Scrub completed successfully."),
		end(base.Add(5*time.Hour), "snapraid-weekly.service", "weekly1", "done", nil),
		start(base.Add(24*time.Hour), "snapraid-daily.service", "daily1"),
		say(base.Add(24*time.Hour), "snapraid-daily.service", "daily1", "[2026-10-26T08:00:01-07:00] Starting daily sync."),
		say(base.Add(25*time.Hour), "snapraid-daily.service", "daily1", "     592 file errors"),
		say(base.Add(25*time.Hour), "snapraid-daily.service", "daily1", "       0 io errors"),
		say(base.Add(25*time.Hour), "snapraid-daily.service", "daily1", "  45%, 1200 MB"),
		say(base.Add(25*time.Hour), "snapraid-daily.service", "daily1", "[2026-10-26T09:00:00-07:00] FAILED: daily maintenance, exit 1"),
		end(base.Add(25*time.Hour), "snapraid-daily.service", "daily1", "failed", map[string]any{"EXIT_STATUS": "1"}),
		start(base.Add(48*time.Hour), "snapraid-daily.service", "daily2"),
		say(base.Add(48*time.Hour), "snapraid-daily.service", "daily2", "[2026-10-27T08:00:01-07:00] Waiting for the maintenance lock (daily)."),
		// Not active any more and never finished (journal cut): not reported.
		start(base.Add(70*time.Hour), "snapraid-weekly.service", "lost"),
	}
	output := "Id=snapraid-daily.service\nActiveState=active\nInvocationID=daily2\n\nId=snapraid-weekly.service\nActiveState=inactive\nInvocationID=\n---journal---\n" + strings.Join(lines, "\n") + "\n"
	states, entries, err := parseSnapraidOutput([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(49 * time.Hour)
	source := opsCollectSource{JobID: "den-nas-snapraid", HostID: "den-nas"}
	bodies := writesByPath(t, snapraidWrites(source, states, entries, now))
	if len(bodies) != 4 {
		t.Fatalf("writes = %v", sortedKeys(bodies))
	}
	weekly := bodies["/v1/ops/jobs/den-nas-snapraid/runs/snapraid-weekly-weekly1"]
	if weekly["state"] != "success" || weekly["exit_code"] != 0.0 || weekly["kind"] != "maintenance" {
		t.Fatalf("weekly = %v", weekly)
	}
	scrub := bodies["/v1/ops/jobs/den-nas-snapraid/verifications/snapraid-weekly-weekly1:scrub"]
	if scrub["state"] != "success" || scrub["full_data_read"] != false {
		t.Fatalf("scrub = %v", scrub)
	}
	failed := bodies["/v1/ops/jobs/den-nas-snapraid/runs/snapraid-daily-daily1"]
	if failed["state"] != "failed" || failed["exit_code"] != 1.0 || !reflect.DeepEqual(failed["source_notices"], []any{"592 file errors"}) {
		t.Fatalf("failed = %v", failed)
	}
	if text := failed["error"].(string); !strings.Contains(text, "FAILED: daily maintenance") || strings.Contains(text, "45%") {
		t.Fatalf("error tail = %q", text)
	}
	running := bodies["/v1/ops/jobs/den-nas-snapraid/runs/snapraid-daily-daily2"]
	if running["state"] != "running" || running["finished_at"] != nil || running["phase"] != "waiting for maintenance lock" || running["updated_at"] != ts(now) {
		t.Fatalf("running = %v", running)
	}
}

func vzdumpLog(lines ...string) []proxmoxLogLine {
	log := make([]proxmoxLogLine, 0, len(lines))
	for _, line := range lines {
		log = append(log, proxmoxLogLine{Text: line})
	}
	return log
}

func TestParseVzdumpTaskSplitsGuestsWithLocalTimes(t *testing.T) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	log := vzdumpLog(
		"INFO: starting new backup job: vzdump 100 104 105 --mode snapshot --storage backup-server",
		"INFO: Starting Backup of VM 100 (qemu)",
		"INFO: Backup started at 2026-10-28 06:00:02",
		"INFO: creating vzdump archive '/mnt/pve/backup-server/dump/vzdump-qemu-100-2026_10_28-06_00_02.vma.zst'",
		"INFO:  50% (32.0 GiB of 64.0 GiB) in 36s, read: 598.8 MiB/s, write: 259.8 MiB/s",
		"INFO: transferred 64.00 GiB in 83 seconds (789.6 MiB/s)",
		"INFO: Finished Backup of VM 100 (00:01:26)",
		"INFO: Backup finished at 2026-10-28 06:01:28",
		"INFO: Starting Backup of VM 104 (lxc)",
		"INFO: Backup started at 2026-10-28 06:01:28",
		"INFO: creating vzdump archive '/mnt/pve/backup-server/dump/vzdump-lxc-104-2026_10_28-06_01_28.tar.zst'",
		"WARN: unable to read /var/lib/thing: Permission denied",
		"INFO: Total bytes written: 1075660800 (1.1GiB, 92MiB/s)",
		"INFO: Finished Backup of VM 104 (00:00:15)",
		"INFO: Backup finished at 2026-10-28 06:01:43",
		"INFO: Starting Backup of VM 105 (lxc)",
		"INFO: Backup started at 2026-10-28 06:01:43",
		"ERROR: Backup of VM 105 failed - command 'tar' failed: exit code 2",
		"INFO: Failed at 2026-10-28 06:02:00",
		"INFO: Backup job finished with errors",
		"TASK ERROR: job errors",
	)
	task := proxmoxTask{UPID: "UPID:prox:1:2:3:vzdump::root@pam:", Status: "job errors", StartTime: 1793192402, EndTime: 1793192520}
	guests := map[int]proxmoxGuest{100: {Name: "opnsense"}, 104: {Name: "caddy"}, 105: {Name: "SillyTavern"}}
	archives := map[string]proxmoxContent{"vzdump-qemu-100-2026_10_28-06_00_02.vma.zst": {VolID: "backup-server:backup/vzdump-qemu-100-2026_10_28-06_00_02.vma.zst", Size: 2396731178}}
	runs := parseVzdumpTask(opsCollectSource{HostID: "den-prox-mini"}, task, log, location, guests, archives, time.Now())
	if len(runs) != 3 {
		t.Fatalf("runs = %v", runs)
	}
	first := runs[0]
	if first["state"] != "success" || first["started_at"] != time.Date(2026, 10, 28, 13, 0, 2, 0, time.UTC) || first["finished_at"] != time.Date(2026, 10, 28, 13, 1, 28, 0, time.UTC) {
		t.Fatalf("first = %v", first)
	}
	if first["run_id"] != "UPID:prox:1:2:3:vzdump::root@pam:100" || first["host"] != "opnsense" {
		t.Fatalf("first identity = %v", first)
	}
	stats := first["stats"].(map[string]any)
	if stats["compressed_size"] != int64(2396731178) || stats["original_size"] != int64(64<<30) {
		t.Fatalf("stats = %v", stats)
	}
	if archive := first["archive"].(map[string]any); archive["id"] != "backup-server:backup/vzdump-qemu-100-2026_10_28-06_00_02.vma.zst" {
		t.Fatalf("archive = %v", archive)
	}
	if runs[1]["state"] != "warning" || len(runs[1]["warnings"].([]string)) != 1 || runs[1]["stats"].(map[string]any)["original_size"] != int64(1075660800) {
		t.Fatalf("warning guest = %v", runs[1])
	}
	if runs[2]["state"] != "failed" || runs[2]["error"] != "command 'tar' failed: exit code 2" || runs[2]["host"] != "sillytavern" {
		t.Fatalf("failed guest = %v", runs[2])
	}

	running := parseVzdumpTask(opsCollectSource{HostID: "den-prox-mini"}, proxmoxTask{UPID: "UPID:x:", StartTime: 1793192402}, vzdumpLog(
		"INFO: Starting Backup of VM 100 (qemu)",
		"INFO: Backup started at 2026-10-28 06:00:02",
	), location, guests, nil, time.Date(2026, 10, 28, 13, 5, 0, 0, time.UTC))
	if len(running) != 1 || running[0]["state"] != "running" || running[0]["finished_at"] != nil {
		t.Fatalf("running task = %v", running)
	}
	cut := parseVzdumpTask(opsCollectSource{HostID: "den-prox-mini"}, proxmoxTask{UPID: "UPID:y:", Status: "unexpected status", StartTime: 1793192402, EndTime: 1793192500}, vzdumpLog(
		"INFO: Starting Backup of VM 100 (qemu)",
	), location, guests, nil, time.Now())
	if len(cut) != 1 || cut[0]["state"] != "failed" || !strings.Contains(cut[0]["error"].(string), "unexpected status") {
		t.Fatalf("interrupted task = %v", cut)
	}
}

func TestProxmoxScheduleParsesSimpleEventsOnly(t *testing.T) {
	for _, test := range []struct {
		event    string
		times    []string
		weekdays []string
	}{
		{"6:00", []string{"06:00"}, []string{}},
		{"daily", []string{"00:00"}, []string{}},
		{"sat 02:30", []string{"02:30"}, []string{"sat"}},
		{"mon..wed,fri 21:00", []string{"21:00"}, []string{"mon", "tue", "wed", "fri"}},
		{"2,14:30", []string{"02:30", "14:30"}, []string{}},
	} {
		schedule, ok := proxmoxSchedule(test.event)
		if !ok || !reflect.DeepEqual(schedule["times"], test.times) || !reflect.DeepEqual(schedule["weekdays"], test.weekdays) {
			t.Fatalf("%q -> %v, %v", test.event, schedule, ok)
		}
	}
	for _, event := range []string{"*/2:00", "mon 25:00", "weekly", "sun..mon 1:00", ""} {
		if schedule, ok := proxmoxSchedule(event); ok {
			t.Fatalf("%q parsed as %v", event, schedule)
		}
	}
}

func TestCollectProxmoxRegistersCoverageAndSkipsReportedTasks(t *testing.T) {
	now := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	inventory := `{"jobs":[{"id":"backup-1","schedule":"6:00","enabled":1,"vmid":"100,106","storage":"backup-server","mode":"snapshot","repeat-missed":0},
	                        {"id":"off","schedule":"7:00","enabled":0,"vmid":"101","storage":"backup-server"}],
	  "guests":[{"vmid":100,"name":"opnsense","type":"qemu","node":"prox","status":"running"},
	            {"vmid":101,"name":"unifi","type":"lxc","node":"prox","status":"stopped"},
	            {"vmid":106,"name":"den-services","type":"lxc","node":"prox","status":"running"},
	            {"vmid":900,"name":"template","type":"qemu","node":"prox","template":1},
	            {"vmid":200,"name":"elsewhere","type":"qemu","node":"other"}],
	  "tasks":[{"upid":"UPID:prox:A:","status":"OK","starttime":1793192402,"endtime":1793192960},
	           {"upid":"UPID:prox:B:","status":"OK","starttime":1793106005,"endtime":1793106581}],
	  "storages":[{"storage":"backup-server","type":"nfs","server":"192.168.1.9","export":"/mnt/storage/backup/prox","prune-backups":"keep-all=1"}],
	  "timezone":"America/Los_Angeles"}`
	var commands []string
	remote := func(_ context.Context, ssh []string, command string) ([]byte, error) {
		commands = append(commands, command)
		if strings.Contains(command, "/cluster/backup") {
			return []byte(inventory), nil
		}
		if strings.Contains(command, "UPID:prox:B:") {
			t.Fatal("a task already fully reported was fetched again")
		}
		return []byte(`{"config-100":{"scsi0":"local-zfs:vm-100-disk-1,size=64G","scsi1":"local-zfs:vm-100-disk-3,backup=0,size=1T"},
		  "config-101":{"rootfs":"local-zfs:subvol-101-disk-0,size=8G"},
		  "config-106":{"rootfs":"local-zfs:subvol-106-disk-0,size=32G","mp0":"/tank/data,mp=/data"},
		  "content-backup-server":[{"volid":"backup-server:backup/vzdump-lxc-106-x.tar.zst","size":42,"vmid":106}],
		  "log-0":[{"t":"INFO: Starting Backup of VM 106 (lxc)"},{"t":"INFO: Backup started at 2026-10-28 06:06:24"},
		           {"t":"INFO: creating vzdump archive '/mnt/pve/backup-server/dump/vzdump-lxc-106-x.tar.zst'"},
		           {"t":"INFO: Finished Backup of VM 106 (00:02:42)"},{"t":"INFO: Backup finished at 2026-10-28 06:09:06"}]}`), nil
	}
	env := testCollectEnv(now)
	env.remote = remote
	env.cache.markDone(proxmoxDoneMarker+"UPID:prox:B:", now)
	source := opsCollectSource{Type: "proxmox-vzdump", HostID: "den-prox-mini", Node: "prox", SSH: []string{"ssh", "target"}}
	writes, err := collectProxmox(context.Background(), source, env, map[string]json.RawMessage{"opnsense": json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	bodies := writesByPath(t, writes)
	if _, ok := bodies["/v1/ops/hosts/opnsense"]; ok {
		t.Fatal("a statically registered host was overwritten")
	}
	if notes := bodies["/v1/ops/hosts/unifi"]["notes"].(string); !strings.Contains(notes, "Not in any enabled Proxmox backup job") {
		t.Fatalf("uncovered guest notes = %q", notes)
	}
	if _, ok := bodies["/v1/ops/jobs/den-prox-mini-vzdump-101"]; ok {
		t.Fatal("a disabled job was registered")
	}
	if _, ok := bodies["/v1/ops/hosts/template"]; ok {
		t.Fatal("a template was registered as a host")
	}
	ct := bodies["/v1/ops/jobs/den-prox-mini-vzdump-106"]
	if ct["host_id"] != "den-services" || !strings.Contains(ct["scope"].(string), "mp0 /data") {
		t.Fatalf("CT job = %v", ct)
	}
	if schedule := ct["schedule"].(map[string]any); schedule["timezone"] != "America/Los_Angeles" || !reflect.DeepEqual(schedule["times"], []any{"06:00"}) {
		t.Fatalf("schedule = %v", schedule)
	}
	if scope := bodies["/v1/ops/jobs/den-prox-mini-vzdump-100"]["scope"].(string); !strings.Contains(scope, "scsi1 (backup=0)") || strings.Contains(scope, "scsi0") {
		t.Fatalf("VM scope = %q", scope)
	}
	run := bodies["/v1/ops/jobs/den-prox-mini-vzdump-106/runs/UPID:prox:A:106"]
	if run["state"] != "success" || run["finished_at"] != "2026-10-28T13:09:06Z" || run["stats"].(map[string]any)["compressed_size"] != 42.0 {
		t.Fatalf("run = %v", run)
	}
	var marked bool
	for _, write := range writes {
		marked = marked || write.Done == proxmoxDoneMarker+"UPID:prox:A:"
	}
	if !marked || len(commands) != 2 {
		t.Fatalf("done marker %v, %d remote calls", marked, len(commands))
	}
}

// TestOpsCollectReportsThroughOutageIntoRealOps runs the collector against the
// real Ops service: first while Ops is unreachable (everything spools), then
// for real, and checks what Ops concludes about each job.
func TestOpsCollectReportsThroughOutageIntoRealOps(t *testing.T) {
	opsURL := startOpsService(t)
	now := time.Now().UTC()
	dir := t.TempDir()
	borgDir, thinDir := filepath.Join(dir, "borg"), filepath.Join(dir, "thin")
	borgRunnerFixture(t, borgDir, "fx-git", now)
	thinConfigFixture(t, thinDir, now, map[string]any{"state": "deferred", "reason": "NAS maintenance lock busy", "detail": ""})
	daily := func(at time.Time, grace string) map[string]any {
		return map[string]any{"timezone": "UTC", "times": []string{at.Format("15:04")}, "weekdays": []string{}, "grace": grace}
	}
	job := func(host string, schedule map[string]any) map[string]any {
		body := map[string]any{"host_id": host, "engine": "borg", "kind": "backup", "enabled": true}
		if schedule != nil {
			body["schedule"] = schedule
		}
		return body
	}
	overdueStart := now.Add(-30 * time.Hour)
	writeJSONFile(t, filepath.Join(borgDir, "..", "overdue", "runs", "o1", "result.json"), map[string]any{
		"job_id": "fx-overdue", "run_id": "o1", "kind": "backup", "state": "success", "backup_complete": true,
		"started_at": ts(overdueStart), "finished_at": ts(overdueStart.Add(time.Minute)),
	})
	config := map[string]any{
		"cache_file": filepath.Join(dir, "cache.json"),
		"hosts": map[string]any{
			"fx-host":      map[string]any{"display_name": "fx-host"},
			"fx-uncovered": map[string]any{"display_name": "fx-uncovered", "notes": "No backup job"},
		},
		"jobs": map[string]any{
			"fx-git":             job("fx-host", nil),
			"fx-git-maintenance": job("fx-host", nil),
			"fx-nas-config":      job("fx-host", nil),
			"fx-nas-copy":        job("fx-host", nil),
			"fx-missing":         job("fx-host", daily(now.Add(-3*time.Hour), "1h")),
			"fx-overdue":         job("fx-host", daily(now.Add(-3*time.Hour), "1h")),
		},
		"sources": []any{
			map[string]any{"type": "borg-runner", "state_dir": borgDir, "maintenance_job": "fx-git-maintenance"},
			map[string]any{"type": "borg-runner", "state_dir": filepath.Join(dir, "overdue")},
			map[string]any{"type": "thin-config", "state_dir": thinDir, "nas_copy_job": "fx-nas-copy"},
		},
	}
	configPath := filepath.Join(dir, "config.json")
	writeJSONFile(t, configPath, config)
	t.Setenv("DEN_OPS_SPOOL", filepath.Join(dir, "spool"))

	t.Setenv("DEN_OPS_URL", "http://"+freeAddress(t))
	offline := cli(t, 0, "ops", "collect", "--config", configPath, "--json")
	var summary opsCollectSummary
	if err := json.Unmarshal([]byte(offline), &summary); err != nil || summary.Spooled != summary.Writes || summary.Delivered != 0 {
		t.Fatalf("offline collection = %s (%v)", offline, err)
	}

	t.Setenv("DEN_OPS_URL", opsURL)
	online := cli(t, 0, "ops", "collect", "--config", configPath, "--json")
	// The 8 registrations are always sent; everything else was spooled
	// while offline and must be replayed, not sent again.
	if err := json.Unmarshal([]byte(online), &summary); err != nil || summary.Delivered != 8 || summary.Unchanged != summary.Writes-8 {
		t.Fatalf("online collection = %s (%v); spooled writes should replay, not resend", online, err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "spool")); len(entries) != 0 {
		t.Fatalf("spool not drained: %d entries", len(entries))
	}

	statuses := map[string]string{}
	jobs := getJSON(t, opsURL+"/v1/ops/jobs")["jobs"].([]any)
	for _, item := range jobs {
		job := item.(map[string]any)
		statuses[job["job_id"].(string)] = job["status"].(string)
	}
	want := map[string]string{
		"fx-git": "ok", "fx-git-maintenance": "failed", "fx-nas-config": "failed",
		"fx-nas-copy": "warning", "fx-missing": "never_reported", "fx-overdue": "overdue",
	}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("statuses = %v, want %v", statuses, want)
	}
	nas := getJSON(t, opsURL+"/v1/ops/jobs/fx-nas-config")["job"].(map[string]any)
	if success, _ := nas["last_success"].(map[string]any); success == nil || success["run_id"] != "R1-fx-nas" {
		t.Fatalf("the failed capture hid the last good restore point: %v", nas["last_success"])
	}
	git := getJSON(t, opsURL+"/v1/ops/jobs/fx-git")["job"].(map[string]any)
	if check, _ := git["last_verification"].(map[string]any); check == nil || check["state"] != "failed" {
		t.Fatalf("failed repository check not on the backup job: %v", git["last_verification"])
	}
	for _, item := range getJSON(t, opsURL+"/v1/ops/hosts")["hosts"].([]any) {
		host := item.(map[string]any)
		if host["host_id"] == "fx-uncovered" && host["coverage"] != "not_configured" {
			t.Fatalf("uncovered host = %v", host)
		}
	}

	// A third collection with nothing new sends only the registrations.
	again := cli(t, 0, "ops", "collect", "--config", configPath, "--json")
	if err := json.Unmarshal([]byte(again), &summary); err != nil || summary.Delivered != 8 || summary.Spooled != 0 {
		t.Fatalf("repeat collection = %s", again)
	}
}

const proxmoxTestInventory = `{"jobs":[{"id":"backup-1","schedule":"6:00","enabled":%s,"vmid":"%s","storage":"backup-server","mode":"snapshot"}],
  "guests":[{"vmid":100,"name":"opnsense","type":"qemu","node":"prox","status":"running"},
            {"vmid":106,"name":"den-services","type":"lxc","node":"prox","status":"running"}],
  "tasks":[],"storages":[{"storage":"backup-server","type":"nfs","server":"192.168.1.9","export":"/mnt/storage/backup/prox"}],
  "timezone":"America/Los_Angeles"}`

// proxmoxTestEnv fakes the two SSH calls of a proxmox-vzdump collection.
func proxmoxTestEnv(now time.Time, inventory string, config106 string) opsCollectEnv {
	env := testCollectEnv(now)
	env.remote = func(_ context.Context, _ []string, command string) ([]byte, error) {
		if strings.Contains(command, "/cluster/backup") {
			return []byte(inventory), nil
		}
		return []byte(`{"config-100":{"scsi0":"local-zfs:vm-100-disk-1,size=64G"},"config-106":` + config106 + `,"content-backup-server":[]}`), nil
	}
	return env
}

var proxmoxTestSource = opsCollectSource{Type: "proxmox-vzdump", HostID: "den-prox-mini", Node: "prox", SSH: []string{"ssh", "target"}}

func TestProxmoxUnreadableGuestConfigIsUnknownCoverage(t *testing.T) {
	now := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	env := proxmoxTestEnv(now, fmt.Sprintf(proxmoxTestInventory, "1", "100,106"), "null")
	writes, err := collectProxmox(context.Background(), proxmoxTestSource, env, nil)
	if err == nil || !strings.Contains(err.Error(), "config of guest 106 could not be read") {
		t.Fatalf("missing config was not reported: %v", err)
	}
	bodies := writesByPath(t, writes)
	scope := bodies["/v1/ops/jobs/den-prox-mini-vzdump-106"]["scope"].(string)
	if !strings.Contains(scope, "disk exclusions unknown") {
		t.Fatalf("unreadable config published as plain whole-guest coverage: %q", scope)
	}
	if notes := bodies["/v1/ops/hosts/den-services"]["notes"].(string); !strings.Contains(notes, "Disk exclusions unknown") {
		t.Fatalf("host notes = %q", notes)
	}
	if scope := bodies["/v1/ops/jobs/den-prox-mini-vzdump-100"]["scope"].(string); scope != "whole guest, vzdump snapshot mode" {
		t.Fatalf("a readable guest was affected: %q", scope)
	}
}

func TestProxmoxIncompleteInventoryChangesNothing(t *testing.T) {
	now := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	inventory := strings.Replace(fmt.Sprintf(proxmoxTestInventory, "1", "100"), `"jobs":[{"id":"backup-1","schedule":"6:00","enabled":1,"vmid":"100","storage":"backup-server","mode":"snapshot"}]`, `"jobs":null`, 1)
	env := proxmoxTestEnv(now, inventory, `{}`)
	env.registeredJobs = func(context.Context) ([]opsRegisteredJob, error) {
		t.Fatal("an incomplete inventory must not reach coverage reconciliation")
		return nil, nil
	}
	writes, err := collectProxmox(context.Background(), proxmoxTestSource, env, nil)
	if err == nil || len(writes) != 0 {
		t.Fatalf("incomplete inventory = %d writes, %v", len(writes), err)
	}
}

func registeredVzdumpJob(id string, host string) opsRegisteredJob {
	job := opsRegisteredJob{
		JobID: id, HostID: host, DisplayName: host + " Proxmox backup", Engine: "vzdump", Kind: "backup",
		Scope: "whole guest, vzdump snapshot mode", Registered: true, Enabled: true, HeartbeatStaleAfterSeconds: 1800,
	}
	job.Schedule = &struct {
		Timezone     string   `json:"timezone"`
		Times        []string `json:"times"`
		Weekdays     []string `json:"weekdays"`
		GraceSeconds int64    `json:"grace_seconds"`
	}{Timezone: "America/Los_Angeles", Times: []string{"06:00"}, Weekdays: []string{}, GraceSeconds: 7200}
	return job
}

func TestProxmoxRetiresCoverageThatDisappeared(t *testing.T) {
	now := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	previous := []opsRegisteredJob{
		registeredVzdumpJob("den-prox-mini-vzdump-100", "opnsense"),
		registeredVzdumpJob("den-prox-mini-vzdump-106", "den-services"),
		registeredVzdumpJob("den-prox-vzdump-100", "den-agents"), // another source's job
	}
	for _, test := range []struct {
		name, enabled, members string
		retired                []string
	}{
		{"guest removed from the job", "1", "100", []string{"den-prox-mini-vzdump-106"}},
		{"job disabled", "0", "100,106", []string{"den-prox-mini-vzdump-100", "den-prox-mini-vzdump-106"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := proxmoxTestEnv(now, fmt.Sprintf(proxmoxTestInventory, test.enabled, test.members), `{"rootfs":"local-zfs:subvol-106-disk-0"}`)
			env.registeredJobs = func(context.Context) ([]opsRegisteredJob, error) { return previous, nil }
			writes, err := collectProxmox(context.Background(), proxmoxTestSource, env, nil)
			if err != nil {
				t.Fatal(err)
			}
			bodies := writesByPath(t, writes)
			var retired []string
			for path, body := range bodies {
				if body["enabled"] == false {
					retired = append(retired, strings.TrimPrefix(path, "/v1/ops/jobs/"))
				}
			}
			slices.Sort(retired)
			if !slices.Equal(retired, test.retired) {
				t.Fatalf("retired = %v, want %v", retired, test.retired)
			}
			body := bodies["/v1/ops/jobs/den-prox-mini-vzdump-106"]
			schedule := body["schedule"].(map[string]any)
			if body["host_id"] != "den-services" || body["engine"] != "vzdump" || schedule["grace"] != "7200s" || body["heartbeat_stale_after"] != "1800s" {
				t.Fatalf("retired registration lost its fields: %v", body)
			}
		})
	}

	env := proxmoxTestEnv(now, fmt.Sprintf(proxmoxTestInventory, "1", "100"), `{}`)
	env.registeredJobs = func(context.Context) ([]opsRegisteredJob, error) { return nil, fmt.Errorf("ops service unreachable") }
	writes, err := collectProxmox(context.Background(), proxmoxTestSource, env, nil)
	if err == nil || !strings.Contains(err.Error(), "not retired") {
		t.Fatalf("Ops outage during reconciliation = %v", err)
	}
	for _, write := range writes {
		if strings.Contains(string(write.Body), `"enabled":false`) {
			t.Fatalf("coverage retired without knowing what Ops has: %s", write.Path)
		}
	}
}

// TestProxmoxRetirementRoundTripsThroughRealOps registers derived coverage in
// the real Ops service, removes the guest from its job, and checks that the
// job is disabled (history kept) and the guest becomes not configured.
func TestProxmoxRetirementRoundTripsThroughRealOps(t *testing.T) {
	opsURL := startOpsService(t)
	client, err := NewOpsClient(opsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	put := func(writes []opsCollectWrite) {
		for _, write := range writes {
			if _, err := client.Do(ctx, "PUT", write.Path, write.Body); err != nil {
				t.Fatalf("PUT %s: %v", write.Path, err)
			}
		}
	}
	listJobs := func(ctx context.Context) ([]opsRegisteredJob, error) {
		body, err := client.Do(ctx, "GET", "/v1/ops/jobs", nil)
		if err != nil {
			return nil, err
		}
		var response struct {
			Jobs []opsRegisteredJob `json:"jobs"`
		}
		return response.Jobs, json.Unmarshal(body, &response)
	}
	now := time.Now().UTC()
	first := proxmoxTestEnv(now, fmt.Sprintf(proxmoxTestInventory, "1", "100,106"), `{}`)
	first.registeredJobs = listJobs
	writes, err := collectProxmox(ctx, proxmoxTestSource, first, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(writes)

	second := proxmoxTestEnv(now, fmt.Sprintf(proxmoxTestInventory, "1", "100"), `{}`)
	second.registeredJobs = listJobs
	writes, err = collectProxmox(ctx, proxmoxTestSource, second, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(writes)

	job := getJSON(t, opsURL+"/v1/ops/jobs/den-prox-mini-vzdump-106")["job"].(map[string]any)
	if job["enabled"] != false || job["status"] != "disabled" || job["host_id"] != "den-services" {
		t.Fatalf("retired job = %v", job)
	}
	for _, item := range getJSON(t, opsURL+"/v1/ops/hosts")["hosts"].([]any) {
		host := item.(map[string]any)
		switch host["host_id"] {
		case "den-services":
			if host["coverage"] != "not_configured" {
				t.Fatalf("guest removed from its backup job still counts as covered: %v", host)
			}
		case "opnsense":
			if host["coverage"] != "configured" {
				t.Fatalf("still-covered guest = %v", host)
			}
		}
	}
}
