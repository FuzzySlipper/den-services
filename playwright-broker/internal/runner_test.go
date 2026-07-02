package broker

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

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

func TestRunChoosesCleanPortWhenPreferredPortHostsWrongApp(t *testing.T) {
	wrongServer, wrongURL, wrongPort := startHTTPServer(t, "wrong-app", "")
	defer wrongServer.Close()
	repoRoot := t.TempDir()
	outputPath := filepath.Join(t.TempDir(), "base-url.txt")
	writeManifest(t, repoRoot, map[string]any{
		"project": "alpha",
		"serve": map[string]any{
			"command":       helperCommand("server"),
			"preferredPort": wrongPort,
			"portRange":     map[string]any{"start": wrongPort + 1, "end": wrongPort + 20},
			"healthUrl":     "/",
			"readyText":     "alpha-ready",
		},
		"tests": map[string]any{
			"command":        helperCommand("playwright"),
			"artifactPolicy": "live-ui",
			"env":            map[string]string{"DEN_PLAYWRIGHT_TEST_OUTPUT": outputPath},
		},
	})
	result, err := NewRunner(testConfig(t)).Run(t.Context(), RunOptions{Project: "alpha", RepoRoot: repoRoot, Grep: "@live"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Evidence.Server.Port == wrongPort {
		t.Fatalf("broker reused wrong-app port %d", wrongPort)
	}
	if !result.Evidence.HumanInspectionRequired {
		t.Fatal("live-ui artifact policy did not require human inspection")
	}
	assertBaseURLFile(t, outputPath, result.Evidence.Server.BaseURL)
	assertEvidenceFile(t, result.Evidence.Artifacts.IndexPath, "alpha")
	assertHTTPBody(t, wrongURL, "wrong-app")
}

func TestRunReusesExplicitMatchingServerWithoutOwningIt(t *testing.T) {
	server, serverURL, port := startHTTPServer(t, "beta-ready", "beta")
	defer server.Close()
	repoRoot := t.TempDir()
	outputPath := filepath.Join(t.TempDir(), "base-url.txt")
	writeManifest(t, repoRoot, map[string]any{
		"project": "beta",
		"serve": map[string]any{
			"command":        helperCommand("server"),
			"preferredPort":  port,
			"healthUrl":      "/",
			"readyText":      "beta-ready",
			"identityHeader": "X-Den-Project",
			"reusePolicy":    "explicit",
		},
		"tests": map[string]any{
			"command": helperCommand("playwright"),
			"env":     map[string]string{"DEN_PLAYWRIGHT_TEST_OUTPUT": outputPath},
		},
	})
	result, err := NewRunner(testConfig(t)).Run(t.Context(), RunOptions{Project: "beta", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Evidence.Server.Reused || result.Evidence.Server.ReuseSource != "explicit" {
		t.Fatalf("server reuse = %v/%q, want explicit", result.Evidence.Server.Reused, result.Evidence.Server.ReuseSource)
	}
	if result.Evidence.Server.OwnedPID != 0 {
		t.Fatalf("OwnedPID = %d, want 0", result.Evidence.Server.OwnedPID)
	}
	assertBaseURLFile(t, outputPath, result.Evidence.Server.BaseURL)
	assertHTTPBody(t, serverURL, "beta-ready")
}

func TestTwoProjectManifestsRunThroughSameBrokerPath(t *testing.T) {
	cfg := testConfig(t)
	for _, project := range []string{"gamma", "delta"} {
		repoRoot := t.TempDir()
		outputPath := filepath.Join(t.TempDir(), project+"-base-url.txt")
		start := freePort(t) + 1
		writeManifest(t, repoRoot, map[string]any{
			"project": project,
			"serve": map[string]any{
				"command":   helperCommand("server"),
				"portRange": map[string]any{"start": start, "end": start + 40},
				"healthUrl": "/",
				"readyText": project + "-ready",
			},
			"tests": map[string]any{
				"command": helperCommand("playwright"),
				"env":     map[string]string{"DEN_PLAYWRIGHT_TEST_OUTPUT": outputPath},
			},
		})
		result, err := NewRunner(cfg).Run(t.Context(), RunOptions{Project: project, RepoRoot: repoRoot, DenProjectID: "den-services", DenTaskID: 3967})
		if err != nil {
			t.Fatalf("Run(%s) error = %v", project, err)
		}
		if result.Evidence.Den == nil || result.Evidence.Den.TaskID != 3967 {
			t.Fatalf("Den evidence missing for %s", project)
		}
		assertBaseURLFile(t, outputPath, result.Evidence.Server.BaseURL)
		assertEvidenceFile(t, result.Evidence.Artifacts.IndexPath, project)
	}
}

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("DEN_PLAYWRIGHT_TEST_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "server":
		port := os.Getenv("PORT")
		project := os.Getenv("DEN_PLAYWRIGHT_TEST_PROJECT")
		if project == "" {
			project = strings.TrimSuffix(os.Getenv("DEN_PLAYWRIGHT_TEST_READY"), "-ready")
		}
		ready := os.Getenv("DEN_PLAYWRIGHT_TEST_READY")
		if ready == "" {
			ready = project + "-ready"
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Den-Project", project)
			_, _ = fmt.Fprint(w, ready)
		})
		if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "playwright":
		outputPath := os.Getenv("DEN_PLAYWRIGHT_TEST_OUTPUT")
		if outputPath != "" {
			if err := os.WriteFile(outputPath, []byte(os.Getenv("BASE_URL")), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		artifactRoot := os.Getenv("PLAYWRIGHT_BROKER_ARTIFACT_ROOT")
		if artifactRoot != "" {
			if err := os.WriteFile(filepath.Join(artifactRoot, "trace.zip"), []byte("fake trace"), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		os.Exit(1)
	}
	os.Exit(0)
}

func testConfig(t *testing.T) *Config {
	t.Helper()
	start := freePort(t) + 1
	cfg := &Config{
		StateDir:     filepath.Join(t.TempDir(), "state"),
		ArtifactRoot: filepath.Join(t.TempDir(), "runs"),
		Host:         "127.0.0.1",
		PortRange:    PortRange{Start: start, End: start + 50},
		Timeouts: TimeoutConfig{
			LockTimeout:     2 * time.Second,
			StartupTimeout:  5 * time.Second,
			HealthTimeout:   time.Second,
			HealthInterval:  50 * time.Millisecond,
			ShutdownTimeout: time.Second,
			RunTimeout:      5 * time.Second,
		},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	return cfg
}

func writeManifest(t *testing.T, repoRoot string, manifest map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, DefaultManifestName), data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func helperCommand(mode string) string {
	ready := "{project}-ready"
	return strings.Join([]string{
		"DEN_PLAYWRIGHT_TEST_HELPER=" + shellQuote(mode),
		"DEN_PLAYWRIGHT_TEST_PROJECT=" + shellQuote("{project}"),
		"DEN_PLAYWRIGHT_TEST_READY=" + shellQuote(ready),
		shellQuote(os.Args[0]),
		"-test.run=TestHelperProcess",
		"--",
		mode,
	}, " ")
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
	return server, "http://" + listener.Addr().String(), port
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
	if string(data) != want {
		t.Fatalf("body = %q, want %q", string(data), want)
	}
}

func assertBaseURLFile(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("BASE_URL = %q, want %q", string(data), want)
	}
}

func assertEvidenceFile(t *testing.T, path string, project string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var evidence Evidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if evidence.Project != project || evidence.Status != "passed" {
		t.Fatalf("evidence project/status = %s/%s", evidence.Project, evidence.Status)
	}
	if len(evidence.Artifacts.Files) == 0 {
		t.Fatal("evidence did not include artifact files")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	port, err := strconv.Atoi(strings.TrimPrefix(listener.Addr().String(), "127.0.0.1:"))
	if err != nil {
		t.Fatalf("parsing port: %v", err)
	}
	return port
}
