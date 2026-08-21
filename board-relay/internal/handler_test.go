package boardrelay

import (
	"context"
	"errors"
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

func TestHandlerSyncSerializesPartialReceiptOnFailure(t *testing.T) {
	service := &fakeRelayUseCases{syncReceipt: SyncReceipt{ProjectID: "alpha", ExportedPosts: 1, ErrorItems: 1, ItemURLs: []string{"https://example.test/issues/1"}}, syncError: &SyncRunError{Phase: "export_posts", Cause: errors.New("github unavailable")}}
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/projects/alpha/board/github-sync", nil))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, wanted := range []string{`"exported_posts":1`, `"error_items":1`, `"item_urls":["https://example.test/issues/1"]`, `"code":"sync_failed"`} {
		if !strings.Contains(recorder.Body.String(), wanted) {
			t.Fatalf("response does not preserve partial receipt %q: %s", wanted, recorder.Body.String())
		}
	}
}

type fakeRelayUseCases struct {
	projectID   string
	visibility  string
	syncReceipt SyncReceipt
	syncError   error
}

func (f *fakeRelayUseCases) Sync(_ context.Context, projectID string) (SyncReceipt, error) {
	f.projectID = projectID
	if f.syncError != nil {
		return f.syncReceipt, f.syncError
	}
	return SyncReceipt{ProjectID: projectID}, nil
}

func (f *fakeRelayUseCases) SetVisibility(_ context.Context, request VisibilityRequest) error {
	f.visibility = request.Visibility
	return nil
}
