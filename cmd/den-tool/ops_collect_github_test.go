package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func githubRepoReport(repository string, runID string, state string, started time.Time, date string) map[string]any {
	slug := strings.ToLower(strings.ReplaceAll(repository, "/", "-"))
	report := map[string]any{
		"schema_version": 1, "job_id": "github-" + slug, "run_id": runID, "host": "den-agents",
		"engine": "github-source-zip", "runner": "den-github-backup", "kind": "backup", "repository": repository,
		"scope": "GitHub default-branch source ZIP, not Git history or local uncommitted files",
		"state": state, "started_at": ts(started), "updated_at": ts(started.Add(time.Minute)), "date": date,
		"backup_complete": state == "success", "report_publication": map[string]any{"state": "success"},
	}
	switch state {
	case "success":
		snapshot := date + "_abc_" + runID
		report["phase"], report["finished_at"] = "complete", ts(started.Add(time.Minute))
		report["branch"], report["commit"], report["snapshot_id"] = "main", "abc123", snapshot
		report["archive_path"], report["archive_bytes"] = "/mnt/storage/backup/github/"+repository+"/"+snapshot+"/source.zip", 1000.0
		report["verification"] = map[string]any{
			"state": "success", "at": ts(started.Add(time.Minute)), "method": "all ZIP member CRCs; no extraction",
			"files": 10.0, "uncompressed_bytes": 4000.0, "lfs_pointer_files": 0.0,
			"coverage_notes": []string{"GitHub source ZIP only: no Git history or uncommitted local files"},
		}
		report["coverage_notes"] = []string{"GitHub source ZIP only: no Git history or uncommitted local files"}
		report["retention"] = map[string]any{"daily_points_retained": 2.0, "removed": []string{}, "warnings": []string{}}
	case "failed":
		report["phase"], report["finished_at"] = "failed", ts(started.Add(time.Minute))
		report["error"] = "GitHub API request failed: HTTP 502"
	}
	return report
}

// githubFixture writes a den-github-backup state directory for two repos:
// alpha failed today after succeeding yesterday, beta succeeded today. The
// latest summary is a quiet-window skip; last-success is yesterday's set.
func githubFixture(t *testing.T, dir string, now time.Time) {
	t.Helper()
	pacific, _ := time.LoadLocation("America/Los_Angeles")
	today := now.In(pacific).Format("2006-01-02")
	yesterday := now.Add(-24 * time.Hour).In(pacific).Format("2006-01-02")
	earlier, later := now.Add(-25*time.Hour), now.Add(-time.Hour)
	alphaOld := githubRepoReport("Owner/alpha", "R1", "success", earlier, yesterday)
	alphaOld["keep_daily"] = 7.0
	betaOld := githubRepoReport("Owner/beta", "R1", "success", earlier, yesterday)
	alphaNew := githubRepoReport("Owner/alpha", "R2", "failed", later, today)
	betaNew := githubRepoReport("Owner/beta", "R2", "success", later, today)
	betaNew["retention"] = map[string]any{"removed": []string{}, "warnings": []string{"could not remove 2026-09-01 snapshot"}}
	for name, report := range map[string]map[string]any{
		"R1-Owner--alpha.json": alphaOld, "R1-Owner--beta.json": betaOld, "R2-Owner--alpha.json": alphaNew,
		"R2-Owner--beta.json": betaNew, "Owner--alpha-latest.json": alphaNew, "Owner--beta-latest.json": betaNew,
		"Owner--alpha-last-success.json": alphaOld, "Owner--beta-last-success.json": betaNew,
	} {
		writeJSONFile(t, filepath.Join(dir, "reports", name), report)
	}
	set := func(runID string, state string, started time.Time, jobs []any) map[string]any {
		return map[string]any{
			"schema_version": 1, "job_id": "github-archives", "run_id": runID, "host": "den-agents", "engine": "github-source-zip",
			"kind": "backup", "started_at": ts(started), "finished_at": ts(started.Add(5 * time.Minute)), "state": state,
			"keep_daily": 30, "keep_daily_overrides": map[string]any{"Owner/alpha": 7}, "jobs": jobs,
			"report_publication": map[string]any{"state": "success"},
		}
	}
	writeJSONFile(t, filepath.Join(dir, "last-success.json"), set("R1", "success", earlier, []any{alphaOld, betaOld}))
	skipped := set("R3", "skipped", now.Add(-10*time.Minute), []any{alphaNew, betaNew})
	skipped["reason"] = "Outside the 07:00-10:00 Pacific start window"
	writeJSONFile(t, filepath.Join(dir, "latest.json"), skipped)
}

