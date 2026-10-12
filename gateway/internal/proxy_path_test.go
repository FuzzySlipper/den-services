package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayPreservesEncodedDocumentSlugs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetPath string
		rewrite    pathRewriteFile
		wantPrefix string
	}{
		{name: "direct", wantPrefix: "/v1/projects"},
		{name: "upstream base", targetPath: "/den%20api", wantPrefix: "/den%20api/v1/projects"},
		{name: "rewritten", rewrite: pathRewriteFile{FromPrefix: "/v1/projects", ToPrefix: "/projects"}, wantPrefix: "/projects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, slug := range []struct {
				encoded string
				decoded string
			}{
				{"design%2Fhydrology-9819", "design/hydrology-9819"},
				{"design%2fwater", "design/water"},
				{"design%252Fwater", "design%2Fwater"},
				{"design%2Fwater%20%E6%B0%B4", "design/water 水"},
			} {
				for _, suffix := range []string{"", "/discussion"} {
					t.Run(slug.encoded+suffix, func(t *testing.T) {
						mux := http.NewServeMux()
						mux.HandleFunc("GET "+tc.wantPrefix+"/{project}/documents/{slug}"+suffix, func(w http.ResponseWriter, r *http.Request) {
							wantURI := tc.wantPrefix + "/rusty-craftsurvive/documents/" + slug.encoded + suffix + "?anchor=water%2Flevel"
							if got := r.URL.RequestURI(); got != wantURI {
								t.Errorf("upstream URI = %q, want %q", got, wantURI)
							}
							_, _ = io.WriteString(w, r.PathValue("slug"))
						})
						upstream := httptest.NewServer(mux)
						defer upstream.Close()
						table, err := NewRouteTable([]routeFile{{
							Name: "documents", PathPattern: "/v1/projects", LegacyUpstreamURL: upstream.URL + tc.targetPath, PathRewrite: tc.rewrite,
						}})
						if err != nil {
							t.Fatal(err)
						}
						server := newTestGatewayServerWithRoutes(t, table, "token")
						request := httptest.NewRequest(http.MethodGet, "/v1/projects/rusty-craftsurvive/documents/"+slug.encoded+suffix+"?anchor=water%2Flevel", nil)
						request.Header.Set("Authorization", "Bearer token")
						response := httptest.NewRecorder()
						server.ServeHTTP(response, request)
						if response.Code != http.StatusOK || response.Body.String() != slug.decoded {
							t.Fatalf("document response = %d %q, want 200 %q", response.Code, response.Body.String(), slug.decoded)
						}
					})
				}
			}
		})
	}
}
