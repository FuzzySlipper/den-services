package docpublish

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestServerRoutesRequireAuthAndExposeHealth(t *testing.T) {
	server := newTestServer(t)

	healthRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want 200", healthRecorder.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/blog/publications/preview", strings.NewReader("{}")))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.Code)
	}
}

func TestPreviewPublishAndGetRoutes(t *testing.T) {
	server := newTestServer(t)
	req := PublicationRequest{
		Source:      testSource(),
		RequestedBy: "pi",
		Document:    testDocument(),
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	preview := httptest.NewRecorder()
	server.Handler.ServeHTTP(preview, authedRequest(http.MethodPost, "/v1/blog/publications/preview", body))
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200; body=%s", preview.Code, preview.Body.String())
	}
	assertContains(t, preview.Body.String(), `"status":"previewed"`)

	publish := httptest.NewRecorder()
	server.Handler.ServeHTTP(publish, authedRequest(http.MethodPost, "/v1/blog/publications", body))
	if publish.Code != http.StatusCreated {
		t.Fatalf("publish status = %d, want 201; body=%s", publish.Code, publish.Body.String())
	}
	var response PublicationResponse
	if err := json.Unmarshal(publish.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal(publish response) error = %v", err)
	}
	get := httptest.NewRecorder()
	server.Handler.ServeHTTP(get, authedRequest(http.MethodGet, "/v1/blog/publications/"+response.PublicationID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", get.Code, get.Body.String())
	}
	assertContains(t, get.Body.String(), `"status":"published"`)
}

func newTestServer(t *testing.T) *http.Server {
	t.Helper()
	repo, remote := initBlogRepo(t)
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/den-web/documents/example-doc" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"Canonical Title","slug":"example-doc","content":"Canonical body"}`))
	}))
	t.Cleanup(sourceServer.Close)
	cfg := &Config{
		BindAddr:     "127.0.0.1:0",
		ServiceToken: "test-token",
		Blog:         testBlogConfig(repo, remote, false),
		Records:      RecordsConfig{Path: t.TempDir()},
		Source:       SourceConfig{DocumentsBaseURL: sourceServer.URL, RequestTimeout: time.Second},
		Git:          GitConfig{CommandTimeout: 5 * time.Second},
		HTTP:         HTTPConfig{ReadHeaderTimeout: time.Second},
	}
	info, err := health.NewBuildInfo("doc-publish", "dev", "unknown", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(cfg, info)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server
}

func TestMissingPublicationRouteReturns404(t *testing.T) {
	server := newTestServer(t)

	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/v1/blog/publications/missing", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing publication status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
	assertContains(t, recorder.Body.String(), `"code":"not_found"`)
}

func authedRequest(method string, target string, body []byte) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	return request
}
