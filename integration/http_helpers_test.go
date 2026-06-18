package integration

import (
	"net/http"
	"testing"
)

func TestNewAuthenticatedRequest(t *testing.T) {
	request, err := NewAuthenticatedRequest(http.MethodPost, "http://example.test", []byte(`{"ok":true}`), "secret")
	if err != nil {
		t.Fatalf("NewAuthenticatedRequest() error = %v", err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", got)
	}
	if got := request.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}
