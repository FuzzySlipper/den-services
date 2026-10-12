package webedge

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEdgePreservesEncodedDocumentSlugs(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		slug string
	}{
		{"document", "design%2Fhydrology-9819", "design/hydrology-9819"},
		{"discussion", "design%2Fwater-9819/discussion", "design/water-9819"},
		{"literal percent", "design%252Fwater", "design%2Fwater"},
		{"spaces and unicode", "design%2Fwater%20%E6%B0%B4", "design/water 水"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			handler := func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.RequestURI(); got != "/v1/projects/rusty-craftsurvive/documents/"+tc.path+"?anchor=water%2Flevel" {
					t.Errorf("upstream URI = %q", got)
				}
				_, _ = io.WriteString(w, r.PathValue("slug"))
			}
			mux.HandleFunc("GET /v1/projects/{project}/documents/{slug}", handler)
			mux.HandleFunc("GET /v1/projects/{project}/documents/{slug}/discussion", handler)
			upstream := httptest.NewServer(mux)
			defer upstream.Close()
			edge := newTestEdge(t, releaseFixture(t), upstream.URL)
			response := get(t, edge.URL+"/api/v1/projects/rusty-craftsurvive/documents/"+tc.path+"?anchor=water%2Flevel")
			body := readBody(t, response)
			if response.StatusCode != http.StatusOK || body != tc.slug {
				t.Fatalf("document response = %d %q, want 200 %q", response.StatusCode, body, tc.slug)
			}
		})
	}
}
