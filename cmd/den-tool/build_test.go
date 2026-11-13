package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestBuildHostRankingSkipsBusySharedMemoryAndStaleHosts(t *testing.T) {
	var state buildResourcesState
	err := json.Unmarshal([]byte(`{
		"hosts": [
			{"id":"disabled","status":"quiet","telemetry":{"cpu_percent":0}},
			{"id":"stale","status":"quiet","stale":true,"telemetry":{"cpu_percent":0}},
			{"id":"gpu-busy","status":"working","telemetry":{"cpu_percent":0,"gpus":[{"utilization_percent":50}]}},
			{"id":"queued","status":"queued","telemetry":{"cpu_percent":0,"gpus":[{"utilization_percent":0}]},
			 "apps":[{"name":"ComfyUI","online":true,"running":0,"queued":1}]},
			{"id":"reported","status":"working","telemetry":{"cpu_percent":0,"gpus":[{"utilization_percent":0}]}},
			{"id":"no-gpu-data","status":"quiet","telemetry":{"cpu_percent":0,"gpus":[{"utilization_percent":null}]}},
			{"id":"building","status":"working","telemetry":{"cpu_percent":0}},
			{"id":"quiet-strix","status":"quiet","telemetry":{"cpu_percent":2,"gpus":[{"utilization_percent":0}]}},
			{"id":"quiet-cpu","status":"quiet","telemetry":{"cpu_percent":8}},
			{"id":"working-cpu","status":"working","telemetry":{"cpu_percent":1}}
		],
		"work": [
			{"host":"reported","state":"running","source":"workbench"},
			{"host":"building","state":"running","source":"build"},
			{"host":"quiet-strix","state":"running","source":"build","stale":true}
		]
	}`), &state)
	if err != nil {
		t.Fatal(err)
	}
	limit := 10.0
	hosts := []buildHost{
		{ID: "disabled"},
		{ID: "stale", Enabled: true},
		{ID: "gpu-busy", Enabled: true, MaxGPUPercent: &limit},
		{ID: "queued", Enabled: true, MaxGPUPercent: &limit},
		{ID: "reported", Enabled: true, MaxGPUPercent: &limit},
		{ID: "no-gpu-data", Enabled: true, MaxGPUPercent: &limit},
		{ID: "building", Enabled: true},
		{ID: "working-cpu", Enabled: true},
		{ID: "quiet-cpu", Enabled: true},
		{ID: "quiet-strix", Enabled: true, MaxGPUPercent: &limit},
	}
	candidates, skipped := rankBuildHosts(hosts, state, "")
	var ids []string
	for _, candidate := range candidates {
		ids = append(ids, candidate.host.ID)
	}
	if want := []string{"quiet-strix", "quiet-cpu", "working-cpu"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ranked hosts %v, want %v (skipped %v)", ids, want, skipped)
	}
}

