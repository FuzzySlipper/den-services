package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseOpsRunArgsDefaultsBorgWarningCode(t *testing.T) {
	options, err := parseOpsRunArgs([]string{"--job", "den-agents-git", "--engine", "borg", "--", "borgmatic", "create"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(options.warningCodes, []int{1}) || !reflect.DeepEqual(options.command, []string{"borgmatic", "create"}) {
		t.Fatalf("options = %+v", options)
	}
	options, err = parseOpsRunArgs([]string{"--job", "j", "--warning-exit-codes", "3,4", "--", "true"})
	if err != nil || !reflect.DeepEqual(options.warningCodes, []int{3, 4}) {
		t.Fatalf("explicit codes = %v, %v", options.warningCodes, err)
	}
	for _, bad := range [][]string{
		{"--engine", "borg", "--", "true"},
		{"--job", "j"},
		{"--job", "j", "--"},
		{"--job", "j", "--warning-exit-codes", "x", "--", "true"},
	} {
		if _, err := parseOpsRunArgs(bad); err == nil {
			t.Fatalf("parse(%v) succeeded", bad)
		}
	}
}

func TestOpsSpoolReplayKeepsOrderAndRejectsInvalid(t *testing.T) {
	var received []string
	failNext := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		switch {
		case strings.Contains(body.String(), "invalid"):
			w.WriteHeader(http.StatusBadRequest)
		case failNext:
			w.WriteHeader(http.StatusBadGateway)
		default:
			received = append(received, body.String())
			_, _ = w.Write([]byte(`{"outcome":"created"}`))
		}
	}))
	defer server.Close()
	client, err := NewOpsClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	spool := &OpsSpool{dir: t.TempDir()}
	base := time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC)
	for index, body := range []string{`{"n":1}`, `{"n":"invalid"}`, `{"n":3}`} {
		if _, err := spool.Add("PUT", "/v1/ops/jobs/j/runs/r", []byte(body), base.Add(time.Duration(index)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	failNext = true
	if result := spool.Replay(context.Background(), client); result.Delivered != 0 || result.Pending != 3 || result.Error == "" {
		t.Fatalf("outage replay = %+v", result)
	}
	failNext = false
	result := spool.Replay(context.Background(), client)
	if result.Delivered != 2 || len(result.Rejected) != 1 || result.Pending != 0 {
		t.Fatalf("replay = %+v", result)
	}
	if !reflect.DeepEqual(received, []string{`{"n":1}`, `{"n":3}`}) {
		t.Fatalf("received out of order: %v", received)
	}
	if again := spool.Replay(context.Background(), client); again.Delivered != 0 {
		t.Fatalf("second replay resent: %+v", again)
	}
}

// opsService runs the real Ops binary against a throwaway SQLite file.
func startOpsService(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "ops")
	if output, err := exec.Command("go", "build", "-o", binary, "den-services/ops/cmd/ops").CombinedOutput(); err != nil {
		t.Fatalf("build ops: %v\n%s", err, output)
	}
	address := freeAddress(t)
	config := fmt.Sprintf("bind_addr: %q\ndatabase_path: %q\nhttp:\n  read_header_timeout: \"5s\"\ndefaults:\n  heartbeat_stale_after: \"15m\"\n  run_list_limit: 50\n",
		address, filepath.Join(dir, "data", "ops.sqlite3"))
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	service := exec.Command(binary)
	service.Env = append(os.Environ(), "OPS_CONFIG_PATH="+configPath)
	if err := service.Start(); err != nil {
		t.Fatalf("start ops: %v", err)
	}
	t.Cleanup(func() { _ = service.Process.Kill(); _ = service.Wait() })
	baseURL := "http://" + address
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if response, err := http.Get(baseURL + "/health"); err == nil {
			response.Body.Close()
			return baseURL
		}
	}
	t.Fatal("ops did not become healthy")
	return ""
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func cli(t *testing.T, wantExit int, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := runCLI(args, &stdout, &stderr); code != wantExit {
		t.Fatalf("den-tool %v exit = %d, want %d\nstdout: %s\nstderr: %s", args, code, wantExit, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var value map[string]any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestOpsRunReportsRealCommandThroughOutageAndReplay(t *testing.T) {
	opsURL := startOpsService(t)
	spoolDir := t.TempDir()
	t.Setenv("DEN_OPS_SPOOL", spoolDir)
	t.Setenv("DEN_OPS_URL", opsURL)
	jobFile := filepath.Join(t.TempDir(), "job.json")
	if err := os.WriteFile(jobFile, []byte(`{"host_id":"den-agents","engine":"borg","kind":"backup","target":"den-nas:/tmp/test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cli(t, 0, "ops", "register-job", "e2e-borg", jobFile)

	// Ops unreachable: the command still runs, keeps its exit status, and the
	// terminal report waits in the spool.
	t.Setenv("DEN_OPS_URL", "http://"+freeAddress(t))
	cli(t, 1, "ops", "run", "--job", "e2e-borg", "--run-id", "offline-1", "--engine", "borg", "--heartbeat", "50ms",
		"--", "sh", "-c", "sleep 0.2; echo archived; echo 'file changed while we backed it up' >&2; exit 1")
	spooled, _ := filepath.Glob(filepath.Join(spoolDir, "*.json"))
	if len(spooled) != 1 {
		t.Fatalf("spool = %v", spooled)
	}
	duplicate, err := os.ReadFile(spooled[0])
	if err != nil {
		t.Fatal(err)
	}

	// Ops back: replay delivers it once; a second copy of the same report is a duplicate.
	t.Setenv("DEN_OPS_URL", opsURL)
	if out := cli(t, 0, "ops", "replay"); !strings.HasPrefix(out, "delivered 1,") {
		t.Fatalf("replay = %q", out)
	}
	if err := os.WriteFile(filepath.Join(spoolDir, "99999999999999999999-copy.json"), duplicate, 0o600); err != nil {
		t.Fatal(err)
	}
	cli(t, 0, "ops", "replay")
	run := getJSON(t, opsURL+"/v1/ops/jobs/e2e-borg/runs/offline-1")["run"].(map[string]any)
	if run["state"] != "warning" || run["exit_code"].(float64) != 1 || run["report_count"].(float64) != 1 {
		t.Fatalf("offline run = %v", run)
	}
	if !strings.Contains(fmt.Sprint(run["warnings"]), "file changed while we backed it up") {
		t.Fatalf("stderr tail missing from warnings: %v", run["warnings"])
	}

	// Online run with heartbeats, then a late heartbeat that must not reopen it.
	cli(t, 0, "ops", "run", "--job", "e2e-borg", "--run-id", "online-2", "--engine", "borg", "--heartbeat", "50ms",
		"--", "sh", "-c", "sleep 0.3")
	run = getJSON(t, opsURL+"/v1/ops/jobs/e2e-borg/runs/online-2")["run"].(map[string]any)
	if run["state"] != "success" || run["report_count"].(float64) < 3 {
		t.Fatalf("online run = %v", run)
	}
	late := `{"job_id":"e2e-borg","run_id":"online-2","state":"running","started_at":"2026-01-01T00:00:00Z","updated_at":"2099-01-01T00:00:00Z"}`
	lateFile := filepath.Join(t.TempDir(), "late.json")
	if err := os.WriteFile(lateFile, []byte(late), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := cli(t, 0, "ops", "report", lateFile); !strings.Contains(out, `"outcome":"ignored_terminal"`) {
		t.Fatalf("late heartbeat = %s", out)
	}

	// A clean backup followed by a failed restore test still needs attention.
	restore := `{"kind":"restore_test","state":"failed","at":"2026-10-29T08:00:00Z","archive":"e2e-archive","scope":"fixture"}`
	restoreFile := filepath.Join(t.TempDir(), "restore.json")
	if err := os.WriteFile(restoreFile, []byte(restore), 0o600); err != nil {
		t.Fatal(err)
	}
	cli(t, 0, "ops", "verification", "e2e-borg", "restore-1", restoreFile)
	errorsOut := cli(t, 0, "ops", "errors")
	if strings.Contains(errorsOut, "no job needs attention") || !strings.Contains(errorsOut, "restore-1") || !strings.Contains(errorsOut, "restore_test failed") {
		t.Fatalf("errors view hid the failed restore test:\n%s", errorsOut)
	}

	// A hard failure keeps the earlier success as the restore point.
	cli(t, 2, "ops", "run", "--job", "e2e-borg", "--run-id", "failed-3", "--engine", "borg", "--heartbeat", "0",
		"--", "sh", "-c", "echo 'repository locked' >&2; exit 2")
	job := getJSON(t, opsURL+"/v1/ops/jobs/e2e-borg")["job"].(map[string]any)
	if job["status"] != "failed" || job["last_success"].(map[string]any)["run_id"] != "online-2" {
		t.Fatalf("job after failure = %v %v", job["status"], job["last_success"])
	}
	runs := getJSON(t, opsURL+"/v1/ops/jobs/e2e-borg/runs")["runs"].([]any)
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3 distinct runs", len(runs))
	}

	// The CLI's --json output is the API body itself.
	var fromCLI map[string]any
	if err := json.Unmarshal([]byte(cli(t, 0, "ops", "status", "--json")), &fromCLI); err != nil {
		t.Fatal(err)
	}
	fromAPI := getJSON(t, opsURL+"/v1/ops/summary")
	if !reflect.DeepEqual(withoutClockFields(fromCLI), withoutClockFields(fromAPI)) {
		t.Fatalf("CLI and API summaries differ:\nCLI %v\nAPI %v", fromCLI, fromAPI)
	}
	if human := cli(t, 0, "ops", "status"); !strings.Contains(human, "failed") || !strings.Contains(human, "e2e-borg") {
		t.Fatalf("human status = %q", human)
	}
}

// withoutClockFields drops values that change between two reads a moment apart.
func withoutClockFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, item := range typed {
			if key == "generated_at" || key == "evaluated_at" || key == "heartbeat_age_seconds" {
				continue
			}
			result[key] = withoutClockFields(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, withoutClockFields(item))
		}
		return result
	}
	return value
}