func TestCollectGitHubZipReportsReposCRCAndSetRules(t *testing.T) {
	now := time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	githubFixture(t, dir, now)
	source := opsCollectSource{Type: "github-zip", StateDir: dir, JobID: "github-archives", HostID: "github"}
	writes, err := collectGitHubZip(source, testCollectEnv(now))
	if err != nil {
		t.Fatal(err)
	}
	bodies := writesByPath(t, writes)
	if got := bodies["/v1/ops/jobs/github-owner-alpha"]["retention"]; got != "newest snapshot for each of 7 successful Pacific dates" {
		t.Fatalf("alpha retention = %v", got)
	}
	if got := bodies["/v1/ops/jobs/github-archives"]["retention"]; got != "30 daily points per repository; Owner/alpha keeps 7" {
		t.Fatalf("aggregate retention = %v", got)
	}
	if scope := bodies["/v1/ops/jobs/github-owner-beta"]["scope"].(string); !strings.Contains(scope, "submodule repositories are not fetched") || !strings.Contains(scope, "not Git history") {
		t.Fatalf("scope = %q", scope)
	}
	failed := bodies["/v1/ops/jobs/github-owner-alpha/runs/R2"]
	if failed["state"] != "failed" || failed["error"] == nil {
		t.Fatalf("alpha failure = %v", failed)
	}
	beta := bodies["/v1/ops/jobs/github-owner-beta/runs/R2"]
	if beta["state"] != "warning" || beta["verification"] != nil || beta["archive"].(map[string]any)["id"] == nil {
		t.Fatalf("beta retention warning = %v", beta)
	}
	check := bodies["/v1/ops/jobs/github-owner-beta/verifications/R2:zip-crc"]
	if check["kind"] != "zip-crc-check" || !strings.Contains(check["scope"].(string), "not a restore") {
		t.Fatalf("CRC check = %v", check)
	}
	if skipped := bodies["/v1/ops/jobs/github-archives/runs/R3"]; skipped["state"] != "skipped" || skipped["backup_complete"] != false || skipped["jobs"] != nil {
		t.Fatalf("skipped set = %v", skipped)
	}
	if set := bodies["/v1/ops/jobs/github-archives/runs/R1"]; set["state"] != "success" || set["backup_complete"] != true {
		t.Fatalf("complete set = %v", set)
	}
}

func TestGitHubAggregateCompletesOnlyWithEveryRepoForTheDate(t *testing.T) {
	started := time.Date(2026, 10, 29, 17, 0, 0, 0, time.UTC) // 10:00 PDT, 2026-10-29
	job := func(name string, date string, state string) map[string]any {
		return map[string]any{"repository": name, "job_id": "github-" + name, "state": state, "date": date, "backup_complete": state == "success"}
	}
	for _, test := range []struct {
		name     string
		state    string
		jobs     []any
		want     string
		complete bool
	}{
		{"all repos today", "success", []any{job("a", "2026-10-29", "success"), job("b", "2026-10-29", "success")}, "success", true},
		{"partial retry", "success", []any{job("a", "2026-10-29", "success"), job("b", "2026-10-28", "success")}, "warning", false},
		{"missing repo", "failed", []any{job("a", "2026-10-29", "success"), map[string]any{"repository": "b", "state": "missing"}}, "failed", false},
		{"skipped", "skipped", []any{job("a", "2026-10-29", "success"), job("b", "2026-10-29", "success")}, "skipped", false},
	} {
		summary := reportFile{fields: map[string]any{"run_id": "R", "started_at": ts(started), "finished_at": ts(started), "state": test.state, "jobs": test.jobs}}
		run, ok := githubAggregateRun(summary)
		if !ok || run["state"] != test.want || run["backup_complete"] != test.complete || run["date"] != "2026-10-29" {
			t.Fatalf("%s: %v", test.name, run)
		}
		if test.name == "missing repo" && !strings.Contains(run["error"].(string), "no report for: b") {
			t.Fatalf("missing repo error = %v", run["error"])
		}
	}
}

// TestCollectGitHubZipIntoRealOps checks what the real Ops service concludes
// from the fixture: a skip never refreshes the set's last success, a repo's
// failure stays its own and keeps its earlier snapshot, and the CRC check is
// a verification, not a restore test.
func TestCollectGitHubZipIntoRealOps(t *testing.T) {
	opsURL := startOpsService(t)
	now := time.Now().UTC()
	dir := t.TempDir()
	githubFixture(t, filepath.Join(dir, "github"), now)
	configPath := filepath.Join(dir, "config.json")
	writeJSONFile(t, configPath, map[string]any{
		"cache_file": filepath.Join(dir, "cache.json"),
		"hosts":      map[string]any{"github": map[string]any{"display_name": "GitHub"}},
		"sources":    []any{map[string]any{"type": "github-zip", "state_dir": filepath.Join(dir, "github"), "job_id": "github-archives", "host_id": "github"}},
	})
	t.Setenv("DEN_OPS_SPOOL", filepath.Join(dir, "spool"))
	t.Setenv("DEN_OPS_URL", opsURL)
	cli(t, 0, "ops", "collect", "--config", configPath)

	job := func(id string) map[string]any {
		return getJSON(t, opsURL+"/v1/ops/jobs/"+id)["job"].(map[string]any)
	}
	set := job("github-archives")
	if attempt := set["last_attempt"].(map[string]any); attempt["run_id"] != "R3" || attempt["state"] != "skipped" {
		t.Fatalf("set last attempt = %v", attempt)
	}
	if success := set["last_success"].(map[string]any); success["run_id"] != "R1" {
		t.Fatalf("a skip refreshed the set's last success: %v", success)
	}
	alpha := job("github-owner-alpha")
	if alpha["status"] != "failed" || alpha["last_success"].(map[string]any)["run_id"] != "R1" {
		t.Fatalf("alpha = %v / %v", alpha["status"], alpha["last_success"])
	}
	beta := job("github-owner-beta")
	if beta["status"] != "warning" || beta["last_restore_test"] != nil {
		t.Fatalf("beta = %v, restore test %v", beta["status"], beta["last_restore_test"])
	}
	if check := beta["last_verification"].(map[string]any); check["check_type"] != "zip-crc-check" {
		t.Fatalf("beta verification = %v", check)
	}
	var detail struct {
		Runs []json.RawMessage `json:"runs"`
	}
	raw, _ := json.Marshal(getJSON(t, opsURL+"/v1/ops/jobs/github-owner-beta"))
	if err := json.Unmarshal(raw, &detail); err != nil || len(detail.Runs) != 2 {
		t.Fatalf("beta runs = %d (%v)", len(detail.Runs), err)
	}
}
