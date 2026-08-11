package broker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
	session := PlaytestSession{
		SessionID: "gone-driver",
		Status:    "running",
		Endpoint:  endpoint,
		DriverPID: 999999999,
		IndexPath: filepath.Join(t.TempDir(), "playtest-index.json"),
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
}

func playtestTestConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		StateDir: t.TempDir(),
		Timeouts: TimeoutConfig{LockTimeout: time.Second, ShutdownTimeout: 50 * time.Millisecond},
		Playtest: PlaytestConfig{CommandTimeout: 250 * time.Millisecond},
	}
}
