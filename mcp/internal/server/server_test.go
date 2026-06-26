package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/shared/health"

	"den-services/mcp/internal/config"
)

func TestNewHTTPServerRegistersHealthAndVersion(t *testing.T) {
	server := newTestServer(t, true, nil)

	for _, path := range []string{"/health", "/version"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusOK)
		}
	}
}

func TestPlaceholderMCPHandlerIsWired(t *testing.T) {
	server := newTestServer(t, true, nil)

	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotImplemented {
		t.Fatalf("POST /mcp status = %d, want %d", response.Code, http.StatusNotImplemented)
	}
}

func TestMCPHandlerRequiresAuthWhenConfigured(t *testing.T) {
	server := newTestServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("POST /mcp without token status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	request = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("POST /mcp with token status = %d, want %d", response.Code, http.StatusAccepted)
	}
}

func TestPlaceholderMCPRejectsNonPost(t *testing.T) {
	server := newTestServer(t, true, nil)

	request := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func newTestServer(t *testing.T, allowUnauthenticatedLocalDev bool, handler MCPHandler) *http.Server {
	t.Helper()
	info, err := health.NewBuildInfo("mcp", "dev", "test", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(&config.Config{
		Server: config.ServerConfig{
			ListenAddr:        "127.0.0.1:0",
			MCPEndpointPath:   "/mcp",
			ReadHeaderTimeout: 5 * time.Second,
		},
		Security: config.SecurityConfig{
			ServiceToken:                 "test-token",
			AllowUnauthenticatedLocalDev: allowUnauthenticatedLocalDev,
		},
	}, info, handler)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server
}
