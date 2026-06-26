package evaluator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"den-services/visual-inspect/internal/schema"
)

func TestVisionEvaluatorAcceptsStructuredVerdicts(t *testing.T) {
	tests := []struct {
		name    string
		verdict schema.Verdict
	}{
		{"pass", schema.VerdictPass},
		{"fail", schema.VerdictFail},
		{"uncertain", schema.VerdictUncertain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{responses: []string{modelPacket(tt.verdict, 0.91, "visible evidence supports result")}}
			eval := NewVisionEvaluator(testEvaluatorConfig(t), client)

			response, err := eval.Evaluate(context.Background(), testRequest("first title"), testImages())
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if response.Verdict != tt.verdict {
				t.Fatalf("Verdict = %s, want %s", response.Verdict, tt.verdict)
			}
			if response.ModelInfo.Provider != "openai_compatible" || response.ModelInfo.Model != "vision-test" {
				t.Fatalf("ModelInfo = %+v", response.ModelInfo)
			}
			if response.ModelInfo.SchemaVersion != "visual-inspect-evaluate-response/vtest" {
				t.Fatalf("SchemaVersion = %q", response.ModelInfo.SchemaVersion)
			}
			if len(client.requests) != 1 {
				t.Fatalf("provider calls = %d", len(client.requests))
			}
			assertFreshPromptShape(t, client.requests[0])
		})
	}
}

func TestVisionEvaluatorMalformedOutputNormalizesToUncertain(t *testing.T) {
	client := &fakeClient{responses: []string{"```json\n{\"verdict\":\"pass\"}\n```"}}
	eval := NewVisionEvaluator(testEvaluatorConfig(t), client)

	response, err := eval.Evaluate(context.Background(), testRequest("malformed"), testImages())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if response.Verdict != schema.VerdictUncertain {
		t.Fatalf("Verdict = %s", response.Verdict)
	}
	if !containsWarning(response.Warnings, "model_output_invalid") {
		t.Fatalf("Warnings = %v", response.Warnings)
	}
}

func TestVisionEvaluatorLowConfidencePassNormalizesToUncertain(t *testing.T) {
	client := &fakeClient{responses: []string{modelPacket(schema.VerdictPass, 0.30, "visible but weak")}}
	eval := NewVisionEvaluator(testEvaluatorConfig(t), client)

	response, err := eval.Evaluate(context.Background(), testRequest("low confidence"), testImages())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if response.Verdict != schema.VerdictUncertain {
		t.Fatalf("Verdict = %s", response.Verdict)
	}
	if !containsWarning(response.Warnings, "pass_confidence_below_threshold:terminal-focused") {
		t.Fatalf("Warnings = %v", response.Warnings)
	}
}

