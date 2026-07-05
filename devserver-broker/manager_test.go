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

func TestUpFallsBackWhenPreferredPortHasWrongIdentity(t *testing.T) {
	wrongServer, wrongURL, wrongPort := startHTTPServer(t, "wrong-ready", "wrong")
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
		t.Fatalf("reused wrong identity port %d", wrongPort)
	}
	assertHTTPBody(t, wrongURL, "wrong-ready")
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
