package evaluator

import (
	"context"
	"fmt"
	"strings"

	"den-services/shared/api"

	"den-services/visual-inspect/internal/schema"
)

type VisionEvaluator struct {
	cfg    Config
	client ProviderClient
}

func NewVisionEvaluator(cfg Config, client ProviderClient) *VisionEvaluator {
	return &VisionEvaluator{
		cfg:    cfg,
		client: client,
	}
}

func (e *VisionEvaluator) Evaluate(ctx context.Context, req schema.EvaluateRequest, images []Image) (schema.EvaluateResponse, error) {
	profile, err := e.profileFor(req)
	if err != nil {
		return schema.EvaluateResponse{}, err
	}
	rendered, err := renderPrompt(profile, req, images)
	if err != nil {
		return schema.EvaluateResponse{}, err
	}
	metadata, err := loadSchemaMetadata(profile.ResponseSchemaFile)
	if err != nil {
		return schema.EvaluateResponse{}, err
	}
	metadata.provider = e.cfg.Provider
	metadata.model = e.cfg.Model
	chatReq := ChatRequest{
		Provider:        e.cfg.Provider,
		Model:           e.cfg.Model,
		Temperature:     e.cfg.Temperature,
		MaxOutputTokens: e.cfg.MaxOutputTokens,
		JSONMode:        true,
		Messages:        rendered.messages,
	}
	chatResp, err := e.client.Complete(ctx, chatReq)
	if err != nil {
		return schema.EvaluateResponse{}, fmt.Errorf("%w: visual LLM provider unavailable: %w", api.ErrUnavailable, err)
	}
	response, _ := normalizeModelResponse(chatResp.Content, req, profile, metadata)
	return response, nil
}

func (e *VisionEvaluator) Describe(ctx context.Context, req schema.DescribeRequest, images []Image) (schema.DescribeResponse, error) {
	profile, err := e.describeProfileFor(req)
	if err != nil {
		return schema.DescribeResponse{}, err
	}
	rendered, err := renderDescribePrompt(profile, req, images)
	if err != nil {
		return schema.DescribeResponse{}, err
	}
	chatReq := ChatRequest{
		Provider:        e.cfg.Provider,
		Model:           e.cfg.Model,
		Temperature:     e.cfg.Temperature,
		MaxOutputTokens: e.cfg.MaxOutputTokens,
		JSONMode:        false,
		Messages:        rendered.messages,
	}
	chatResp, err := e.client.Complete(ctx, chatReq)
	if err != nil {
		return schema.DescribeResponse{}, fmt.Errorf("%w: visual LLM provider unavailable: %w", api.ErrUnavailable, err)
	}
	description := strings.TrimSpace(chatResp.Content)
	warnings := []string{}
	if description == "" {
		description = "No description was returned by the model."
		warnings = append(warnings, "empty_model_description")
	}
	return schema.DescribeResponse{
		RequestID:     req.RequestID,
		Description:   description,
		ScreenshotIDs: screenshotIDs(images),
		ModelInfo: schema.ModelInfo{
			Provider:      e.cfg.Provider,
			Model:         e.cfg.Model,
			PromptProfile: profile.Name + "/describe",
		},
		Warnings: warnings,
	}, nil
}

func (e *VisionEvaluator) profileFor(req schema.EvaluateRequest) (PromptProfile, error) {
	name := req.ProfileOrDefault(e.cfg.DefaultProfile)
	profile, ok := e.cfg.Profiles[name]
	if !ok {
		return PromptProfile{}, schema.BadRequest("options.profile is not configured: %s", name)
	}
	profile.Name = name
	return profile, nil
}

func (e *VisionEvaluator) describeProfileFor(req schema.DescribeRequest) (PromptProfile, error) {
	name := req.ProfileOrDefault(e.cfg.DefaultProfile)
	profile, ok := e.cfg.Profiles[name]
	if !ok {
		return PromptProfile{}, schema.BadRequest("options.profile is not configured: %s", name)
	}
	profile.Name = name
	return profile, nil
}

func screenshotIDs(images []Image) []string {
	ids := make([]string, 0, len(images))
	for _, image := range images {
		ids = append(ids, image.ScreenshotID)
	}
	return ids
}