func TestVisionEvaluatorDoesNotReusePriorRequestContent(t *testing.T) {
	client := &fakeClient{
		responses: []string{
			modelPacket(schema.VerdictUncertain, 0.80, "first visible evidence"),
			modelPacket(schema.VerdictUncertain, 0.80, "second visible evidence"),
		},
	}
	eval := NewVisionEvaluator(testEvaluatorConfig(t), client)

	if _, err := eval.Evaluate(context.Background(), testRequest("first-title-unique"), testImages()); err != nil {
		t.Fatalf("first Evaluate() error = %v", err)
	}
	if _, err := eval.Evaluate(context.Background(), testRequest("second-title-unique"), testImages()); err != nil {
		t.Fatalf("second Evaluate() error = %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("provider calls = %d", len(client.requests))
	}
	secondRequest, err := json.Marshal(client.requests[1])
	if err != nil {
		t.Fatalf("marshalling second request: %v", err)
	}
	serialized := string(secondRequest)
	if strings.Contains(serialized, "first-title-unique") {
		t.Fatalf("second request reused prior content: %s", serialized)
	}
	if strings.Contains(strings.ToLower(serialized), "session") || strings.Contains(strings.ToLower(serialized), "thread") {
		t.Fatalf("request contains persistent conversation handle: %s", serialized)
	}
}

func TestVisionEvaluatorDescribeReturnsPlainText(t *testing.T) {
	client := &fakeClient{responses: []string{"The screenshot shows a terminal card and a browser card."}}
	eval := NewVisionEvaluator(testEvaluatorConfig(t), client)

	response, err := eval.Describe(context.Background(), testDescribeRequest("describe-title"), testImages())
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if !strings.Contains(response.Description, "terminal card") {
		t.Fatalf("Description = %q", response.Description)
	}
	if len(response.ScreenshotIDs) != 1 || response.ScreenshotIDs[0] != "overview" {
		t.Fatalf("ScreenshotIDs = %v", response.ScreenshotIDs)
	}
	if response.ModelInfo.PromptProfile != "visual-inspect-v0/describe" {
		t.Fatalf("PromptProfile = %q", response.ModelInfo.PromptProfile)
	}
	if len(client.requests) != 1 {
		t.Fatalf("provider calls = %d", len(client.requests))
	}
	req := client.requests[0]
	if req.JSONMode {
		t.Fatal("Describe request used JSONMode")
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v", req.Messages)
	}
}

type fakeClient struct {
	requests  []ChatRequest
	responses []string
}

func (c *fakeClient) Complete(_ context.Context, req ChatRequest) (ChatResponse, error) {
	c.requests = append(c.requests, req)
	if len(c.responses) == 0 {
		return ChatResponse{Content: modelPacket(schema.VerdictUncertain, 0.80, "default")}, nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return ChatResponse{Content: response}, nil
}

func testEvaluatorConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	systemPrompt := filepath.Join(dir, "system.md")
	developerPrompt := filepath.Join(dir, "developer.md")
	responseSchema := filepath.Join(dir, "schema.json")
	writeFile(t, systemPrompt, "system prompt: use screenshots only")
	writeFile(t, developerPrompt, "developer prompt: return json")
	writeFile(t, responseSchema, `{"$id":"visual-inspect-evaluate-response/vtest","type":"object"}`)
	return Config{
		Provider:        "openai_compatible",
		Model:           "vision-test",
		Temperature:     0,
		MaxOutputTokens: 2000,
		DefaultProfile:  "visual-inspect-v0",
		Profiles: map[string]PromptProfile{
			"visual-inspect-v0": {
				Name:                 "visual-inspect-v0",
				SystemPromptFile:     systemPrompt,
				DeveloperPromptFile:  developerPrompt,
				ResponseSchemaFile:   responseSchema,
				MinConfidenceForPass: 0.70,
				MinConfidenceForFail: 0.60,
			},
		},
	}
}

func testRequest(title string) schema.EvaluateRequest {
	weight := 1.0
	return schema.EvaluateRequest{
		RequestID: "req-test",
		Criteria: []schema.CriterionRequest{{
			ID:        "terminal-focused",
			Statement: "The terminal is visibly focused.",
			Required:  true,
			Weight:    &weight,
		}},
		Screenshots: []schema.ScreenshotRef{{
			ID:          "overview",
			Ref:         "file:///tmp/overview.png",
			MimeType:    "image/png",
			Description: "overview",
		}},
		Context: &schema.EvaluateContext{TaskTitle: title},
		Options: &schema.EvaluateOptions{
			Profile:       "visual-inspect-v0",
			ReturnRegions: true,
		},
	}
}

func testDescribeRequest(title string) schema.DescribeRequest {
	return schema.DescribeRequest{
		RequestID: "describe-test",
		Screenshots: []schema.ScreenshotRef{{
			ID:          "overview",
			Ref:         "file:///tmp/overview.png",
			MimeType:    "image/png",
			Description: "overview",
		}},
		Context: &schema.EvaluateContext{TaskTitle: title},
		Prompt:  "Describe the visible cards.",
		Options: &schema.DescribeOptions{
			Profile: "visual-inspect-v0",
			Detail:  "concise",
		},
	}
}

func testImages() []Image {
	return []Image{{
		ScreenshotID: "overview",
		MimeType:     "image/png",
		Bytes:        []byte("png-bytes"),
		Width:        10,
		Height:       8,
		SHA256:       "hash",
	}}
}

func modelPacket(verdict schema.Verdict, confidence float64, explanation string) string {
	response := schema.EvaluateResponse{
		Verdict:    verdict,
		Confidence: confidence,
		CriteriaResults: []schema.CriterionResult{{
			CriterionID:  "terminal-focused",
			Verdict:      verdict,
			Confidence:   confidence,
			Explanation:  explanation,
			Observations: []schema.Observation{},
		}},
		FollowUpHints: []string{},
		Warnings:      []string{},
	}
	data, err := json.Marshal(response)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func assertFreshPromptShape(t *testing.T, req ChatRequest) {
	t.Helper()
	if req.Model != "vision-test" {
		t.Fatalf("Model = %q", req.Model)
	}
	if req.Temperature != 0 {
		t.Fatalf("Temperature = %v", req.Temperature)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[1].Role != "developer" || req.Messages[2].Role != "user" {
		t.Fatalf("roles = %s,%s,%s", req.Messages[0].Role, req.Messages[1].Role, req.Messages[2].Role)
	}
	userParts := req.Messages[2].Content
	if len(userParts) != 2 {
		t.Fatalf("user content parts = %d", len(userParts))
	}
	if userParts[1].Type != "image" || userParts[1].MimeType != "image/png" {
		t.Fatalf("image part = %+v", userParts[1])
	}
}

func containsWarning(warnings []string, expected string) bool {
	for _, warning := range warnings {
		if warning == expected {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