func TestBuildRunUsesPinnedCleanCheckoutAndKeepsCache(t *testing.T) {
	for _, name := range []string{"bash", "git", "flock", "df"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("requires Linux build tools: %v", err)
		}
	}
	base := t.TempDir()
	source := filepath.Join(base, "sample")
	root := filepath.Join(base, "worker's builds")
	remoteBin := filepath.Join(base, "bin")
	if err := os.MkdirAll(remoteBin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", remoteBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BUILD_TEST_PATH", os.Getenv("PATH"))
	t.Setenv("BUILD_TEST_LOCK", filepath.Join(root, ".lock"))
	t.Setenv("BUILD_TEST_UPDATE_LOG", filepath.Join(base, "rustup.log"))
	t.Setenv("RUSTUP_TOOLCHAIN", "1.99.1")
	writeBuildTestFile(t, filepath.Join(remoteBin, "rustup"), `#!/usr/bin/env bash
set -eu
if flock -n "$BUILD_TEST_LOCK" true; then
    echo unlocked >> "$BUILD_TEST_UPDATE_LOG"
fi
printf '%s\n' "$*" >> "$BUILD_TEST_UPDATE_LOG"
case "$*" in
    "update stable --no-self-update") exit 1 ;;
    "run stable rustc --version") echo "rustc fixture" ;;
    *) exit 2 ;;
esac
`, 0o755)
	helperPIDPath := filepath.Join(base, "compiler-helper.pid")
	t.Setenv("BUILD_TEST_HELPER_PID", helperPIDPath)
	t.Cleanup(func() {
		if contents, err := os.ReadFile(helperPIDPath); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(contents))); err == nil {
				if helper, err := os.FindProcess(pid); err == nil {
					_ = helper.Kill()
				}
			}
		}
	})
	writeBuildTestFile(t, filepath.Join(remoteBin, "ssh"), `#!/usr/bin/env bash
export PATH="$BUILD_TEST_PATH"
if [[ ${@: -2:1} == no-tools ]]; then export BUILD_TEST_TOOLCHAIN=no
else export BUILD_TEST_TOOLCHAIN=yes
fi
exec bash -c "${@: -1}"
`, 0o755)
	buildTestGit(t, base, "init", "-q", source)
	writeBuildTestFile(t, filepath.Join(source, "lane"), `#!/usr/bin/env bash
set -eu
test ! -e uncommitted
test ! -e scratch
test "$CARGO_BUILD_JOBS" = 4
test "$BUILD_TEST_ENV" = "has a 'quote'"
if [[ -f target/cache ]]; then
    echo warm
else
    # Simulate a compiler server that outlives the lane.
    sleep 30 </dev/null >/dev/null 2>&1 &
    echo "$!" > "$BUILD_TEST_HELPER_PID"
    echo cold
fi
mkdir -p target
touch target/cache scratch
cat version
exit "$(cat status)"
`, 0o755)
	writeBuildTestFile(t, filepath.Join(source, "version"), "original\n", 0o644)
	writeBuildTestFile(t, filepath.Join(source, "status"), "0\n", 0o644)
	buildTestGit(t, source, "add", ".")
	buildTestGit(t, source, "commit", "-qm", "first")
	first := buildTestGit(t, source, "rev-parse", "HEAD")
	writeBuildTestFile(t, filepath.Join(source, "version"), "next\n", 0o644)
	writeBuildTestFile(t, filepath.Join(source, "status"), "37\n", 0o644)
	buildTestGit(t, source, "commit", "-qam", "second")
	second := buildTestGit(t, source, "rev-parse", "HEAD")
	writeBuildTestFile(t, filepath.Join(source, "version"), "dirty source\n", 0o644)
	buildTestGit(t, source, "add", "version")
	writeBuildTestFile(t, filepath.Join(source, "uncommitted"), "local changes\n", 0o644)

	var mu sync.Mutex
	var work []resourceWorkReport
	var finished []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case resourcesPath("state"):
			_, _ = io.WriteString(w, `{"hosts":[
				{"id":"no-tools","status":"quiet","telemetry":{"cpu_percent":1}},
				{"id":"worker","status":"quiet","telemetry":{"cpu_percent":5}}
			]}`)
		case resourcesPath("work"):
			var report resourceWorkReport
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
				t.Error(err)
			}
			work = append(work, report)
			_, _ = io.WriteString(w, `{"id":"test-work"}`)
		case resourcesPath("work", "test-work"):
			var update struct{ State string }
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				t.Error(err)
			}
			finished = append(finished, update.State)
			_, _ = io.WriteString(w, `{"id":"test-work"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := buildConfig{
		ProbeTimeoutSeconds: 5,
		Hosts: []buildHost{
			{ID: "no-tools", Enabled: true, SSH: []string{"no-tools"}, BuildRoot: root, SourceRoot: base, Jobs: 4},
			{ID: "worker", Enabled: true, SSH: []string{"worker"}, BuildRoot: root, SourceRoot: base, Jobs: 4},
		},
		Toolchains: map[string]string{"fixture": `test "$BUILD_TEST_TOOLCHAIN" = yes`},
		Repositories: map[string]buildRepository{
			"sample": {Keep: []string{"target"}, Lanes: map[string]buildLane{
				"verify": {
					Command: []string{"./lane"}, Toolchains: []string{"fixture"}, MinFreeGiB: 1,
					Env: map[string]string{"BUILD_TEST_ENV": "has a 'quote'"},
				},
				"verify-stable": {
					Command: []string{"./lane"}, Toolchains: []string{"fixture"}, MinFreeGiB: 1,
					Env: map[string]string{"BUILD_TEST_ENV": "has a 'quote'", "RUSTUP_TOOLCHAIN": "stable"},
				},
			}},
		},
	}
	body, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(base, "config.json")
	writeBuildTestFile(t, configPath, string(body), 0o644)

	for _, run := range []struct {
		ref, lane, output string
		status            int
	}{
		{ref: first, lane: "gpu", status: 2},
		{ref: first, lane: "verify", output: "cold\noriginal\n", status: 0},
		{ref: second, lane: "verify-stable", output: "warm\nnext\n", status: 37},
	} {
		var stdout, stderr bytes.Buffer
		args := []string{"build", "run", "--config", configPath, "--url", server.URL, "--repo", "sample", "--lane", run.lane, "--ref", run.ref}
		if code := runCLI(args, &stdout, &stderr); code != run.status || stdout.String() != run.output {
			t.Fatalf("build %s %s: got %d %q, want %d %q. stderr: %s", run.lane, run.ref, code, stdout.String(), run.status, run.output, stderr.String())
		}
		if run.lane == "verify-stable" && (!strings.Contains(stderr.String(), "warning: could not check for a Rust stable update") ||
			!strings.Contains(stderr.String(), "rustc fixture")) {
			t.Fatalf("build did not report the failed update and installed compiler: %s", stderr.String())
		}
	}

	updateLog, err := os.ReadFile(filepath.Join(base, "rustup.log"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "update stable --no-self-update\nrun stable rustc --version\n"; string(updateLog) != want {
		t.Fatalf("rustup calls = %q, want %q only for the stable lane while the worker is locked", updateLog, want)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(work) != 2 || !reflect.DeepEqual(finished, []string{"completed", "failed"}) {
		t.Fatalf("work ledger got %v, checkout states %v", work, finished)
	}
	for index, ref := range []string{first, second} {
		report := work[index]
		if report.Host != "worker" || report.Source != buildWorkSource || report.State != "running" ||
			!strings.Contains(report.Note, ref) || report.ExternalID == "" {
			t.Errorf("wrong build report: %+v", report)
		}
	}
	if work[0].ExternalID == work[1].ExternalID {
		t.Fatal("separate runs must have separate ledger rows")
	}
}

func buildTestGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", directory, "-c", "user.name=Build test", "-c", "user.email=build@test.invalid"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeBuildTestFile(t *testing.T, path string, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}
