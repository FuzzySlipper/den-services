package tasks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectScopeClientAssertWritable(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer projects-token" {
			t.Fatalf("Authorization = %q", got)
		}
		switch r.URL.EscapedPath() {
		case "/v1/scopes/rusty%2Froleplay/assert-writable":
			w.WriteHeader(http.StatusNoContent)
		case "/v1/scopes/archived/assert-writable":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"archived_scope_write","message":"scope is archived"}}`))
		case "/v1/scopes/missing/assert-writable":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"scope_not_found","message":"scope not found"}}`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.EscapedPath())
		}
	}))
	defer server.Close()

	client := NewProjectScopeClient(server.URL, "projects-token")
	if err := client.AssertWritable(context.Background(), "rusty/roleplay"); err != nil {
		t.Fatalf("AssertWritable(success) error = %v", err)
	}
	if err := client.AssertWritable(context.Background(), "archived"); serviceErrorStatus(err) != http.StatusConflict {
		t.Fatalf("AssertWritable(conflict) error = %v", err)
	}
	if err := client.AssertWritable(context.Background(), "missing"); serviceErrorStatus(err) != http.StatusBadRequest {
		t.Fatalf("AssertWritable(not found) error = %v", err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestProjectScopeClientFailsClosedWhenUnconfigured(t *testing.T) {
	err := NewProjectScopeClient("", "projects-token").AssertWritable(context.Background(), "den-services")
	if !errors.Is(err, ErrProjectScopeClientUnconfigured) || serviceErrorStatus(err) != http.StatusInternalServerError {
		t.Fatalf("AssertWritable(unconfigured) error = %v", err)
	}
}

func serviceErrorStatus(err error) int {
	var serviceError interface {
		HTTPStatus() int
	}
	if errors.As(err, &serviceError) {
		return serviceError.HTTPStatus()
	}
	return 0
}
