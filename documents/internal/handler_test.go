package documents

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerStoreSearchAndComment(t *testing.T) {
	service := NewService(newMemoryStore(), NoopProjectValidator{}, StaticGuidanceReader{Ready: true}, fixedClock())
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/projects/den-services/documents", bytes.NewBufferString(`{"slug":"doc","title":"Doc","content":"searchable body","tags":["docs"]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("store status = %d body=%s", rec.Code, rec.Body.String())
	}
	var doc DocumentResponse
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if doc.ProjectID != "den-services" || doc.Slug != "doc" {
		t.Fatalf("doc response = %#v", doc)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/den-services/documents/search?query=searchable", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d body=%s", rec.Code, rec.Body.String())
	}
	var results []DocumentSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&results); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(results) != 1 || results[0].Slug != "doc" {
		t.Fatalf("results = %#v", results)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/den-services/documents/doc/discussion/comments", bytes.NewBufferString(`{"author_identity":"pi","body_markdown":"hello"}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment status = %d body=%s", rec.Code, rec.Body.String())
	}
	var comment DiscussionCommentResponse
	if err := json.NewDecoder(rec.Body).Decode(&comment); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if comment.ThreadID == 0 || comment.AuthorIdentity != "pi" {
		t.Fatalf("comment = %#v", comment)
	}
}
