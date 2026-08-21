package boardrelay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerSyncPassesExplicitProject(t *testing.T) {
	service := &fakeRelayUseCases{}
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/projects/rusty-engine/board/github-sync", nil))
	if recorder.Code != http.StatusOK || service.projectID != "rusty-engine" {
		t.Fatalf("status=%d project=%q", recorder.Code, service.projectID)
	}
}

func TestHandlerVisibilityRequiresTypedBody(t *testing.T) {
	service := &fakeRelayUseCases{}
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/v1/board/github-visibility", strings.NewReader(`{"visibility":"private"}`)))
	if recorder.Code != http.StatusNoContent || service.visibility != "private" {
		t.Fatalf("status=%d visibility=%q", recorder.Code, service.visibility)
	}
}

type fakeRelayUseCases struct {
	projectID  string
	visibility string
}

func (f *fakeRelayUseCases) Sync(_ context.Context, projectID string) (SyncReceipt, error) {
	f.projectID = projectID
	return SyncReceipt{ProjectID: projectID}, nil
}

func (f *fakeRelayUseCases) SetVisibility(_ context.Context, request VisibilityRequest) error {
	f.visibility = request.Visibility
	return nil
}
