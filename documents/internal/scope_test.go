package documents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentGuidanceClientDocumentReferencesUsesGuidanceSuccessorRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/guidance/document-references" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("document_project_id") != "asha-rulebench" || r.URL.Query().Get("document_slug") != "north-star" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer guidance-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"references":[],"count":0}`))
	}))
	defer server.Close()

	client := NewAgentGuidanceClient(server.URL, "guidance-token")
	references, ready, err := client.DocumentReferences(context.Background(), "asha-rulebench", "north-star")
	if err != nil {
		t.Fatalf("DocumentReferences() error = %v", err)
	}
	if !ready {
		t.Fatal("DocumentReferences() ready = false")
	}
	if len(references) != 0 {
		t.Fatalf("DocumentReferences() = %#v", references)
	}
}
