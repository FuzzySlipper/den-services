package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceTokenAuthMiddlewareAllowsBearerToken(t *testing.T) {
	auth, err := NewServiceTokenAuth("secret")
	if err != nil {
		t.Fatalf("NewServiceTokenAuth() error = %v", err)
	}

	called := false
	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if !called {
		t.Fatal("next handler was not called")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestServiceTokenAuthMiddlewareRejectsMissingToken(t *testing.T) {
	auth, err := NewServiceTokenAuth("secret")
	if err != nil {
		t.Fatalf("NewServiceTokenAuth() error = %v", err)
	}

	handler := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestNewServiceTokenAuthValidatesToken(t *testing.T) {
	_, err := NewServiceTokenAuth("")
	if !errors.Is(err, ErrMissingServiceToken) {
		t.Fatalf("NewServiceTokenAuth() error = %v, want %v", err, ErrMissingServiceToken)
	}
}
