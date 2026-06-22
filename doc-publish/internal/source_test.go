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
