package artifacts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestNewHTTPServerRegistersHealthAndProtectsAPI(t *testing.T) {
	server, err := NewHTTPServer(testServerConfig(), testBuildInfo(t), &notFoundService{})
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}

	healthRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(healthResponse, healthRequest)
	if healthResponse.Code != http.StatusOK {
		t.Fatalf("health status = %d", healthResponse.Code)
	}

	apiRequest := httptest.NewRequest(http.MethodGet, "/v1/artifacts/art_123/metadata", nil)
	apiResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusUnauthorized {
		t.Fatalf("api status without token = %d", apiResponse.Code)
	}

	authRequest := httptest.NewRequest(http.MethodGet, "/v1/artifacts/art_123/metadata", nil)
	authRequest.Header.Set("Authorization", "Bearer test-token")
	authResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(authResponse, authRequest)
	if authResponse.Code != http.StatusNotFound {
		t.Fatalf("api status with token = %d", authResponse.Code)
	}
}

func testServerConfig() *Config {
	return &Config{
		BindAddr:     "127.0.0.1:0",
		DatabaseURL:  "postgres://example",
		ServiceToken: "test-token",
		Storage: StorageConfig{
			Backend:   "filesystem",
			RootPath:  "/var/lib/den/artifacts",
			KeyPrefix: "sha256",
		},
		Limits: LimitConfig{
			MaxBytesPerArtifact: 1024,
			MaxPixelsPerImage:   1,
		},
		Retention: RetentionConfig{
			TemporaryTTL: time.Hour,
		},
		HTTP: HTTPConfig{ReadHeaderTimeout: time.Second},
	}
}

func testBuildInfo(t *testing.T) health.BuildInfo {
	t.Helper()
	info, err := health.NewBuildInfo("artifacts", "dev", "test", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	return info
}

type notFoundService struct{}

func (s *notFoundService) Create(context.Context, CreateArtifactRequest, UploadContent) (*Artifact, error) {
	return nil, notFound("missing")
}

func (s *notFoundService) GetMetadata(context.Context, string) (*Artifact, error) {
	return nil, notFound("missing")
}

func (s *notFoundService) ResolveRef(context.Context, string) (*Artifact, error) {
	return nil, notFound("missing")
}

func (s *notFoundService) OpenContent(context.Context, string) (*ArtifactContent, error) {
	return nil, notFound("missing")
}

func (s *notFoundService) Delete(context.Context, string) (*Artifact, error) {
	return nil, notFound("missing")
}
