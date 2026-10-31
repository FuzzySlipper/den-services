package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The den-nas offsite backup (den-offsite-backup, Restic to Backblaze B2)
// leaves its status as JSON under /var/lib/den-offsite-backup on the NAS. This
// source reads those files over SSH in one call and reports:
//
//   - each runs/RUN_ID/report.json as a run of the backup job, keyed by the
//     directory name, so latest.json and last-success.json (copies of a run)
//     land on the same run;
//   - the Saturday prune and check inside a run as a run of the maintenance
//     job, and the check as a verification on both jobs. A maintenance problem
//     never turns a complete capture into a failure;
//   - restore-verification.json as a restore test (a sample restore, not a
//     full read), and maintenance-verification.json as a maintenance run;
//   - bucket-status.json as a dated observation in the job's notes. Runs never
//     refresh it, and a run's newly stored bytes are not bucket usage.
//
// It reads status files only: never settings.json or the credentials beside it.

const (
	resticZone            = "America/Los_Angeles"
	resticMaintenanceNote = "Saturday prune and check are reported on %s."
)

// resticReadScript prints {"files": {name: object}, "errors": {name: text}}
// for the status files and the run reports at or after the cutoff run ID.
const resticReadScript = `import json, os, sys
state, cutoff = sys.argv[1], sys.argv[2]
out = {'files': {}, 'errors': {}}
names = ['latest.json', 'last-success.json', 'restore-verification.json', 'maintenance-verification.json', 'bucket-status.json']
try:
    names += ['runs/%s/report.json' % run for run in sorted(os.listdir(os.path.join(state, 'runs'))) if run >= cutoff]
except OSError as error:
    out['errors']['runs'] = str(error)
for name in names:
    try:
        with open(os.path.join(state, name)) as handle:
            out['files'][name] = json.load(handle)
    except FileNotFoundError:
        pass
    except Exception as error:
        out['errors'][name] = '%s: %s' % (type(error).__name__, error)
print(json.dumps(out))
`

type resticBundle struct {
	Files  map[string]json.RawMessage `json:"files"`
	Errors map[string]string          `json:"errors"`
}

func collectResticOffsite(ctx context.Context, source opsCollectSource, env opsCollectEnv, jobs map[string]json.RawMessage) ([]opsCollectWrite, error) {
	cutoff := compactUTC(env.now.Add(-env.lookback))
	command := "python3 -c " + shellQuote(resticReadScript) + " " + shellQuote(source.StateDir) + " " + shellQuote(cutoff)
	output, err := env.remote(ctx, source.SSH, command)
	if err != nil {
		return nil, err
	}
	var bundle resticBundle
	if err := json.Unmarshal(output, &bundle); err != nil {
		return nil, fmt.Errorf("decode offsite status from %s: %w", source.StateDir, err)
	}
	writes, problems := resticWrites(source, bundle, jobs[source.JobID])
	return writes, joinProblems(problems)
}

func resticWrites(source opsCollectSource, bundle resticBundle, registration json.RawMessage) ([]opsCollectWrite, []string) {
	var problems []string
	for _, name := range sortedKeys(bundle.Errors) {
		problems = append(problems, fmt.Sprintf("%s/%s: %s", source.StateDir, name, bundle.Errors[name]))
	}
	reports := map[string]reportFile{}
	for _, name := range sortedKeys(bundle.Files) {
		var fields map[string]any
		if err := json.Unmarshal(bundle.Files[name], &fields); err != nil || fields == nil {
			problems = append(problems, fmt.Sprintf("%s/%s is not a JSON object", source.StateDir, name))
			continue
		}
		reports[name] = reportFile{path: name, raw: bundle.Files[name], fields: fields}
	}

	writes := newWriteSet()
	byStart := map[string]string{} // started_at -> run ID, for copies without run_id
	for _, name := range sortedKeys(reports) {
		runID, ok := strings.CutSuffix(strings.TrimPrefix(name, "runs/"), "/report.json")
		if !ok {
			continue
		}
		byStart[reports[name].text("started_at")] = runID
		addResticRun(source, writes, runID, reports[name])
	}
	for _, name := range []string{"latest.json", "last-success.json"} {
		report, ok := reports[name]
		if !ok {
			continue
		}
		runID := firstNonEmpty(report.text("run_id"), byStart[report.text("started_at")])
		if runID == "" {
			started, ok := report.time("started_at")
			if !ok {
				problems = append(problems, fmt.Sprintf("%s/%s has no started_at", source.StateDir, name))
				continue
			}
			runID = compactUTC(started)
		}
		addResticRun(source, writes, runID, report)
	}
	if report, ok := reports["restore-verification.json"]; ok {
		if check, id, ok := resticRestoreTest(source, report); ok {
			writes.add(opsCollectWrite{Path: opsPath("jobs", source.JobID, "verifications", id), Body: mustJSON(check)}, report.text("tested_at"))
		}
	}
	if report, ok := reports["maintenance-verification.json"]; ok {
		if run, id, ok := resticInitialPrune(source, report); ok {
			writes.add(opsCollectWrite{Path: opsPath("jobs", source.MaintenanceJob, "runs", id), Body: mustJSON(run)}, report.text("tested_at"))
		}
	}

	var registrations []opsCollectWrite
	if bucket, ok := reports["bucket-status.json"]; ok && len(registration) > 0 {
		if body, err := resticRegistrationWithBucket(registration, bucket); err == nil {
			registrations = append(registrations, opsCollectWrite{Path: opsPath("jobs", source.JobID), Body: body})
		} else {
			problems = append(problems, err.Error())
		}
	}
	return append(registrations, writes.list()...), problems
}

