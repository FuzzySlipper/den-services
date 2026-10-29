package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The GitHub source-ZIP job (den-github-backup) archives each repository's
// default branch on the NAS. This source reports each repository as its own
// job and the daily set as the aggregate job. A source ZIP is not Git history,
// local work, LFS payloads or submodules, and its ZIP CRC check is a
// verification, never a restore test.

const (
	githubLFSNote = "Git LFS payloads are not in the ZIP unless GitHub includes them; pointer files found: %v"
	// githubZone is the zone whose calendar date defines a daily set.
	githubZone = "America/Los_Angeles"
)

func collectGitHubZip(source opsCollectSource, env opsCollectEnv) ([]opsCollectWrite, error) {
	files, err := recentFiles(env, filepath.Join(source.StateDir, "reports", "*.json"))
	if err != nil {
		return nil, err
	}
	summary, summaryErr := readReportFile(filepath.Join(source.StateDir, "latest.json"))
	writes := newWriteSet()
	var problems []string
	if summaryErr != nil && !errors.Is(summaryErr, os.ErrNotExist) {
		problems = append(problems, summaryErr.Error())
	}
	repositories := map[string]reportFile{} // job_id -> newest report, for registration
	for _, file := range files {
		if strings.HasSuffix(file, "-last-success.json") {
			continue // a copy of a run that is reported from its own file
		}
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
		if previous, ok := repositories[jobID]; !ok || report.order() >= previous.order() {
			repositories[jobID] = report
		}
		writes.add(opsCollectWrite{Path: opsPath("jobs", jobID, "runs", runID), Body: mustJSON(githubRepositoryRun(report))}, report.order())
		if check := githubZipCheck(report); check != nil {
			writes.add(opsCollectWrite{Path: opsPath("jobs", jobID, "verifications", runID+":zip-crc"), Body: mustJSON(check)}, report.order())
		}
	}

	registrations := []opsCollectWrite{}
	for _, jobID := range sortedKeys(repositories) {
		body := githubRepositoryJob(source, repositories[jobID], summary)
		registrations = append(registrations, opsCollectWrite{Path: opsPath("jobs", jobID), Body: mustJSON(body)})
	}
	if summaryErr == nil {
		registrations = append(registrations, opsCollectWrite{Path: opsPath("jobs", source.JobID), Body: mustJSON(githubAggregateJob(source, summary))})
		for _, name := range []string{"latest.json", "last-success.json"} {
			report, err := readReportFile(filepath.Join(source.StateDir, name))
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					problems = append(problems, err.Error())
				}
				continue
			}
			if run, ok := githubAggregateRun(report); ok {
				writes.add(opsCollectWrite{Path: opsPath("jobs", source.JobID, "runs", report.text("run_id")), Body: mustJSON(run)}, report.order())
			}
		}
	}
	return append(registrations, writes.list()...), joinProblems(problems)
}

// githubRepositoryRun maps a repository report onto the Ops run fields. The
// original fields stay in the body; the CRC check is reported separately so
// it is never mistaken for a restore.
func githubRepositoryRun(report reportFile) map[string]any {
	run := withField(report.fields, "runner", "den-github-backup")
	delete(run, "verification")
	if snapshot := report.text("snapshot_id"); snapshot != "" {
		run["archive"] = map[string]any{"id": snapshot, "name": snapshot}
	}
	stats := map[string]any{}
	if size, ok := report.fields["archive_bytes"].(float64); ok {
		stats["compressed_size"] = size
	}
	if check, ok := report.fields["verification"].(map[string]any); ok {
		if size, ok := check["uncompressed_bytes"].(float64); ok {
			stats["original_size"] = size
		}
		if files, ok := check["files"].(float64); ok {
			stats["nfiles"] = files
		}
	}
	if len(stats) > 0 {
		run["stats"] = stats
	}
	if path := report.text("archive_path"); path != "" {
		run["target"] = "den-nas:" + path
		run["recovery_pointer"] = "den-nas:" + path
	}
	run["source_notices"] = githubCoverageNotes(report)
	warnings := []string{}
	if retention, ok := report.fields["retention"].(map[string]any); ok {
		for _, warning := range anyStrings(retention["warnings"]) {
			warnings = append(warnings, "Retention: "+warning)
		}
	}
	if publication, ok := report.fields["report_publication"].(map[string]any); ok && publication["state"] == "failed" {
		warnings = append(warnings, fmt.Sprintf("Status was not published to den-nas: %v", publication["error"]))
	}
	run["warnings"] = warnings
	if run["state"] == "success" && len(warnings) > 0 {
		run["state"] = "warning"
	}
	return run
}

func githubZipCheck(report reportFile) map[string]any {
	check, ok := report.fields["verification"].(map[string]any)
	if !ok || check["at"] == nil {
		return nil
	}
	return map[string]any{
		"kind": "zip-crc-check", "state": check["state"], "at": check["at"], "run_id": report.text("run_id"),
		"archive": report.text("snapshot_id"), "full_data_read": true,
		"scope": fmt.Sprintf("NAS check at publication (%v) over %v files; not a restore", check["method"], check["files"]),
	}
}

func githubCoverageNotes(report reportFile) []string {
	notes := anyStrings(report.fields["coverage_notes"])
	if check, ok := report.fields["verification"].(map[string]any); ok && check["lfs_pointer_files"] != nil {
		notes = append(notes, fmt.Sprintf(githubLFSNote, check["lfs_pointer_files"]))
	}
	return notes
}

