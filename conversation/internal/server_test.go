package conversation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestHealthAndVersionArePublic(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{"/health", "/version"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()

		server.Handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
	}
}

func TestConversationAPIRequiresAuth(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/conversation/channels", nil)
	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestConversationAPIAuthenticatedPlaceholderIsNotFound(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/conversation/channels", nil)
	request.Header.Set("Authorization", "Bearer conversation-token")
	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func newTestServer(t *testing.T) *http.Server {
	t.Helper()
	info, err := health.NewBuildInfo("conversation", "test", "testcommit", fixedBuiltAt())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServerWithStore(&Config{
		BindAddr:     "127.0.0.1:0",
		DatabaseURL:  "postgres://channels.example/denservices",
		ServiceToken: "conversation-token",
		DefaultLimit: 100,
		MaxLimit:     500,
		HTTP:         HTTPConfig{ReadHeaderTimeout: 5 * time.Second},
	}, info, fakeStore{})
	if err != nil {
		t.Fatalf("NewHTTPServerWithStore() error = %v", err)
	}
	return server
}

type fakeStore struct{}

func (fakeStore) Ping(context.Context) error {
	return nil
}

func fixedBuiltAt() time.Time {
	return time.Date(2026, 6, 20, 2, 0, 0, 0, time.UTC)
}
