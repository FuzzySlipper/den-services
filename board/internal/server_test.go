package board

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestHTTPServerRejectsRequestBodiesOverConfiguredLimit(t *testing.T) {
	limits := DefaultLimits()
	service := NewServiceWithLimits(NewMemoryStore(), NoopProjectValidator{}, fixedClock(), limits)
	server := newTestHTTPServer(t, service, Config{
		BindAddr:        "127.0.0.1:8100",
		ServiceToken:    "board-token",
		AdapterIdentity: "den-web-adapter",
		Limits:          limits,
		HTTP: HTTPConfig{
			ReadHeaderTimeout:   time.Second,
			MaxRequestBodyBytes: 64,
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/projects/project-a/board/posts", strings.NewReader(`{"title":"topic","body_markdown":"`+strings.Repeat("x", 200)+`","author_identity":"agent"}`))
	request.Header.Set("Authorization", "Bearer board-token")
	request.Header.Set("Content-Type", "application/json")
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized request status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestHTTPServerBindsAdapterIdentityAfterAuthentication(t *testing.T) {
	store := &auditCaptureStore{MemoryStore: NewMemoryStore()}
	service := NewService(store, NoopProjectValidator{}, fixedClock())
	server := newTestHTTPServer(t, service, Config{
		BindAddr:        "127.0.0.1:8100",
		ServiceToken:    "board-token",
		AdapterIdentity: "server-den-web",
		Limits:          DefaultLimits(),
		HTTP: HTTPConfig{
			ReadHeaderTimeout:   time.Second,
			MaxRequestBodyBytes: DefaultMaxRequestBodyBytes,
		},
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/projects/project-a/board/posts", strings.NewReader(`{"title":"topic","body_markdown":"body","author_identity":"agent"}`))
	request.Header.Set("Authorization", "Bearer board-token")
	request.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(createResponse, request)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", createResponse.Code, createResponse.Body.String())
	}

	purgeRequest := httptest.NewRequest(http.MethodDelete, "/v1/board/posts/1", strings.NewReader(`{"actor_identity":"attacker","reason":"misleading"}`))
	purgeRequest.Header.Set("Authorization", "Bearer board-token")
	purgeRequest.Header.Set("Content-Type", "application/json")
	purgeResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(purgeResponse, purgeRequest)
	if purgeResponse.Code != http.StatusNoContent {
		t.Fatalf("purge status = %d: %s", purgeResponse.Code, purgeResponse.Body.String())
	}
	if store.audit.AdapterIdentity != "server-den-web" {
		t.Fatalf("stored adapter identity = %q", store.audit.AdapterIdentity)
	}
}

func newTestHTTPServer(t *testing.T, service BoardUseCases, cfg Config) *http.Server {
	t.Helper()
	info, err := health.NewBuildInfo("board", "test", "test", time.Date(2026, time.August, 13, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewHTTPServer(&cfg, info, service)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
