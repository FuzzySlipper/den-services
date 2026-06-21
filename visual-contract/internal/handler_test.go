package visualcontract

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestServerRoutesRequireAuthAndExposeHealth(t *testing.T) {
	cfg := &Config{
		BindAddr:     "127.0.0.1:0",
		ServiceToken: "test-token",
		Artifacts:    ArtifactConfig{BaseURL: "http://127.0.0.1:8086/artifacts"},
		HTTP:         HTTPConfig{ReadHeaderTimeout: time.Second},
	}
	info, err := health.NewBuildInfo("visual-contract", "dev", "unknown", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(cfg, info)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}

	healthRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want 200", healthRecorder.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/visual-contracts/validate", strings.NewReader("{}")))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.Code)
	}
}

func TestValidateRoute(t *testing.T) {
	cfg := &Config{
		BindAddr:     "127.0.0.1:0",
		ServiceToken: "test-token",
		Artifacts:    ArtifactConfig{BaseURL: "http://127.0.0.1:8086/artifacts"},
		HTTP:         HTTPConfig{ReadHeaderTimeout: time.Second},
	}
	info, err := health.NewBuildInfo("visual-contract", "dev", "unknown", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(cfg, info)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	contract := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	body, err := json.Marshal(ValidateRequest{Contract: contract})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/visual-contracts/validate", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-token")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("validate status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	assertContains(t, recorder.Body.String(), `"valid":true`)
}

func loadJSONFixture(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", path, err)
	}
}

func assertContains(t *testing.T, text string, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("text does not contain %q\n%s", want, text)
	}
}
