package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"den-services/shared/health"
)

func TestGatewayProxiesRequestWithoutPayloadModification(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.RequestURI() != "/api/messages?id=42" {
			t.Fatalf("request uri = %s", r.URL.RequestURI())
		}
		if string(body) != `{"hello":"den"}` {
			t.Fatalf("body = %s", string(body))
		}
		if r.Header.Get("X-Test-Header") != "kept" {
			t.Fatalf("X-Test-Header = %q, want kept", r.Header.Get("X-Test-Header"))
		}
		w.Header().Set("X-Upstream", "legacy")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer upstream.Close()

	server := newTestGatewayServer(t, upstream.URL, "token")
	request := httptest.NewRequest(http.MethodPost, "/api/messages?id=42", strings.NewReader(`{"hello":"den"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Test-Header", "kept")
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if recorder.Header().Get("X-Upstream") != "legacy" {
		t.Fatalf("X-Upstream = %q, want legacy", recorder.Header().Get("X-Upstream"))
	}
	if recorder.Body.String() != "accepted" {
		t.Fatalf("body = %q, want accepted", recorder.Body.String())
	}
}

func TestGatewayRejectsUnauthenticatedProxyRequest(t *testing.T) {
	server := newTestGatewayServer(t, "http://127.0.0.1:1", "token")
	request := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestGatewayHealthAndVersionArePublic(t *testing.T) {
	server := newTestGatewayServer(t, "http://127.0.0.1:1", "token")
	for _, path := range []string{"/health", "/version"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		server.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
	}
}

func newTestGatewayServer(t *testing.T, upstreamURL string, token string) http.Handler {
	t.Helper()
	table, err := NewRouteTable([]routeFile{{Name: "all", PathPattern: "/", LegacyUpstreamURL: upstreamURL}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}
	info, err := health.NewBuildInfo("gateway", "test", "testcommit", fixedBuiltAt())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(&Config{
		BindAddr:          "127.0.0.1:0",
		RoutingConfigPath: "routes.yaml",
		ServiceToken:      token,
		HTTP:              HTTPConfig{ReadHeaderTimeout: testTimeout()},
	}, table, info, slog.Default())
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server.Handler
}
