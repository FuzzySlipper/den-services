package webedge

import (
	"bufio"
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

func TestServerPreservesProxyBodyIdempotencyHeadersAndStatus(t *testing.T) {
	var receivedBody string
	var receivedIdempotencyKey string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read Gateway request body: %v", err)
			return
		}
		receivedBody = string(body)
		receivedIdempotencyKey = r.Header.Get("Idempotency-Key")
		if r.Method != http.MethodPost {
			t.Errorf("Gateway method = %s, want POST", r.Method)
		}
		w.Header().Set("Location", "/v1/review/finalizations/17")
		w.Header().Set("X-Request-ID", "gateway-request-17")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer gateway.Close()

	edge := newTestEdge(t, releaseFixture(t), gateway.URL)
	request, err := http.NewRequest(
		http.MethodPost,
		edge.URL+"/api/v1/review/finalizations",
		strings.NewReader(`{"review_round_id":17}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "finalization-17")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("proxy status = %d, want 202", response.StatusCode)
	}
	if got := response.Header.Get("Location"); got != "/v1/review/finalizations/17" {
		t.Fatalf("Location = %q", got)
	}
	if got := response.Header.Get("X-Request-ID"); got != "gateway-request-17" {
		t.Fatalf("X-Request-ID = %q", got)
	}
	if receivedBody != `{"review_round_id":17}` {
		t.Fatalf("Gateway body = %q", receivedBody)
	}
	if receivedIdempotencyKey != "finalization-17" {
		t.Fatalf("Gateway Idempotency-Key = %q", receivedIdempotencyKey)
	}
}

func TestServerStreamsSSEThroughTheEdge(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Stream-Owner", "timeline")
		_, _ = io.WriteString(w, "event: stream_open\ndata: {\"scope\":\"den-web\"}\n\n")
		w.(http.Flusher).Flush()
	}))
	defer gateway.Close()

	edge := newTestEdge(t, releaseFixture(t), gateway.URL)
	response := get(t, edge.URL+"/api/v1/timeline/projects/den-web/stream?limit=1")
	defer response.Body.Close()
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Header.Get("X-Stream-Owner"); got != "timeline" {
		t.Fatalf("X-Stream-Owner = %q", got)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "event: stream_open\n" {
		t.Fatalf("first streamed line = %q", line)
	}
}

func TestStaticHandlerRejectsMethodsAndTraversalAndSupportsHEAD(t *testing.T) {
	root := releaseFixture(t)
	handler, err := newStaticHandler(testConfig(root, &url.URL{}).Static)
	if err != nil {
		t.Fatal(err)
	}

	post := httptest.NewRequest(http.MethodPost, "http://edge/", nil)
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / status = %d, want 405", postRecorder.Code)
	}
	if got := postRecorder.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q", got)
	}

	traversal := httptest.NewRequest(http.MethodGet, "http://edge/", nil)
	traversal.URL.Path = "/../outside.txt"
	traversalRecorder := httptest.NewRecorder()
	handler.ServeHTTP(traversalRecorder, traversal)
	if traversalRecorder.Code != http.StatusNotFound {
		t.Fatalf("traversal status = %d, want 404", traversalRecorder.Code)
	}

	head := httptest.NewRequest(http.MethodHead, "http://edge/main.12345678.js", nil)
	headRecorder := httptest.NewRecorder()
	handler.ServeHTTP(headRecorder, head)
	if headRecorder.Code != http.StatusOK {
		t.Fatalf("HEAD asset status = %d", headRecorder.Code)
	}
	if headRecorder.Body.Len() != 0 {
		t.Fatalf("HEAD asset body length = %d, want 0", headRecorder.Body.Len())
	}
	if headRecorder.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD asset is missing Content-Length")
	}
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

func newTestEdge(t *testing.T, root string, gatewayURL string) *httptest.Server {
	t.Helper()
	target, err := url.Parse(gatewayURL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := health.NewBuildInfo("web-edge", "test", "edge-commit", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewHTTPServer(
		testConfig(root, target),
		info,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewServer(server.Handler)
	t.Cleanup(edge.Close)
	return edge
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
