package devserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadServeManifestDoesNotRequirePlaywrightTestsBlock(t *testing.T) {
	cfg := testConfig(t)
	repoRoot := t.TempDir()
	writeManifestFile(t, filepath.Join(repoRoot, ".den-playwright.json"), map[string]any{
		"project": "alpha",
		"serve": map[string]any{
			"command":       helperCommand("server"),
			"preferredPort": 0,
			"healthUrl":     "/health",
			"readyText":     "alpha-ready",
		},
	})
	path, err := FindManifest(repoRoot, "")
	if err != nil {
		t.Fatalf("FindManifest() error = %v", err)
	}
	manifest, err := LoadServeManifest(path, repoRoot, cfg)
	if err != nil {
		t.Fatalf("LoadServeManifest() error = %v", err)
	}
	if manifest.Project != "alpha" {
		t.Fatalf("Project = %q, want alpha", manifest.Project)
	}
	if manifest.BindHost != DefaultBindHost {
		t.Fatalf("BindHost = %q, want %q", manifest.BindHost, DefaultBindHost)
	}
}

func TestUpBindsLanFacingProbesLoopbackAndReusesBrokerOwnedSession(t *testing.T) {
	cfg := testConfig(t)
	manager := newTestManager(t, cfg)
	repoRoot := t.TempDir()
	writeServeManifest(t, repoRoot, "alpha", map[string]any{
		"command":   helperCommand("server"),
		"healthUrl": "/health",
		"readyText": "alpha-ready",
	})
	result, err := manager.Up(t.Context(), UpOptions{Project: "alpha", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	defer stopSession(t, manager, result.Session)
	if !result.Started || result.Reused {
		t.Fatalf("Started/Reused = %v/%v, want started only", result.Started, result.Reused)
	}
	session := result.Session
	if session.BindHost != DefaultBindHost {
		t.Fatalf("BindHost = %q, want %q", session.BindHost, DefaultBindHost)
	}
	if session.ProbeHost != DefaultProbeHost {
		t.Fatalf("ProbeHost = %q, want %q", session.ProbeHost, DefaultProbeHost)
	}
	if !strings.HasPrefix(session.HealthURL, "http://127.0.0.1:") {
		t.Fatalf("HealthURL = %q, want loopback probe", session.HealthURL)
	}
	if !strings.HasPrefix(session.LANURL, "http://203.0.113.10:") {
		t.Fatalf("LANURL = %q, want configured public host", session.LANURL)
	}
	assertHTTPBody(t, session.LocalURL+"health", "alpha-ready")
	second, err := manager.Up(t.Context(), UpOptions{Project: "alpha", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("second Up() error = %v", err)
	}
	if !second.Reused || second.Session.Port != session.Port || second.Session.PID != session.PID {
		t.Fatalf("second session reuse = %v port/pid %d/%d, want %d/%d", second.Reused, second.Session.Port, second.Session.PID, session.Port, session.PID)
	}
}

func TestNodeNpmHostStaysBrokerOwnedAfterLauncherExits(t *testing.T) {
	cfg := testConfig(t)
	manager := newTestManager(t, cfg)
	repoRoot := t.TempDir()
	writeNpmDetachedHost(t, repoRoot)
	writeServeManifest(t, repoRoot, "node-host", map[string]any{
		"command":   "npm run dev -- --host {bind_host} --port {port} &",
		"healthUrl": "/health",
		"readyText": "node-host-ready",
	})

	result, err := manager.Up(t.Context(), UpOptions{Project: "node-host", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	defer func() {
		_ = StopProcessGroup(result.Session.PID, time.Second)
	}()
	if !waitForCondition(time.Second, func() bool { return !processAlive(result.Session.PID) }) {
		t.Fatalf("npm launcher pid %d did not exit", result.Session.PID)
	}
	if !processGroupAlive(result.Session.PID) {
		t.Fatalf("process group %d stopped with the npm launcher", result.Session.PID)
	}

	status, err := manager.Status(t.Context(), StatusOptions{Project: "node-host", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Status != "running" || status.PID != result.Session.PID || !status.Health.Matched {
		t.Fatalf("Status() = status=%q pid=%d health=%+v, want running current broker group", status.Status, status.PID, status.Health)
	}
	reused, err := manager.Up(t.Context(), UpOptions{Project: "node-host", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("second Up() error = %v", err)
	}
	if !reused.Reused || reused.Session.PID != result.Session.PID {
		t.Fatalf("second Up() = reused=%v pid=%d, want broker-owned group reuse", reused.Reused, reused.Session.PID)
	}

	stopped, err := manager.Stop(t.Context(), StopOptions{Project: "node-host", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !stopped.Stopped || stopped.Session.Status != "stopped" {
		t.Fatalf("Stop() = stopped=%v status=%q, want stopped", stopped.Stopped, stopped.Session.Status)
	}
	if !waitForCondition(time.Second, func() bool { return !processGroupAlive(result.Session.PID) }) {
		t.Fatalf("broker-owned process group %d remained alive after Stop()", result.Session.PID)
	}
}

func TestUpWaitsForDelayedNativeBrowserHostAndAcceptsContractHeader(t *testing.T) {
	cfg := testConfig(t)
	cfg.Timeouts.HealthInterval = 25 * time.Millisecond
	manager := newTestManager(t, cfg)
	repoRoot := t.TempDir()
	const startupDelay = 150 * time.Millisecond
	writeServeManifest(t, repoRoot, "browser-host", map[string]any{
		"command":             "DEN_SERVE_TEST_START_DELAY=" + shellQuote(startupDelay.String()) + " " + helperCommand("delayed-native-browser-host"),
		"healthUrl":           "/health",
		"readyText":           `"project": "browser-host"`,
		"identityHeader":      "X-ASHA-Browser-Host",
		"identityHeaderValue": "browser-host.v0",
	})

	startedAt := time.Now()
	result, err := manager.Up(t.Context(), UpOptions{Project: "browser-host", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	defer stopSession(t, manager, result.Session)
	if elapsed := time.Since(startedAt); elapsed < startupDelay {
		t.Fatalf("Up() returned after %s, want at least delayed readiness %s", elapsed, startupDelay)
	}
	if !result.Started || !result.Session.Health.Matched || !result.Session.Health.HeaderMatched {
		t.Fatalf("Up() = started=%v health=%+v, want running native browser host", result.Started, result.Session.Health)
	}
	status, err := manager.Status(t.Context(), StatusOptions{Project: "browser-host", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Status != "running" || !status.Health.Matched {
		t.Fatalf("Status() = status=%q health=%+v, want running native browser host", status.Status, status.Health)
	}
}

func TestUpFallsBackWhenPreferredPortHasExpectedBodyButWrongHeaderValue(t *testing.T) {
	wrongServer, wrongURL, wrongPort := startHTTPServer(t, "beta-ready", "wrong")
	defer wrongServer.Close()
	cfg := testConfig(t)
	cfg.PortRange = PortRange{Start: wrongPort + 1, End: wrongPort + 40}
	manager := newTestManager(t, cfg)
	repoRoot := t.TempDir()
	writeServeManifest(t, repoRoot, "beta", map[string]any{
		"command":        helperCommand("server"),
		"preferredPort":  wrongPort,
		"portRange":      map[string]any{"start": wrongPort + 1, "end": wrongPort + 40},
		"healthUrl":      "/health",
		"readyText":      "beta-ready",
		"identityHeader": "X-Den-Project",
	})
	result, err := manager.Up(t.Context(), UpOptions{Project: "beta", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	defer stopSession(t, manager, result.Session)
	if result.Session.Port == wrongPort {
		t.Fatalf("reused port %d with an incorrect identity header value", wrongPort)
	}
	assertHTTPBody(t, wrongURL, "beta-ready")
}

func TestStopLeavesExplicitUnownedReuseRunning(t *testing.T) {
	server, url, port := startHTTPServer(t, "gamma-ready", "gamma")
	defer server.Close()
	cfg := testConfig(t)
	manager := newTestManager(t, cfg)
	repoRoot := t.TempDir()
	writeServeManifest(t, repoRoot, "gamma", map[string]any{
		"command":        helperCommand("server"),
		"preferredPort":  port,
		"healthUrl":      "/",
		"readyText":      "gamma-ready",
		"identityHeader": "X-Den-Project",
		"reusePolicy":    "explicit",
	})
	result, err := manager.Up(t.Context(), UpOptions{Project: "gamma", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if !result.Reused || result.Session.Ownership != "unowned" {
		t.Fatalf("Reused/Ownership = %v/%q, want explicit unowned reuse", result.Reused, result.Session.Ownership)
	}
	stopped, err := manager.Stop(t.Context(), StopOptions{Project: "gamma", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if stopped.Stopped {
		t.Fatal("Stop() stopped an unowned explicit reuse")
	}
	assertHTTPBody(t, url, "gamma-ready")
}

func TestSessionKeyIncludesRepoRoot(t *testing.T) {
	first := SessionKey("delta", "/tmp/one")
	second := SessionKey("delta", "/tmp/two")
	if first == second {
		t.Fatalf("SessionKey() collision for different repo roots: %q", first)
	}
	if first != SessionKey("delta", "/tmp/one/../one") {
		t.Fatal("SessionKey() should normalize equivalent repo paths")
	}
}

func TestLeaseRegistryRecoversStaleLock(t *testing.T) {
	registry := NewLeaseRegistry(t.TempDir())
	if err := os.MkdirAll(registry.dir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(registry.lockPath, []byte("pid=999999999 acquired_at=2026-07-02T00:00:00Z\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := registry.Lock(t.Context(), time.Second); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if registry.lockFile == nil {
		t.Fatal("Lock() did not acquire replacement lock file")
	}
	if err := registry.Unlock(); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
}

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("DEN_SERVE_TEST_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "server":
		host := os.Getenv("HOST")
		if host == "" {
			host = DefaultProbeHost
		}
		port := os.Getenv("PORT")
		project := os.Getenv("DEN_SERVE_TEST_PROJECT")
		ready := os.Getenv("DEN_SERVE_TEST_READY")
		if ready == "" {
			ready = project + "-ready"
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Den-Project", project)
			_, _ = fmt.Fprint(w, ready)
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Den-Project", project)
			_, _ = fmt.Fprint(w, ready)
		})
		if err := http.ListenAndServe(net.JoinHostPort(host, port), mux); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "delayed-native-browser-host":
		delay, err := time.ParseDuration(os.Getenv("DEN_SERVE_TEST_START_DELAY"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		time.Sleep(delay)
		host := os.Getenv("HOST")
		if host == "" {
			host = DefaultProbeHost
		}
		port := os.Getenv("PORT")
		project := os.Getenv("DEN_SERVE_TEST_PROJECT")
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-ASHA-Browser-Host", "browser-host.v0")
			_, _ = fmt.Fprintf(w, `{ "ok": true, "project": %q }`, project)
		})
		if err := http.ListenAndServe(net.JoinHostPort(host, port), mux); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		os.Exit(1)
	}
	os.Exit(0)
}

func testConfig(t *testing.T) ManagerConfig {
	t.Helper()
	start := freePort(t) + 1
	cfg := ManagerConfig{
		StateDir:    filepath.Join(t.TempDir(), "state"),
		SessionRoot: filepath.Join(t.TempDir(), "sessions"),
		BindHost:    DefaultBindHost,
		ProbeHost:   DefaultProbeHost,
		PublicHost:  "203.0.113.10",
		PortRange:   PortRange{Start: start, End: start + 80},
		Timeouts: TimeoutConfig{
			LockTimeout:     2 * time.Second,
			StartupTimeout:  5 * time.Second,
			HealthTimeout:   time.Second,
			HealthInterval:  50 * time.Millisecond,
			ShutdownTimeout: 100 * time.Millisecond,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	return cfg
}

func newTestManager(t *testing.T, cfg ManagerConfig) *Manager {
	t.Helper()
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func writeServeManifest(t *testing.T, repoRoot string, project string, serve map[string]any) {
	t.Helper()
	writeManifestFile(t, filepath.Join(repoRoot, ".den-serve.json"), map[string]any{
		"project": project,
		"serve":   serve,
	})
}

func writeManifestFile(t *testing.T, path string, manifest map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func writeNpmDetachedHost(t *testing.T, repoRoot string) {
	t.Helper()
	packageJSON := `{"private":true,"scripts":{"dev":"node server.js"}}`
	serverJS := `
const http = require("node:http");
const args = process.argv.slice(2);
const valueAfter = (flag, fallback) => {
  const index = args.indexOf(flag);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
};
const host = valueAfter("--host", process.env.HOST || "127.0.0.1");
const port = Number(valueAfter("--port", process.env.PORT || "0"));
http.createServer((_, response) => {
  response.writeHead(200, {"content-type":"text/plain"});
  response.end("node-host-ready");
}).listen(port, host);
`
	if err := os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(packageJSON), 0o600); err != nil {
		t.Fatalf("writing package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "server.js"), []byte(serverJS), 0o600); err != nil {
		t.Fatalf("writing server.js: %v", err)
	}
}

func waitForCondition(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return condition()
}

func helperCommand(mode string) string {
	ready := "{project}-ready"
	return strings.Join([]string{
		"DEN_SERVE_TEST_HELPER=" + shellQuote(mode),
		"DEN_SERVE_TEST_PROJECT=" + shellQuote("{project}"),
		"DEN_SERVE_TEST_READY=" + shellQuote(ready),
		shellQuote(os.Args[0]),
		"-test.run=TestHelperProcess",
		"--",
		mode,
	}, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func stopSession(t *testing.T, manager *Manager, session SessionState) {
	t.Helper()
	if _, err := manager.Stop(t.Context(), StopOptions{Project: session.Project, RepoRoot: session.RepoRoot}); err != nil {
		t.Fatalf("Stop() cleanup error = %v", err)
	}
}

func startHTTPServer(t *testing.T, body string, project string) (*http.Server, string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if project != "" {
				w.Header().Set("X-Den-Project", project)
			}
			_, _ = fmt.Fprint(w, body)
		}),
	}
	go func() {
		_ = server.Serve(listener)
	}()
	return server, "http://" + listener.Addr().String() + "/", port
}

func assertHTTPBody(t *testing.T, url string, want string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("body = %q, want substring %q", string(data), want)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
