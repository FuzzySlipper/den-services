package evaluator

import (
	"context"

	"den-services/visual-inspect/internal/schema"
)

type Image struct {
	ScreenshotID string
	MimeType     string
	Bytes        []byte
	Width        int
	Height       int
	SHA256       string
	Sensitive    bool
}

type Evaluator interface {
	Evaluate(ctx context.Context, req schema.EvaluateRequest, images []Image) (schema.EvaluateResponse, error)
}

type Config struct {
	Provider       string
	Model          string
	DefaultProfile string
}

type PreflightEvaluator struct {
	cfg Config
}

func NewPreflightEvaluator(cfg Config) *PreflightEvaluator {
	return &PreflightEvaluator{cfg: cfg}
}

func (e *PreflightEvaluator) Evaluate(_ context.Context, req schema.EvaluateRequest, _ []Image) (schema.EvaluateResponse, error) {
	profile := req.ProfileOrDefault(e.cfg.DefaultProfile)
	results := make([]schema.CriterionResult, 0, len(req.Criteria))
	for _, criterion := range req.Criteria {
		results = append(results, schema.CriterionResult{
			CriterionID:  criterion.ID,
			Verdict:      schema.VerdictUncertain,
			Confidence:   0,
			Explanation:  "visual evaluator is not implemented yet; screenshot inputs passed validation",
			Observations: []schema.Observation{},
		})
	}
	return schema.EvaluateResponse{
		RequestID:       req.RequestID,
		Verdict:         schema.VerdictUncertain,
		Confidence:      0,
		CriteriaResults: results,
		FollowUpHints:   []string{"visual-inspect currently performs screenshot preflight only; LLM evaluation is implemented in a later task"},
		ModelInfo: schema.ModelInfo{
			Provider:      e.cfg.Provider,
			Model:         e.cfg.Model,
			PromptProfile: profile,
		},
		Warnings: []string{"evaluator_not_implemented"},
	}, nil
}