// addResticRun adds a report's backup run (for a "run") and its maintenance
// run and check (when the report has prune or check phases).
func addResticRun(source opsCollectSource, writes *writeSet, runID string, report reportFile) {
	order := report.order()
	if report.text("kind") != "maintenance" {
		writes.add(opsCollectWrite{Path: opsPath("jobs", source.JobID, "runs", runID), Body: mustJSON(resticBackupRun(source, runID, report))}, order)
	}
	run, ok := resticMaintenanceRun(source, runID, report)
	if !ok {
		return
	}
	writes.add(opsCollectWrite{Path: opsPath("jobs", source.MaintenanceJob, "runs", runID), Body: mustJSON(run)}, order)
	if check, ok := resticCheck(source, runID, report); ok {
		for _, jobID := range []string{source.JobID, source.MaintenanceJob} {
			writes.add(opsCollectWrite{Path: opsPath("jobs", jobID, "verifications", runID+":check"), Body: mustJSON(check)}, order)
		}
	}
}

func resticPhase(report reportFile, phase string) (map[string]any, bool) {
	value, ok := report.fields[phase].(map[string]any)
	return value, ok
}

func resticExitCode(phase map[string]any) (int, bool) {
	code, ok := phase["exit_code"].(float64)
	return int(code), ok
}

func isResticMaintenanceWarning(warning string) bool {
	return strings.HasPrefix(warning, "Prune failed") || strings.HasPrefix(warning, "Repository check failed")
}

// resticBackupRun maps a run report onto Ops fields. Its state comes from the
// capture and retention only; prune and check go to the maintenance job.
func resticBackupRun(source opsCollectSource, runID string, report reportFile) map[string]any {
	run := withField(report.fields, "run_id", runID)
	run["job_id"] = source.JobID
	run["runner_kind"] = report.text("kind")
	run["kind"] = "backup"
	run["host"] = source.HostID
	run["engine"] = "restic"
	run["runner"] = "den-offsite-backup"
	run["local_run_directory"] = "den-nas:" + source.StateDir + "/runs/" + runID
	run["log_path"] = "den-nas:" + source.StateDir + "/runs/" + runID + "/backup.stderr"

	warnings := []string{}
	maintenanceProblem := false
	for _, warning := range anyStrings(report.fields["warnings"]) {
		if isResticMaintenanceWarning(warning) {
			maintenanceProblem = true
			continue
		}
		warnings = append(warnings, warning)
	}
	run["warnings"] = warnings
	if _, finished := report.time("finished_at"); finished {
		switch report.text("state") {
		case "error":
			run["state"] = "failed"
			run["error"] = firstNonEmpty(strings.Join(warnings, "; "), "the offsite backup run failed")
		default:
			run["state"] = "success"
			if len(warnings) > 0 {
				run["state"] = "warning"
			}
		}
	}

	if snapshot := report.text("snapshot_id"); snapshot != "" {
		run["archive"] = map[string]any{"id": snapshot, "name": snapshot[:min(8, len(snapshot))]}
	}
	notices := []string{}
	if backup, ok := resticPhase(report, "backup"); ok {
		if summary, ok := backup["summary"].(map[string]any); ok {
			stats := map[string]any{}
			if value, ok := summary["total_bytes_processed"].(float64); ok {
				stats["original_size"] = value
			}
			if value, ok := summary["data_added_packed"].(float64); ok {
				// Newly stored bytes for this run, after deduplication and compression.
				stats["deduplicated_size"] = value
				notices = append(notices, fmt.Sprintf("Newly stored in B2 by this run: %s (packed). This is not total bucket usage.", decimalBytes(value)))
			}
			if value, ok := summary["total_files_processed"].(float64); ok {
				stats["nfiles"] = value
			}
			if len(stats) > 0 {
				run["stats"] = stats
			}
		}
	}
	if repositories, ok := report.fields["git_repositories"].([]any); ok {
		names := []string{}
		for _, item := range repositories {
			if repository, ok := item.(map[string]any); ok {
				names = append(names, fmt.Sprintf("%v (%d refs)", repository["repository"], len(anyStrings(repository["refs"]))))
			}
		}
		notices = append(notices, "Git captured as pushed history only: "+strings.Join(names, ", "))
	}
	if maintenanceProblem {
		notices = append(notices, fmt.Sprintf(resticMaintenanceNote, source.MaintenanceJob)+" It had a problem; the capture is unaffected.")
	}
	run["source_notices"] = notices
	return run
}