// githubKeepDaily reads the repository's retention, falling back to the
// summary's override for it and then the summary default.
func githubKeepDaily(report reportFile, summary reportFile) any {
	if value, ok := report.fields["keep_daily"]; ok {
		return value
	}
	if retention, ok := report.fields["retention"].(map[string]any); ok && retention["keep_daily"] != nil {
		return retention["keep_daily"]
	}
	if overrides, ok := summary.fields["keep_daily_overrides"].(map[string]any); ok && overrides[report.text("repository")] != nil {
		return overrides[report.text("repository")]
	}
	return summary.fields["keep_daily"]
}

func githubRepositoryJob(source opsCollectSource, report reportFile, summary reportFile) map[string]any {
	repository := report.text("repository")
	keep := githubKeepDaily(report, summary)
	retention := "unknown (no keep_daily reported)"
	if keep != nil {
		retention = fmt.Sprintf("newest snapshot for each of %v successful Pacific dates", keep)
	}
	return map[string]any{
		"host_id": source.HostID, "display_name": repository + " source ZIP", "engine": "github-source-zip", "kind": "backup",
		"source": "github.com/" + repository + " (default branch at an exact commit)",
		"scope": firstNonEmpty(report.text("scope"), "GitHub default-branch source ZIP, not Git history or local uncommitted files") +
			"; submodule repositories are not fetched; Git LFS payloads only if GitHub includes them",
		"target":           "den-nas:/mnt/storage/backup/github/" + repository,
		"schedule":         map[string]any{"timezone": "America/Los_Angeles", "times": []string{"07:00"}, "weekdays": []string{}, "grace": "3h"},
		"retention":        retention,
		"recovery_pointer": "den-nas:/mnt/storage/backup/github/" + repository + "/<snapshot>/source.zip (expand in an isolated directory)",
		"notes":            "Runner: den-github-backup.service, after the 07:00 Git backup; it skips (does not catch up) when started after 10:00.",
		"enabled":          true,
	}
}

func githubAggregateJob(source opsCollectSource, summary reportFile) map[string]any {
	overrides := ""
	if values, ok := summary.fields["keep_daily_overrides"].(map[string]any); ok {
		for _, repository := range sortedKeys(values) {
			overrides += fmt.Sprintf("; %s keeps %v", repository, values[repository])
		}
	}
	return map[string]any{
		"host_id": source.HostID, "display_name": "GitHub source ZIP set", "engine": "github-source-zip", "kind": "backup",
		"source": "github.com default branches of the configured repositories",
		"scope":  "Complete only when every configured repository has a snapshot for the same Pacific date",
		"target": "den-nas:/mnt/storage/backup/github",
		"schedule": map[string]any{
			"timezone": "America/Los_Angeles", "times": []string{"07:00"}, "weekdays": []string{}, "grace": "3h",
		},
		"retention":        fmt.Sprintf("%v daily points per repository%s", summary.fields["keep_daily"], overrides),
		"recovery_pointer": "den-agents ~/.local/lib/den-github-backup/README.md; den-nas:/mnt/storage/backup/github",
		"notes":            "Per-repository results are the github-fuzzyslipper-* jobs.",
		"enabled":          true,
	}
}

// githubAggregateRun turns a summary into a run. A skipped summary stays
// skipped, so it never refreshes freshness; a success that does not cover
// every repository for the date is a warning, not a complete set.
func githubAggregateRun(summary reportFile) (map[string]any, bool) {
	if summary.text("run_id") == "" || summary.text("started_at") == "" {
		return nil, false
	}
	run := map[string]any{}
	for key, value := range summary.fields {
		if key != "jobs" {
			run[key] = value
		}
	}
	run["runner"] = "den-github-backup"
	// The set's date is the Pacific date the run started, as the runner
	// defines it; older summaries do not record it.
	date := summary.text("date")
	if started, ok := summary.time("started_at"); ok && date == "" {
		if location, err := time.LoadLocation(githubZone); err == nil {
			date = started.In(location).Format("2006-01-02")
		}
	}
	run["date"] = date
	jobs, _ := summary.fields["jobs"].([]any)
	var repositories []any
	var missing, notToday []string
	for _, item := range jobs {
		job, _ := item.(map[string]any)
		name := fmt.Sprint(job["repository"])
		repositories = append(repositories, map[string]any{
			"repository": job["repository"], "job_id": job["job_id"], "state": job["state"],
			"date": job["date"], "commit": job["commit"], "archive_bytes": job["archive_bytes"],
		})
		switch {
		case job["state"] == "missing":
			missing = append(missing, name)
		case job["backup_complete"] != true || job["date"] != date:
			notToday = append(notToday, name)
		}
	}
	run["repositories"] = repositories
	warnings := anyStrings(summary.fields["warnings"])
	if publication, ok := summary.fields["report_publication"].(map[string]any); ok && publication["state"] == "failed" {
		warnings = append(warnings, fmt.Sprintf("Summary was not published to den-nas: %v", publication["error"]))
	}
	complete := len(jobs) > 0 && len(missing) == 0 && len(notToday) == 0
	if summary.text("state") == "skipped" {
		complete = false
	}
	run["backup_complete"] = complete
	switch summary.text("state") {
	case "skipped":
	case "failed":
		if len(missing) > 0 {
			run["error"] = "no report for: " + strings.Join(missing, ", ")
		} else if _, ok := run["error"]; !ok {
			run["error"] = "at least one repository capture failed; see the github-fuzzyslipper-* jobs"
		}
	default:
		if !complete {
			run["state"] = "warning"
			warnings = append(warnings, fmt.Sprintf("The %s set is incomplete: no completed snapshot for that date from %s",
				date, strings.Join(notToday, ", ")))
		}
	}
	run["warnings"] = warnings
	return run, true
}

func anyStrings(value any) []string {
	items, _ := value.([]any)
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprint(item))
	}
	return values
}
