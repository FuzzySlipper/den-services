package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// collectBorgRunner reports the den-agents Borg runner's own files: each run's
// result.json, latest.json (heartbeats while a run is in progress) and the
// restore tests. Maintenance runs go to their own job, and their repository
// check is also recorded on the backup job it checked.
func collectBorgRunner(source opsCollectSource, env opsCollectEnv) ([]opsCollectWrite, error) {
	files, err := recentFiles(env, filepath.Join(source.StateDir, "runs", "*", "result.json"))
	if err != nil {
		return nil, err
	}
	files = append(files, filepath.Join(source.StateDir, "latest.json"))
	writes := newWriteSet()
	var problems []string
	for _, file := range files {
		report, err := readReportFile(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		jobID, runID := report.text("job_id"), report.text("run_id")
		if report.text("kind") == "maintenance" && source.MaintenanceJob != "" {
			report.fields["job_id"] = source.MaintenanceJob
			if check, ok := report.fields["verification"].(map[string]any); ok {
				check = withField(check, "run_id", runID)
				writes.add(opsCollectWrite{Path: opsPath("jobs", jobID, "verifications", runID+":verification"), Body: mustJSON(check)}, "")
			}
			if restore, ok := report.fields["restore_test"].(map[string]any); ok {
				if id := restoreTestID("restore-test", restore); id != "" {
					writes.add(opsCollectWrite{Path: opsPath("jobs", jobID, "verifications", id), Body: mustJSON(restore)}, "")
				}
			}
			jobID = source.MaintenanceJob
		}
		writes.add(opsCollectWrite{Path: opsPath("jobs", jobID, "runs", runID), Body: report.encode()}, report.order())
	}
	// The standalone restore-test files win over copies embedded in a
	// maintenance report, so each test is always sent with the same body.
	for _, name := range []string{"restore-test", "real-project-restore"} {
		report, err := readReportFile(filepath.Join(source.StateDir, name+".json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if id := restoreTestID(name, report.fields); id != "" && report.text("job_id") != "" {
			writes.replace(opsCollectWrite{Path: opsPath("jobs", report.text("job_id"), "verifications", id), Body: report.raw})
		}
	}
	return writes.list(), joinProblems(problems)
}

// collectThinConfig reports the thin configuration backups. Each host's
// capture and its local restore check are one run; the local repository check
// and the NAS recovery copy are reported separately, so a stale NAS copy or a
// failed repository check is never hidden behind a good capture.
func collectThinConfig(source opsCollectSource, env opsCollectEnv) ([]opsCollectWrite, error) {
	files, err := recentFiles(env, filepath.Join(source.StateDir, "reports", "*.json"))
	if err != nil {
		return nil, err
	}
	writes := newWriteSet()
	var problems []string
	jobIDs := map[string]bool{}
	for _, file := range files {
		report, err := readReportFile(file)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		jobID, runID := report.text("job_id"), report.text("run_id")
		if jobID == "" || runID == "" {
			problems = append(problems, file+": no job_id or run_id")
			continue
		}
		jobIDs[jobID] = true
		report.fields["runner"] = "den-config-backup"
		// The thin runner's "verification" extracts the new archive and checks
		// its contents: that is a restore test of the local repository.
		if check, ok := report.fields["verification"].(map[string]any); ok {
			delete(report.fields, "verification")
			check = withField(check, "kind", "restore_test")
			if archive, ok := report.fields["archive"].(map[string]any); ok {
				check = withField(check, "archive", archive["name"])
			}
			report.fields["restore_test"] = check
		}
		writes.add(opsCollectWrite{Path: opsPath("jobs", jobID, "runs", runID), Body: report.encode()}, report.order())
	}
	maintenance, err := readReportFile(filepath.Join(source.StateDir, "maintenance.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		problems = append(problems, err.Error())
	default:
		if at, ok := maintenance.time("at"); ok && env.now.Sub(at) <= env.lookback {
			check := map[string]any{
				"kind":  "repository-check",
				"state": maintenance.text("state"),
				"at":    maintenance.fields["at"],
				"scope": fmt.Sprintf("den-agents local Borg repository: borg check --repository-only exit %v, compact exit %v",
					maintenance.fields["check_exit"], maintenance.fields["compact_exit"]),
				"detail": maintenance.fields["detail"],
			}
			for _, jobID := range sortedKeys(jobIDs) {
				writes.add(opsCollectWrite{Path: opsPath("jobs", jobID, "verifications", "local-repository-check-"+compactUTC(at)), Body: mustJSON(check)}, "")
			}
		}
	}
	if source.NASCopyJob != "" {
		problems = append(problems, collectThinNASCopy(source, env, writes)...)
	}
	return writes.list(), joinProblems(problems)
}

func collectThinNASCopy(source opsCollectSource, env opsCollectEnv, writes *writeSet) []string {
	var problems []string
	summary, err := readReportFile(filepath.Join(source.StateDir, "latest.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		problems = append(problems, err.Error())
	default:
		if run, ok := thinNASCopyRun(summary); ok {
			writes.add(opsCollectWrite{Path: opsPath("jobs", source.NASCopyJob, "runs", summary.text("run_id")), Body: mustJSON(run)}, "")
		}
	}
	verification, err := readReportFile(filepath.Join(source.StateDir, "mirror-verification.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		problems = append(problems, err.Error())
	default:
		at, ok := verification.time("at")
		if !ok {
			return append(problems, "mirror-verification.json has no at")
		}
		generation := verification.text("generation")
		check := map[string]any{
			"kind": "nas-copy-check", "state": verification.text("state"), "at": verification.fields["at"],
			"scope": verification.text("scope"), "archive": generation, "full_data_read": true,
			"repositories_checked": []any{verification.fields["repository"]},
		}
		writes.add(opsCollectWrite{Path: opsPath("jobs", source.NASCopyJob, "verifications", "nas-copy-check-"+compactUTC(at)), Body: mustJSON(check)}, "")
		restores, _ := verification.fields["restores"].([]any)
		state, checked := "success", []any{}
		for _, item := range restores {
			restore, _ := item.(map[string]any)
			result, _ := restore["verification"].(map[string]any)
			itemState, _ := result["state"].(string)
			state = worseCheckState(state, itemState)
			checked = append(checked, fmt.Sprintf("%v: %v %v", restore["host"], restore["archive"], itemState))
		}
		if len(restores) > 0 {
			restore := map[string]any{
				"kind": "restore_test", "state": state, "at": verification.fields["at"], "archive": generation,
				"scope":   fmt.Sprintf("NAS copy %s: isolated extract and content check of %d host archives", generation, len(restores)),
				"checked": checked,
			}
			writes.add(opsCollectWrite{Path: opsPath("jobs", source.NASCopyJob, "verifications", "nas-copy-restore-"+compactUTC(at)), Body: mustJSON(restore)}, "")
		}
	}
	return problems
}

// thinNASCopyRun turns the thin runner's summary into a run of the NAS copy
// job. A busy NAS lock is a warning that keeps the previous copy current; any
// other publication problem is a failure.
func thinNASCopyRun(summary reportFile) (map[string]any, bool) {
	mirror, ok := summary.fields["nas_mirror"].(map[string]any)
	finished, hasTime := summary.time("at")
	if !ok || !hasTime || summary.text("run_id") == "" {
		return nil, false
	}
	// The copy starts once the last capture of the same run has finished.
	started := time.Time{}
	jobs, _ := summary.fields["jobs"].([]any)
	for _, item := range jobs {
		job, _ := item.(map[string]any)
		runID, _ := job["run_id"].(string)
		if !strings.HasPrefix(runID, summary.text("run_id")+"-") {
			continue
		}
		if at, ok := parseReportTime(job["finished_at"]); ok && at.After(started) && !at.After(finished) {
			started = at
		}
	}
	if started.IsZero() {
		started = finished
	}
	mirrorState, _ := mirror["state"].(string)
	detail, _ := mirror["detail"].(string)
	reason, _ := mirror["reason"].(string)
	run := map[string]any{
		"run_id": summary.text("run_id"), "host": "den-agents", "engine": "ssh-tar", "kind": "replication",
		"runner": "den-config-backup", "started_at": started.Format(time.RFC3339Nano), "finished_at": finished.Format(time.RFC3339Nano),
		"target": "den-nas:/mnt/storage/backup/host-configs",
	}
	warnings := []string{}
	switch mirrorState {
	case "success":
		run["state"] = "success"
		run["backup_complete"] = true
		if path, _ := mirror["path"].(string); path != "" {
			run["target"] = "den-nas:" + path
			run["archive"] = map[string]any{"id": filepath.Base(path), "name": filepath.Base(path)}
		}
		if bytes, ok := mirror["bytes"].(float64); ok {
			run["stats"] = map[string]any{"original_size": bytes}
		}
	case "deferred":
		run["state"] = "warning"
		run["backup_complete"] = false
		warnings = append(warnings, "NAS copy deferred ("+firstNonEmpty(reason, "NAS maintenance lock busy")+"); the previous NAS generation is still the current copy")
	default:
		run["state"] = "failed"
		run["error"] = strings.TrimSpace(firstNonEmpty(reason, "NAS recovery copy failed") + "\n" + detail)
	}
	if publication, ok := summary.fields["report_publication"].(map[string]any); ok && publication["state"] != "success" {
		warnings = append(warnings, fmt.Sprintf("Status JSON was not published to den-nas: %v", publication["detail"]))
		if run["state"] == "success" {
			run["state"] = "warning"
		}
	}
	run["warnings"] = warnings
	return run, true
}

// reportFile is a JSON report kept as both its raw bytes and its fields, so an
// unchanged report is sent exactly as the job wrote it.
type reportFile struct {
	path    string
	raw     []byte
	fields  map[string]any
	changed bool
}

func readReportFile(path string) (reportFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return reportFile{}, err
	}
	report := reportFile{path: path, raw: data}
	if err := json.Unmarshal(data, &report.fields); err != nil || report.fields == nil {
		return reportFile{}, fmt.Errorf("%s is not a JSON object", path)
	}
	return report, nil
}

func (r reportFile) text(field string) string {
	value, _ := r.fields[field].(string)
	return value
}

func (r reportFile) time(field string) (time.Time, bool) {
	return parseReportTime(r.fields[field])
}

// encode returns the original bytes unless a field was changed.
func (r reportFile) encode() []byte {
	original := map[string]any{}
	if json.Unmarshal(r.raw, &original) == nil && jsonEqual(original, r.fields) {
		return r.raw
	}
	return mustJSON(r.fields)
}

// order ranks two copies of the same run: a finished copy beats a heartbeat,
// then the newer update wins.
func (r reportFile) order() string {
	finished := "0"
	if _, ok := r.time("finished_at"); ok {
		finished = "1"
	}
	updated, ok := r.time("updated_at")
	if !ok {
		updated, _ = r.time("started_at")
	}
	return finished + updated.UTC().Format(time.RFC3339Nano)
}

func parseReportTime(value any) (time.Time, bool) {
	text, _ := value.(string)
	if text == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	return parsed, err == nil
}

// writeSet keeps one write per path. When two copies of the same run are
// found, the one with the higher order wins, so a collection is stable.
type writeSet struct {
	paths  []string
	writes map[string]opsCollectWrite
	orders map[string]string
}

func newWriteSet() *writeSet {
	return &writeSet{writes: map[string]opsCollectWrite{}, orders: map[string]string{}}
}

func (s *writeSet) add(write opsCollectWrite, order string) {
	if existing, ok := s.orders[write.Path]; ok && existing > order {
		return
	}
	if _, ok := s.writes[write.Path]; !ok {
		s.paths = append(s.paths, write.Path)
	}
	s.writes[write.Path] = write
	s.orders[write.Path] = order
}

func (s *writeSet) replace(write opsCollectWrite) {
	if _, ok := s.writes[write.Path]; !ok {
		s.paths = append(s.paths, write.Path)
	}
	s.writes[write.Path] = write
	s.orders[write.Path] = "\xff"
}

func (s *writeSet) list() []opsCollectWrite {
	writes := make([]opsCollectWrite, 0, len(s.paths))
	for _, path := range s.paths {
		writes = append(writes, s.writes[path])
	}
	return writes
}

// recentFiles lists matching files changed within the lookback, oldest first.
func recentFiles(env opsCollectEnv, pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	type dated struct {
		path string
		at   time.Time
	}
	var files []dated
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || !info.Mode().IsRegular() || env.now.Sub(info.ModTime()) > env.lookback {
			continue
		}
		files = append(files, dated{match, info.ModTime()})
	}
	slices.SortFunc(files, func(a, b dated) int { return a.at.Compare(b.at) })
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.path)
	}
	return paths, nil
}

func restoreTestID(prefix string, report map[string]any) string {
	at, ok := parseReportTime(report["at"])
	if !ok {
		return ""
	}
	return prefix + "-" + compactUTC(at)
}

func withField(values map[string]any, key string, value any) map[string]any {
	copied := make(map[string]any, len(values)+1)
	for k, v := range values {
		copied[k] = v
	}
	copied[key] = value
	return copied
}

func worseCheckState(current string, next string) string {
	rank := map[string]int{"success": 0, "warning": 1, "failed": 2}
	if _, known := rank[next]; !known {
		next = "failed"
	}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

func jsonEqual(a, b any) bool {
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && string(left) == string(right)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func joinProblems(problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}