// resticMaintenanceRun reports the prune and check phases of a run (the
// Saturday maintenance) or of a standalone maintenance report.
func resticMaintenanceRun(source opsCollectSource, runID string, report reportFile) (map[string]any, bool) {
	prune, hasPrune := resticPhase(report, "prune")
	check, hasCheck := resticPhase(report, "check")
	phase := report.text("phase")
	running := phase == "prune" || phase == "check"
	if report.text("kind") != "maintenance" && !hasPrune && !hasCheck && !running {
		return nil, false
	}
	run := map[string]any{
		"job_id": source.MaintenanceJob, "run_id": runID, "kind": "maintenance", "host": source.HostID,
		"engine": "restic", "runner": "den-offsite-backup", "phase": phase,
		"started_at": report.fields["started_at"], "updated_at": report.fields["updated_at"],
		"local_run_directory": "den-nas:" + source.StateDir + "/runs/" + runID,
		"state":               "running",
	}
	if _, finished := report.time("finished_at"); !finished {
		return run, true
	}
	run["finished_at"] = report.fields["finished_at"]
	var failures []string
	if code, ok := resticExitCode(prune); hasPrune && (!ok || code != 0) {
		failures = append(failures, fmt.Sprintf("prune exited %d", code))
	}
	if code, ok := resticExitCode(check); hasCheck && (!ok || code != 0) {
		failures = append(failures, fmt.Sprintf("check exited %d", code))
	}
	if !hasPrune && !hasCheck && report.text("state") == "error" {
		failures = append(failures, strings.Join(anyStrings(report.fields["warnings"]), "; "))
	}
	run["state"] = "success"
	if len(failures) > 0 {
		run["state"] = "failed"
		run["error"] = strings.Join(failures, "; ")
	}
	phases := []string{}
	if code, ok := resticExitCode(prune); ok {
		phases = append(phases, fmt.Sprintf("prune exit %d", code))
	}
	if code, ok := resticExitCode(check); ok {
		readData, _ := resticReadData(report, check)
		phases = append(phases, fmt.Sprintf("check (%s) exit %d", resticCheckName(readData), code))
	}
	run["source_notices"] = phases
	return run, true
}

// resticReadData says whether the check read every pack. Reports from before
// the runner recorded it are judged by the runner's rule: --read-data when the
// Pacific day of the month is at most 7.
func resticReadData(report reportFile, check map[string]any) (readData bool, inferred bool) {
	if value, ok := check["read_data"].(bool); ok {
		return value, false
	}
	at, ok := report.time("updated_at")
	if !ok {
		at, _ = report.time("started_at")
	}
	if location, err := time.LoadLocation(resticZone); err == nil {
		at = at.In(location)
	}
	return at.Day() <= 7, true
}

func resticCheckName(readData bool) string {
	if readData {
		return "--read-data"
	}
	return "metadata"
}

func resticCheck(source opsCollectSource, runID string, report reportFile) (map[string]any, bool) {
	check, ok := resticPhase(report, "check")
	if !ok {
		return nil, false
	}
	code, ok := resticExitCode(check)
	at := report.text("finished_at")
	if !ok || at == "" {
		return nil, false
	}
	readData, inferred := resticReadData(report, check)
	scope := "restic check of the repository structure and metadata; no pack data read"
	if readData {
		scope = "restic check --read-data: read and verified every stored pack"
	}
	if inferred {
		scope += " (scope inferred from the runner's first-week rule; the report predates read_data)"
	}
	state := "success"
	if code != 0 {
		state = "failed"
	}
	return map[string]any{
		"kind": "restic-check", "state": state, "at": at, "run_id": runID,
		"archive": report.text("snapshot_id"), "full_data_read": readData, "scope": scope,
		"evidence_path": "den-nas:" + source.StateDir + "/runs/" + runID + "/check.jsonl",
	}, true
}

