package serve

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	devserver "den-services/devserver-broker"
)

type fakeSessionLister struct {
	sessions []devserver.SessionState
	err      error
}

func (f fakeSessionLister) List(context.Context) ([]devserver.SessionState, error) {
	return f.sessions, f.err
}

func TestStatusPageListsOnlyRunningProjectsWithClickableAssignments(t *testing.T) {
	page, err := NewStatusPage(fakeSessionLister{sessions: []devserver.SessionState{
		{Project: "zeta", Status: "running", Port: 37302, LANURL: "http://192.168.1.22:37302/"},
		{Project: "stopped", Status: "stopped", Port: 37301, LANURL: "http://192.168.1.22:37301/"},
		{Project: "alpha", Status: "running", Port: 5173, LANURL: "http://192.168.1.22:5173/"},
	}})
	if err != nil {
		t.Fatalf("NewStatusPage() error = %v", err)
	}
	page.clock = func() time.Time { return time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC) }

	request := httptest.NewRequest(http.MethodGet, "http://192.168.1.22:37299/", nil)
	response := httptest.NewRecorder()
	page.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, marker := range []string{
		`href="http://192.168.1.22:5173/">alpha</a>`,
		`href="http://192.168.1.22:37302/">zeta</a>`,
		"Refreshed 2026-08-12T10:00:00Z",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("body missing %q:\n%s", marker, body)
		}
	}
	if strings.Contains(body, ">stopped</a>") {
		t.Fatalf("body includes stopped session:\n%s", body)
	}
	if strings.Index(body, ">alpha</a>") > strings.Index(body, ">zeta</a>") {
		t.Fatalf("projects are not sorted:\n%s", body)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestStatusPageEscapesProjectNamesAndUsesRequestHostFallback(t *testing.T) {
	page, err := NewStatusPage(fakeSessionLister{sessions: []devserver.SessionState{
		{Project: `<script>alert("no")</script>`, Status: "running", Port: 4040},
	}})
	if err != nil {
		t.Fatalf("NewStatusPage() error = %v", err)
	}
	response := httptest.NewRecorder()
	page.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://den-box.local:37299/", nil))
	body := response.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("project name was not escaped:\n%s", body)
	}
	if !strings.Contains(body, `href="http://den-box.local:4040/"`) {
		t.Fatalf("body missing request-host fallback URL:\n%s", body)
	}
}

func TestStatusPageReturnsErrorWhenLiveListFails(t *testing.T) {
	page, err := NewStatusPage(fakeSessionLister{err: errors.New("state unavailable")})
	if err != nil {
		t.Fatalf("NewStatusPage() error = %v", err)
	}
	response := httptest.NewRecorder()
	page.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}

func TestStatusPageRejectsMutatingMethods(t *testing.T) {
	page, err := NewStatusPage(fakeSessionLister{})
	if err != nil {
		t.Fatalf("NewStatusPage() error = %v", err)
	}
	response := httptest.NewRecorder()
	page.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://localhost/", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.Code)
	}
}