func normalizeModelResponse(raw string, req schema.EvaluateRequest, profile PromptProfile, metadata schemaMetadata) (schema.EvaluateResponse, []string) {
	response, warnings, ok := decodeModelResponse(raw)
	if !ok {
		return uncertainResponse(req, profile, metadata, "model_output_invalid"), warnings
	}
	response.RequestID = req.RequestID
	response.ModelInfo = schema.ModelInfo{
		Provider:      metadata.provider,
		Model:         metadata.model,
		PromptProfile: profile.Name,
		SchemaVersion: metadata.version,
	}
	normalizedWarnings := append([]string{}, response.Warnings...)
	normalizedWarnings = append(normalizedWarnings, warnings...)
	normalizedWarnings = append(normalizedWarnings, validateAndNormalizeCriteria(req, profile, &response)...)
	if !response.Verdict.IsValid() {
		response.Verdict = schema.VerdictUncertain
		normalizedWarnings = append(normalizedWarnings, "invalid_overall_verdict")
	}
	if hiddenStateInference(response) {
		for index := range response.CriteriaResults {
			response.CriteriaResults[index].Verdict = schema.VerdictUncertain
			response.CriteriaResults[index].Confidence = 0
		}
		response.Verdict = schema.VerdictUncertain
		response.Confidence = 0
		normalizedWarnings = append(normalizedWarnings, "hidden_state_inference_detected")
	}
	if len(normalizedWarnings) > 0 && response.Verdict == schema.VerdictPass {
		response.Verdict = schema.VerdictUncertain
	}
	response.Warnings = dedupeWarnings(normalizedWarnings)
	if response.FollowUpHints == nil {
		response.FollowUpHints = []string{}
	}
	return response, response.Warnings
}

func validateAndNormalizeCriteria(req schema.EvaluateRequest, profile PromptProfile, response *schema.EvaluateResponse) []string {
	warnings := make([]string, 0)
	resultsByID := make(map[string]int, len(response.CriteriaResults))
	for index, result := range response.CriteriaResults {
		resultsByID[result.CriterionID] = index
		if !result.Verdict.IsValid() {
			response.CriteriaResults[index].Verdict = schema.VerdictUncertain
			response.CriteriaResults[index].Confidence = 0
			warnings = append(warnings, "invalid_criterion_verdict:"+result.CriterionID)
		}
		if result.Confidence < 0 || result.Confidence > 1 {
			response.CriteriaResults[index].Verdict = schema.VerdictUncertain
			response.CriteriaResults[index].Confidence = 0
			warnings = append(warnings, "invalid_criterion_confidence:"+result.CriterionID)
		}
		if result.Verdict == schema.VerdictPass && result.Confidence < profile.MinConfidenceForPass {
			response.CriteriaResults[index].Verdict = schema.VerdictUncertain
			warnings = append(warnings, "pass_confidence_below_threshold:"+result.CriterionID)
		}
		if result.Verdict == schema.VerdictFail && result.Confidence < profile.MinConfidenceForFail {
			response.CriteriaResults[index].Verdict = schema.VerdictUncertain
			warnings = append(warnings, "fail_confidence_below_threshold:"+result.CriterionID)
		}
	}
	for _, criterion := range req.Criteria {
		if _, ok := resultsByID[criterion.ID]; ok {
			continue
		}
		response.CriteriaResults = append(response.CriteriaResults, schema.CriterionResult{
			CriterionID:  criterion.ID,
			Verdict:      schema.VerdictUncertain,
			Confidence:   0,
			Explanation:  "model response did not include this criterion",
			Observations: []schema.Observation{},
		})
		warnings = append(warnings, "missing_criterion_result:"+criterion.ID)
	}
	if response.Confidence < 0 || response.Confidence > 1 {
		response.Confidence = 0
		response.Verdict = schema.VerdictUncertain
		warnings = append(warnings, "invalid_overall_confidence")
	}
	return warnings
}

func hiddenStateInference(response schema.EvaluateResponse) bool {
	for _, result := range response.CriteriaResults {
		explanation := strings.ToLower(result.Explanation)
		if strings.Contains(explanation, "task says") ||
			strings.Contains(explanation, "based on the task") ||
			strings.Contains(explanation, "assume") {
			return true
		}
	}
	return false
}

func uncertainResponse(req schema.EvaluateRequest, profile PromptProfile, metadata schemaMetadata, warning string) schema.EvaluateResponse {
	results := make([]schema.CriterionResult, 0, len(req.Criteria))
	for _, criterion := range req.Criteria {
		results = append(results, schema.CriterionResult{
			CriterionID:  criterion.ID,
			Verdict:      schema.VerdictUncertain,
			Confidence:   0,
			Explanation:  "model output could not be accepted",
			Observations: []schema.Observation{},
		})
	}
	return schema.EvaluateResponse{
		RequestID:       req.RequestID,
		Verdict:         schema.VerdictUncertain,
		Confidence:      0,
		CriteriaResults: results,
		FollowUpHints:   []string{"capture clearer evidence or retry with a validated model response"},
		ModelInfo: schema.ModelInfo{
			Provider:      metadata.provider,
			Model:         metadata.model,
			PromptProfile: profile.Name,
			SchemaVersion: metadata.version,
		},
		Warnings: []string{warning},
	}
}

func dedupeWarnings(warnings []string) []string {
	seen := make(map[string]bool, len(warnings))
	result := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning == "" || seen[warning] {
			continue
		}
		seen[warning] = true
		result = append(result, warning)
	}
	return result
}
