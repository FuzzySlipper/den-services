package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlaytestCallPreservesUnknownFieldsAndAddsOperation(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true,"continued":true}`))
	}))
	defer server.Close()

	manager := NewPlaytestManager(playtestTestConfig(t))
	session := PlaytestSession{SessionID: "session-1", Status: "running", Endpoint: server.URL, DriverPID: 0}
	if err := manager.saveSession(session); err != nil {
		t.Fatalf("saveSession() error = %v", err)
	}
	result, err := manager.Call(t.Context(), session.SessionID, map[string]any{
		"kind":         "act",
		"future_field": map[string]any{"retained": true},
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v", result)
	}
	if received["kind"] != "act" {
		t.Fatalf("kind = %#v, want act", received["kind"])
	}
	future, ok := received["future_field"].(map[string]any)
	if !ok || future["retained"] != true {
		t.Fatalf("future_field = %#v", received["future_field"])
	}
}

func TestPlaytestCallFailsOpenWhenDriverIsUnavailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	endpoint := "http://" + listener.Addr().String()
	listener.Close()

	manager := NewPlaytestManager(playtestTestConfig(t))
	artifactRoot := t.TempDir()
	session := PlaytestSession{
		SchemaVersion: PlaytestSchemaVersion,
		SessionID:     "gone-driver",
		Project:       "fixture",
		Status:        "running",
		Endpoint:      endpoint,
		DriverPID:     999999999,
		ArtifactRoot:  artifactRoot,
		IndexPath:     filepath.Join(artifactRoot, "playtest-index.json"),
		StartedAt:     time.Now().UTC(),
	}
	if err := manager.saveSession(session); err != nil {
		t.Fatalf("saveSession() error = %v", err)
	}
	result, err := manager.Call(context.Background(), session.SessionID, map[string]any{"kind": "inspect"})
	if err != nil {
		t.Fatalf("Call() error = %v, want diagnostic result", err)
	}
	if result["ok"] != false || result["continued"] != false {
		t.Fatalf("result = %#v", result)
	}
	diagnostics, ok := result["discrepancies"].([]map[string]any)
	if !ok || len(diagnostics) != 1 || diagnostics[0]["code"] != "driver_call_error" {
		t.Fatalf("discrepancies = %#v", result["discrepancies"])
	}
	requests := readJSONLines(t, filepath.Join(artifactRoot, "requests.jsonl"))
	if len(requests) != 1 || requests[0]["source"] != "manager_fallback" {
		t.Fatalf("requests = %#v", requests)
	}
	request, _ := requests[0]["request"].(map[string]any)
	if request["kind"] != "inspect" {
		t.Fatalf("retained request = %#v", request)
	}
	index := readIndex(t, session.IndexPath)
	assertIndexDiagnostic(t, index, "driver_call_error")
	timeline, _ := index["timeline"].([]any)
	if len(timeline) != 1 || timeline[0].(map[string]any)["kind"] != "manager_call_failure" {
		t.Fatalf("timeline = %#v", timeline)
	}
}

func TestFinishHostCleanupPersistsManagerDiagnostics(t *testing.T) {
	artifactRoot := t.TempDir()
	blockedStateDir := filepath.Join(t.TempDir(), "state-file")
	if err := os.WriteFile(blockedStateDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg := playtestTestConfig(t)
	cfg.StateDir = blockedStateDir
	manager := NewPlaytestManager(cfg)
	session := PlaytestSession{
		SchemaVersion: PlaytestSchemaVersion,
		SessionID:     "cleanup-failure",
		Project:       "fixture",
		Status:        "running",
		DriverPID:     999999999,
		ServerPID:     999999998,
		ArtifactRoot:  artifactRoot,
		IndexPath:     filepath.Join(artifactRoot, "playtest-index.json"),
		StartedAt:     time.Now().UTC(),
	}
	manager.finishHostCleanup(&session, "finish", "infrastructure_error")

	index := readIndex(t, session.IndexPath)
	assertIndexDiagnostic(t, index, "lease_lock_error")
	assertIndexDiagnostic(t, index, "session_save_error")
	cleanup, _ := index["cleanup"].(map[string]any)
	if cleanup["dev_server_stopped"] != true {
		t.Fatalf("cleanup = %#v", cleanup)
	}
	if cleanup["driver_stopped"] != true || index["status"] != "infrastructure_error" {
		t.Fatalf("status/cleanup = %#v / %#v", index["status"], cleanup)
	}
	artifacts, _ := index["artifacts"].([]any)
	foundSidecar := false
	for _, artifact := range artifacts {
		foundSidecar = foundSidecar || artifact == "host-cleanup.jsonl"
	}
	if !foundSidecar {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	if lines := readJSONLines(t, filepath.Join(artifactRoot, "host-cleanup.jsonl")); len(lines) == 0 {
		t.Fatal("host cleanup sidecar is empty")
	}
}

func playtestTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		StateDir: t.TempDir(),
		Timeouts: TimeoutConfig{LockTimeout: time.Second, ShutdownTimeout: 50 * time.Millisecond},
		Playtest: PlaytestConfig{CommandTimeout: 250 * time.Millisecond},
	}
}

func readIndex(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var index map[string]any
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", path, err)
	}
	return index
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", path, err)
	}
	defer file.Close()
	lines := []map[string]any{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("Unmarshal line error = %v", err)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan(%s) error = %v", path, err)
	}
	return lines
}

func assertIndexDiagnostic(t *testing.T, index map[string]any, code string) {
	t.Helper()
	diagnostics, _ := index["discrepancies"].([]any)
	for _, value := range diagnostics {
		diagnostic, _ := value.(map[string]any)
		if diagnostic["code"] == code {
			return
		}
	}
	t.Fatalf("diagnostic %q not found in %#v", code, diagnostics)
}
