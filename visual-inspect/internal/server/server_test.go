package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/shared/health"

	"den-services/visual-inspect/internal/config"
)

func TestNewHTTPServerRegistersHealth(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			ListenAddr:        "127.0.0.1:0",
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	info, err := health.NewBuildInfo("visual-inspect", "dev", "test", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(cfg, info)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d", response.Code)
	}
}

func TestNewHTTPServerAcceptsFutureRouteRegistrars(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			ListenAddr:        "127.0.0.1:0",
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	info, err := health.NewBuildInfo("visual-inspect", "dev", "test", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(cfg, info, testRegistrar{})
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/visual-inspect/future", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("future route status = %d", response.Code)
	}
}

type testRegistrar struct{}

func (testRegistrar) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/visual-inspect/future", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
}
