package docpublish

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPDocumentFetcherUsesCoreDocumentRouteAndShape(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer source-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Core Title",
			"slug": "core-doc",
			"content": "# Core body",
			"updated_at": "2026-06-22T01:02:03Z"
		}`))
	}))
	t.Cleanup(server.Close)

	fetcher := NewHTTPDocumentFetcher(server.URL, "source-token", time.Second)
	document, err := fetcher.Fetch(context.Background(), DocumentSource{
		DocumentProjectID: "den-web",
		DocumentSlug:      "core-doc",
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if gotPath != "/api/projects/den-web/documents/core-doc" {
		t.Fatalf("path = %s, want Core document route", gotPath)
	}
	if document.Title != "Core Title" || document.Markdown != "# Core body" || document.Slug != "core-doc" {
		t.Fatalf("document = %+v", document)
	}
	if document.UpdatedAt.IsZero() {
		t.Fatal("updated_at should be decoded")
	}
}

func TestHTTPDocumentFetcherFallsBackToSuccessorDocumentRoute(t *testing.T) {
	var gotPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer source-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		if r.URL.Path != "/v1/projects/asha/documents/successor-doc" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Successor Title",
			"slug": "successor-doc",
			"content": "# Successor body",
			"updated_at": "2026-07-02T10:00:28.610687Z"
		}`))
	}))
	t.Cleanup(server.Close)

	fetcher := NewHTTPDocumentFetcher(server.URL, "source-token", time.Second)
	document, err := fetcher.Fetch(context.Background(), DocumentSource{
		DocumentProjectID: "asha",
		DocumentSlug:      "successor-doc",
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(gotPaths) != 2 || gotPaths[0] != "/api/projects/asha/documents/successor-doc" || gotPaths[1] != "/v1/projects/asha/documents/successor-doc" {
		t.Fatalf("paths = %v, want legacy route then successor route", gotPaths)
	}
	if document.Title != "Successor Title" || document.Markdown != "# Successor body" || document.Slug != "successor-doc" {
		t.Fatalf("document = %+v", document)
	}
}

func TestHTTPDocumentFetcherUsesConfiguredVersionedBaseURL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"Configured Title","slug":"configured-doc","content":"Configured body"}`))
	}))
	t.Cleanup(server.Close)

	fetcher := NewHTTPDocumentFetcher(server.URL+"/v1", "", time.Second)
	if _, err := fetcher.Fetch(context.Background(), DocumentSource{
		DocumentProjectID: "den-services",
		DocumentSlug:      "configured-doc",
	}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if gotPath != "/v1/projects/den-services/documents/configured-doc" {
		t.Fatalf("path = %s, want configured versioned document route", gotPath)
	}
}

func TestHTTPDocumentFetcherAcceptsCoreTimestampWithoutTimezone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"title": "Core Title",
			"slug": "core-doc",
			"content": "# Core body",
			"updated_at": "2026-06-22T05:05:49"
		}`))
	}))
	t.Cleanup(server.Close)

	fetcher := NewHTTPDocumentFetcher(server.URL, "", time.Second)
	document, err := fetcher.Fetch(context.Background(), DocumentSource{
		DocumentProjectID: "research",
		DocumentSlug:      "core-doc",
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := document.UpdatedAt.Format(time.RFC3339); got != "2026-06-22T05:05:49Z" {
		t.Fatalf("updated_at = %s, want UTC-normalized Core timestamp", got)
	}
}
