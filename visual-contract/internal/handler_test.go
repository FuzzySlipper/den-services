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
		Artifacts:    ArtifactConfig{BaseURL: "http://127.0.0.1:8086/visual-contracts", Path: t.TempDir()},
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
		Artifacts:    ArtifactConfig{BaseURL: "http://127.0.0.1:8086/visual-contracts", Path: t.TempDir()},
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

func TestCompareRoutePersistsAndRetrievesArtifacts(t *testing.T) {
	server := newTestServer(t)
	reference := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	candidate := loadContractFixture(t, "../testdata/contracts/candidate.fail.web-ui.json")
	body, err := json.Marshal(CompareRequest{Reference: reference, Candidate: candidate})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request := authedRequest(http.MethodPost, "/visual-contracts/compare", body)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("compare status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var report ComparisonReport
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("Unmarshal(compare response) error = %v", err)
	}
	if report.RunID == "" {
		t.Fatal("compare response missing run_id")
	}

	unauthorized := httptest.NewRecorder()
	server.Handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/visual-contracts/"+report.RunID+"/artifacts/report.json", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized artifact status = %d, want 401", unauthorized.Code)
	}

	fetch := httptest.NewRecorder()
	server.Handler.ServeHTTP(fetch, authedRequest(http.MethodGet, "/visual-contracts/"+report.RunID+"/artifacts/report.json", nil))
	if fetch.Code != http.StatusOK {
		t.Fatalf("artifact status = %d, want 200; body=%s", fetch.Code, fetch.Body.String())
	}
	assertContains(t, fetch.Body.String(), `"run_id": "`+report.RunID+`"`)
	assertContains(t, fetch.Body.String(), `"verdict": "fail"`)
}

func TestPromoteContractRoute(t *testing.T) {
	server := newTestServer(t)
	generic := genericASHALikeContract()
	req := ContractPromotionRequest{
		Contract: &generic,
		Objects: []ObjectPromotionRule{
			{SourceID: "node_2", TargetID: "central_3d_viewport", DomainRole: "central_3d_viewport"},
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request := authedRequest(http.MethodPost, "/visual-contracts/promote-contract", body)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("promote-contract status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	assertContains(t, recorder.Body.String(), `"id":"central_3d_viewport"`)
	assertContains(t, recorder.Body.String(), `"diagnostics"`)
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

func newTestServer(t *testing.T) *http.Server {
	t.Helper()
	cfg := &Config{
		BindAddr:     "127.0.0.1:0",
		ServiceToken: "test-token",
		Artifacts:    ArtifactConfig{BaseURL: "http://127.0.0.1:8086/visual-contracts", Path: t.TempDir()},
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
	return server
}

func authedRequest(method string, target string, body []byte) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func assertContains(t *testing.T, text string, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("text does not contain %q\n%s", want, text)
	}
}
