package webedge

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestServerServesReleaseAndProxiesOnlyThroughGateway(t *testing.T) {
	var gatewayAuthorization string
	var gatewayPath string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayAuthorization = r.Header.Get("Authorization")
		gatewayPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[]}`))
	}))
	defer gateway.Close()

	root := releaseFixture(t)
	target, err := url.Parse(gateway.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(root, target)
	info, err := health.NewBuildInfo("web-edge", "test", "edge-commit", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewHTTPServer(cfg, info, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	edge := httptest.NewServer(server.Handler)
	defer edge.Close()

	response := get(t, edge.URL+"/")
	if response.StatusCode != http.StatusOK || !strings.Contains(readBody(t, response), "<title>Den</title>") {
		t.Fatalf("root response status = %d", response.StatusCode)
	}

	response = get(t, edge.URL+"/projects/den-web/tasks")
	if response.StatusCode != http.StatusOK || !strings.Contains(readBody(t, response), "<title>Den</title>") {
		t.Fatalf("SPA response status = %d", response.StatusCode)
	}

	request, err := http.NewRequest(http.MethodGet, edge.URL+"/api/v1/projects?include_archived=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer browser-supplied-token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d", response.StatusCode)
	}
	_ = response.Body.Close()
	if gatewayAuthorization != "Bearer edge-token" {
		t.Fatalf("Gateway Authorization = %q", gatewayAuthorization)
	}
	if gatewayPath != "/v1/projects?include_archived=true" {
		t.Fatalf("Gateway path = %q", gatewayPath)
	}

	response = get(t, edge.URL+"/health")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}
	var healthBody health.HealthResponse
	if err := json.NewDecoder(response.Body).Decode(&healthBody); err != nil {
		t.Fatalf("decoding health: %v", err)
	}
	_ = response.Body.Close()
	if healthBody.Commit != "edge-commit" {
		t.Fatalf("health commit = %q", healthBody.Commit)
	}

	response = get(t, edge.URL+"/api/legacy")
	if response.StatusCode != http.StatusGone {
		t.Fatalf("retired API status = %d", response.StatusCode)
	}
	_ = response.Body.Close()
}

func TestServerCachePolicy(t *testing.T) {
	root := releaseFixture(t)
	target, _ := url.Parse("http://127.0.0.1:1")
	cfg := testConfig(root, target)
	info, _ := health.NewBuildInfo("web-edge", "test", "edge-commit", time.Unix(1, 0))
	server, err := NewHTTPServer(cfg, info, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewServer(server.Handler)
	defer edge.Close()

	response := get(t, edge.URL+"/main.12345678.js")
	if got := response.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("hashed asset Cache-Control = %q", got)
	}
	_ = response.Body.Close()

	response = get(t, edge.URL+"/den-web-config.json")
	if got := response.Header.Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("runtime config Cache-Control = %q", got)
	}
	_ = response.Body.Close()
}

func releaseFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"index.html":          "<!doctype html><title>Den</title>",
		"main.12345678.js":    "console.log('den');",
		"den-web-config.json": `{"tasksSuccessorApiBase":"/api/v1"}`,
		"den-web-build.json":  `{"commit":"frontend-commit"}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testConfig(root string, target *url.URL) *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr:        "127.0.0.1:0",
			ReadHeaderTimeout: time.Second,
			IdleTimeout:       time.Minute,
		},
		Static: StaticConfig{
			Root:                 root,
			RuntimeConfigPath:    filepath.Join(root, "den-web-config.json"),
			BuildSentinelPath:    filepath.Join(root, "den-web-build.json"),
			ImmutableAssetMaxAge: 365 * 24 * time.Hour,
		},
		Gateway: GatewayConfig{
			BaseURL:               target,
			BearerTokenEnv:        "EDGE_TOKEN",
			BearerToken:           "edge-token",
			ResponseHeaderTimeout: time.Second,
		},
	}
}

func get(t *testing.T, target string) *http.Response {
	t.Helper()
	response, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
