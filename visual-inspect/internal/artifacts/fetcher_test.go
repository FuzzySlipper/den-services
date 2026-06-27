package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"den-services/visual-inspect/internal/config"
	"den-services/visual-inspect/internal/schema"
)

func TestFetcherResolvesCanonicalDenArtifactRef(t *testing.T) {
	server := newArtifactServer(t, "art_123", tinyPNG(t, 2, 2), http.StatusOK)
	fetcher := NewFetcher(denArtifactConfig(server.URL, 1024, 100), server.Client())

	image, err := fetcher.Fetch(context.Background(), schema.ScreenshotRef{
		ID:       "overview",
		Ref:      "den-artifact://art_123",
		MimeType: MimePNG,
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if image.Width != 2 || image.Height != 2 {
		t.Fatalf("dimensions = %dx%d", image.Width, image.Height)
	}
	if image.RefScheme != "den-artifact" {
		t.Fatalf("RefScheme = %s", image.RefScheme)
	}
	if !image.Sensitive {
		t.Fatal("Sensitive = false, want registry sensitive flag to propagate")
	}
}

func TestFetcherResolvesScopedDenArtifactRef(t *testing.T) {
	server := newArtifactServer(t, "art_scoped", tinyPNG(t, 3, 1), http.StatusOK)
	fetcher := NewFetcher(denArtifactConfig(server.URL, 1024, 100), server.Client())

	image, err := fetcher.Fetch(context.Background(), schema.ScreenshotRef{
		ID:       "overview",
		Ref:      "den-artifact://den-services/tasks/3477/artifacts/overview.png",
		MimeType: MimePNG,
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if image.Width != 3 || image.Height != 1 {
		t.Fatalf("dimensions = %dx%d", image.Width, image.Height)
	}
	if !server.sawResolve {
		t.Fatal("artifact resolve endpoint was not called")
	}
}

func TestFetcherMapsMissingAndUnauthorizedArtifacts(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantCode   string
	}{
		{"missing", http.StatusNotFound, "artifact_not_found"},
		{"unauthorized", http.StatusUnauthorized, "artifact_unauthorized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newArtifactServer(t, "art_123", tinyPNG(t, 1, 1), tt.statusCode)
			fetcher := NewFetcher(denArtifactConfig(server.URL, 1024, 100), server.Client())

			_, err := fetcher.Fetch(context.Background(), schema.ScreenshotRef{
				ID:       "overview",
				Ref:      "den-artifact://art_123",
				MimeType: MimePNG,
			})
			assertRequestErrorCode(t, err, tt.wantCode)
		})
	}
}

func TestFetcherRejectsOversizedDenArtifactAfterFetch(t *testing.T) {
	server := newArtifactServer(t, "art_123", tinyPNG(t, 16, 16), http.StatusOK)
	fetcher := NewFetcher(denArtifactConfig(server.URL, 1024*1024, 10), server.Client())

	_, err := fetcher.Fetch(context.Background(), schema.ScreenshotRef{
		ID:       "overview",
		Ref:      "den-artifact://art_123",
		MimeType: MimePNG,
	})
	assertRequestErrorCode(t, err, "visual_inspect_payload_too_large")
}

type artifactServer struct {
	*httptest.Server
	t          *testing.T
	artifactID string
	content    []byte
	statusCode int
	sawResolve bool
}

func newArtifactServer(t *testing.T, artifactID string, content []byte, statusCode int) *artifactServer {
	t.Helper()
	server := &artifactServer{
		t:          t,
		artifactID: artifactID,
		content:    content,
		statusCode: statusCode,
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serveHTTP))
	t.Cleanup(server.Close)
	return server
}

func (s *artifactServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer artifact-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if s.statusCode != http.StatusOK {
		w.WriteHeader(s.statusCode)
		return
	}
	switch {
	case r.URL.Path == "/v1/artifacts/resolve":
		s.sawResolve = true
		if !strings.HasPrefix(r.URL.Query().Get("ref"), "den-artifact://den-services/tasks/3477/") {
			s.t.Fatalf("resolve ref = %s", r.URL.Query().Get("ref"))
		}
		s.writeMetadata(w)
	case r.URL.Path == "/v1/artifacts/"+s.artifactID+"/metadata":
		s.writeMetadata(w)
	case r.URL.Path == "/v1/artifacts/"+s.artifactID+"/content":
		w.Header().Set("Content-Type", MimePNG)
		_, _ = w.Write(s.content)
	default:
		s.t.Fatalf("unexpected artifact request %s", r.URL.String())
	}
}

func (s *artifactServer) writeMetadata(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		ArtifactID string `json:"artifact_id"`
		MimeType   string `json:"mime_type"`
		Sensitive  bool   `json:"sensitive"`
	}{
		ArtifactID: s.artifactID,
		MimeType:   MimePNG,
		Sensitive:  true,
	})
}

func denArtifactConfig(baseURL string, maxBytes int64, maxPixels int64) config.ArtifactConfig {
	return config.ArtifactConfig{
		MaxImages:         2,
		MaxBytesPerImage:  maxBytes,
		MaxPixelsPerImage: maxPixels,
		AllowedSchemes:    []string{"den-artifact"},
		ServiceBaseURL:    baseURL,
		ServiceToken:      "artifact-token",
		ServiceTimeout:    time.Second,
	}
}

func tinyPNG(t *testing.T, width int, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: uint8(x), B: uint8(y), A: 255})
		}
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding png: %v", err)
	}
	return buf.Bytes()
}

func assertRequestErrorCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	requestErr, ok := err.(*schema.RequestError)
	if !ok {
		t.Fatalf("error = %T %v, want *RequestError", err, err)
	}
	if requestErr.Code() != wantCode {
		t.Fatalf("error code = %s, want %s", requestErr.Code(), wantCode)
	}
}