// resticRestoreTest reports the installation restore test: sample files
// byte-compared and Git bundles rebuilt and fsck'd. It is not a full read.
func resticRestoreTest(source opsCollectSource, report reportFile) (map[string]any, string, bool) {
	tested, ok := report.time("tested_at")
	if !ok {
		return nil, "", false
	}
	files, compared := []string{}, 0
	failed := report.text("state") != "success"
	for _, item := range anySlice(report.fields["files"]) {
		file, _ := item.(map[string]any)
		files = append(files, fmt.Sprint(file["source"]))
		if file["matches_source"] == true {
			compared++
		} else {
			failed = true
		}
	}
	repositories := []string{}
	for _, item := range anySlice(report.fields["git_repositories"]) {
		repository, _ := item.(map[string]any)
		repositories = append(repositories, fmt.Sprint(repository["repository"]))
		if code, _ := repository["fsck_exit_code"].(float64); code != 0 || repository["metadata_applied"] != true {
			failed = true
		}
	}
	state := "success"
	if failed {
		state = "failed"
	}
	scope := fmt.Sprintf("Sample restore from B2: %d of %d files byte-identical to the source; %d Git bundles restored with refs and metadata and passed git fsck; restic metadata check %v. Not a read of every stored pack.",
		compared, len(files), len(repositories), report.fields["restic_metadata_check"])
	return map[string]any{
		"kind": "restore_test", "state": state, "at": report.fields["tested_at"],
		"archive": report.text("snapshot_id"), "scope": scope, "full_data_read": false,
		"restored_to":    "den-nas:" + report.text("directory"),
		"evidence_path":  "den-nas:" + source.StateDir + "/restore-verification.json",
		"files_compared": files, "repositories_checked": repositories,
	}, "restore-test-" + compactUTC(tested), true
}

// resticInitialPrune reports the prune run at installation.
func resticInitialPrune(source opsCollectSource, report reportFile) (map[string]any, string, bool) {
	tested, ok := report.time("tested_at")
	code, hasCode := report.fields["prune_exit_code"].(float64)
	if !ok || !hasCode {
		return nil, "", false
	}
	run := map[string]any{
		"job_id": source.MaintenanceJob, "kind": "maintenance", "host": source.HostID, "engine": "restic",
		"runner": "den-offsite-backup", "phase": "prune", "started_at": report.fields["tested_at"],
		"finished_at": report.fields["tested_at"], "exit_code": code, "state": "success",
		"log_path":       "den-nas:" + report.text("log"),
		"source_notices": []string{"Prune at installation; no check ran."},
	}
	if code != 0 {
		run["state"] = "failed"
		run["error"] = fmt.Sprintf("prune exited %d", int(code))
	}
	return run, "initial-prune-" + compactUTC(tested), true
}

// resticRegistrationWithBucket adds the dated bucket observation to the
// configured registration's notes.
func resticRegistrationWithBucket(registration json.RawMessage, bucket reportFile) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(registration, &body); err != nil {
		return nil, fmt.Errorf("offsite job registration: %w", err)
	}
	stored, ok := bucket.fields["all_uploaded_version_bytes"].(float64)
	checked := bucket.text("checked_at")
	if !ok || checked == "" {
		return nil, fmt.Errorf("bucket-status.json has no all_uploaded_version_bytes or checked_at")
	}
	note := fmt.Sprintf("B2 bucket %v held %s in %v current files (all stored versions) as observed at %s (bucket-status.json); backup runs do not refresh this figure.",
		bucket.fields["bucket"], decimalBytes(stored), bucket.fields["visible_current_files"], checked)
	notes, _ := body["notes"].(string)
	body["notes"] = strings.TrimSpace(notes + " " + note)
	return mustJSON(body), nil
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

// decimalBytes formats bytes in decimal units (GB), as B2 bills them.
func decimalBytes(value float64) string {
	units := []string{"B", "kB", "MB", "GB", "TB"}
	unit := 0
	for value >= 1000 && unit < len(units)-1 {
		value /= 1000
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f B", value)
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}
