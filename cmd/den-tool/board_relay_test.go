package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBoardRelayClientSyncRequiresProjectAndUsesExplicitPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/projects/rusty-engine/board/github-sync" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer relay-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"project_id":"rusty-engine"}`))
	}))
	defer server.Close()
	client, err := NewBoardRelayClient(server.URL, "relay-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Sync(context.Background(), ""); err == nil {
		t.Fatal("Sync accepted no project")
	}
	if _, err := client.Sync(context.Background(), "rusty-engine"); err != nil {
		t.Fatal(err)
	}
}

func TestRunBoardGitHubVisibilityUsesRelayOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/board/github-visibility" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if !strings.Contains(readTestRequestBody(t, r), `"visibility":"private"`) {
			t.Fatalf("visibility body missing")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	t.Setenv("DEN_BOARD_RELAY_URL", server.URL)
	t.Setenv("DEN_BOARD_RELAY_SERVICE_TOKEN", "relay-token")
	var stdout, stderr bytes.Buffer
	if code := runBoardCommand([]string{"github-visibility", "--visibility", "private"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func readTestRequestBody(t *testing.T, r *http.Request) string {
	t.Helper()
	var body bytes.Buffer
	if _, err := body.ReadFrom(r.Body); err != nil {
		t.Fatal(err)
	}
	return body.String()
}
