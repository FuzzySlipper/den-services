package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"den-services/visual-inspect/internal/artifacts"
	"den-services/visual-inspect/internal/config"
	"den-services/visual-inspect/internal/evaluator"
	"den-services/visual-inspect/internal/schema"
	"den-services/visual-inspect/internal/service"
)

func TestEvaluateValidRequestFetchesImageAndReturnsUncertain(t *testing.T) {
	fixture := writePNGFixture(t, 4, 3)
	cfg := testConfig(filepath.Dir(fixture), int64(len(readFile(t, fixture)))+10, 100)
	eval := &recordingEvaluator{}
	handler := newTestHandler(cfg, eval, nil)

	response := postEvaluate(t, handler, validRequestBody(t, fixture))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if eval.calls != 1 {
		t.Fatalf("evaluator calls = %d", eval.calls)
	}
	if len(eval.images) != 1 {
		t.Fatalf("evaluator images = %d", len(eval.images))
	}
	if eval.images[0].Width != 4 || eval.images[0].Height != 3 {
		t.Fatalf("image dimensions = %dx%d", eval.images[0].Width, eval.images[0].Height)
	}
	assertFixtureOnly(t, filepath.Dir(fixture), fixture)
}

func TestEvaluateRejectsInvalidRefsBeforeEvaluator(t *testing.T) {
	fixture := writePNGFixture(t, 2, 2)
	allowedRoot := t.TempDir()
	cfg := testConfig(allowedRoot, int64(len(readFile(t, fixture)))+10, 100)
	eval := &recordingEvaluator{}
	handler := newTestHandler(cfg, eval, nil)

	response := postEvaluate(t, handler, validRequestBody(t, fixture))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if eval.calls != 0 {
		t.Fatalf("evaluator calls = %d", eval.calls)
	}
	assertErrorCode(t, response, "invalid_visual_inspect_request")
}

func TestEvaluateRejectsOverLimitImageBeforeEvaluator(t *testing.T) {
	fixture := writePNGFixture(t, 16, 16)
	cfg := testConfig(filepath.Dir(fixture), int64(len(readFile(t, fixture)))-1, 1000)
	eval := &recordingEvaluator{}
	handler := newTestHandler(cfg, eval, nil)

	response := postEvaluate(t, handler, validRequestBody(t, fixture))

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if eval.calls != 0 {
		t.Fatalf("evaluator calls = %d", eval.calls)
	}
	assertErrorCode(t, response, "visual_inspect_payload_too_large")
}

