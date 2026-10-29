package integration

import (
	"net/http"
	"testing"
)

func TestNewJSONRequest(t *testing.T) {
	request, err := NewJSONRequest(http.MethodPost, "http://example.test", []byte(`{"ok":true}`))
	if err != nil {
		t.Fatalf("NewJSONRequest() error = %v", err)
	}
	if got := request.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want none", got)
	}
	if got := request.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}