func TestEvaluateRejectsUnsupportedSchemeBeforeEvaluator(t *testing.T) {
	cfg := testConfig(t.TempDir(), 1000, 1000)
	eval := &recordingEvaluator{}
	handler := newTestHandler(cfg, eval, nil)

	request := validRequest("ftp://example.invalid/screenshot.png")
	response := postEvaluate(t, handler, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if eval.calls != 0 {
		t.Fatalf("evaluator calls = %d", eval.calls)
	}
	assertErrorCode(t, response, "invalid_visual_inspect_request")
}

func TestEvaluateRejectsDenArtifactUntilResolverExists(t *testing.T) {
	cfg := testConfig(t.TempDir(), 1000, 1000)
	eval := &recordingEvaluator{}
	handler := newTestHandler(cfg, eval, nil)

	request := validRequest("den-artifact://den-services/tasks/3421/screenshots/overview.png")
	response := postEvaluate(t, handler, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if eval.calls != 0 {
		t.Fatalf("evaluator calls = %d", eval.calls)
	}
	assertErrorCode(t, response, "unsupported_artifact_ref")
}

func TestEvaluateLogsImageMetadataWithoutRawBytes(t *testing.T) {
	fixture := writePNGFixture(t, 5, 5)
	fixtureBytes := readFile(t, fixture)
	cfg := testConfig(filepath.Dir(fixture), int64(len(fixtureBytes))+10, 100)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	handler := newTestHandler(cfg, &recordingEvaluator{}, logger)

	response := postEvaluate(t, handler, validRequestBody(t, fixture))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, "sha256=") {
		t.Fatalf("expected image hash in logs: %s", logOutput)
	}
	if strings.Contains(logOutput, base64.StdEncoding.EncodeToString(fixtureBytes)) {
		t.Fatalf("raw base64 image bytes were logged: %s", logOutput)
	}
	if strings.Contains(logOutput, string(fixtureBytes)) {
		t.Fatalf("raw image bytes were logged")
	}
	assertFixtureOnly(t, filepath.Dir(fixture), fixture)
}

type recordingEvaluator struct {
	calls  int
	images []evaluator.Image
}

func (e *recordingEvaluator) Evaluate(_ context.Context, req schema.EvaluateRequest, images []evaluator.Image) (schema.EvaluateResponse, error) {
	e.calls++
	e.images = images
	return schema.EvaluateResponse{
		RequestID:       req.RequestID,
		Verdict:         schema.VerdictUncertain,
		Confidence:      0,
		CriteriaResults: []schema.CriterionResult{},
		FollowUpHints:   []string{},
		ModelInfo: schema.ModelInfo{
			Provider:      "openai_compatible",
			Model:         "test-model",
			PromptProfile: req.ProfileOrDefault("visual-inspect-v0"),
		},
		Warnings: []string{"test_evaluator"},
	}, nil
}

func newTestHandler(cfg *config.Config, eval evaluator.Evaluator, logger *slog.Logger) http.Handler {
	fetcher := artifacts.NewFetcher(cfg.Artifacts, nil)
	evaluateService := service.NewService(cfg, fetcher, eval, logger)
	handler := New(evaluateService)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func postEvaluate(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/visual-inspect/evaluate", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func validRequestBody(t *testing.T, fixture string) string {
	t.Helper()
	return validRequest(fileURL(fixture))
}

func validRequest(ref string) string {
	return `{
		"request_id": "req-3421",
		"criteria": [
			{
				"id": "terminal-focused",
				"statement": "The terminal card is visibly selected.",
				"required": true,
				"weight": 1.0
			}
		],
		"screenshots": [
			{
				"id": "overview",
				"ref": "` + ref + `",
				"mime_type": "image/png",
				"description": "overview after selecting terminal"
			}
		],
		"context": {
			"task_title": "Visual smoke",
			"acceptance_summary": "Terminal should be selected",
			"ui_surface": "Agora overview"
		},
		"options": {
			"profile": "visual-inspect-v0",
			"min_confidence_for_pass": 0.70,
			"return_regions": true
		}
	}`
}

func testConfig(root string, maxBytes int64, maxPixels int64) *config.Config {
	return &config.Config{
		Artifacts: config.ArtifactConfig{
			MaxImages:         2,
			MaxBytesPerImage:  maxBytes,
			MaxPixelsPerImage: maxPixels,
			AllowedSchemes:    []string{"file", "http", "https", "den-artifact"},
			AllowedFileRoots:  []string{root},
		},
		Prompts: config.PromptConfig{
			DefaultProfile: "visual-inspect-v0",
			Profiles: map[string]config.PromptProfile{
				"visual-inspect-v0": {
					SystemPromptFile:     "system.md",
					DeveloperPromptFile:  "developer.md",
					ResponseSchemaFile:   "schema.json",
					MinConfidenceForPass: 0.70,
					MinConfidenceForFail: 0.60,
				},
			},
		},
	}
}

func writePNGFixture(t *testing.T, width int, height int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.png")
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating png fixture: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encoding png fixture: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func assertErrorCode(t *testing.T, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if envelope.Error.Code != code {
		t.Fatalf("error code = %q, want %q", envelope.Error.Code, code)
	}
}

func assertFixtureOnly(t *testing.T, dir string, fixture string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading fixture dir: %v", err)
	}
	if len(entries) != 1 || filepath.Join(dir, entries[0].Name()) != fixture {
		t.Fatalf("unexpected files in fixture dir: %v", entries)
	}
}
