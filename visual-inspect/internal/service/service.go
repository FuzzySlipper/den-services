package service

import (
	"context"
	"log/slog"
	"strings"

	"den-services/visual-inspect/internal/artifacts"
	"den-services/visual-inspect/internal/config"
	"den-services/visual-inspect/internal/evaluator"
	"den-services/visual-inspect/internal/schema"
)

type ArtifactFetcher interface {
	Fetch(ctx context.Context, ref schema.ScreenshotRef) (artifacts.Image, error)
}

type Service struct {
	cfg       *config.Config
	fetcher   ArtifactFetcher
	evaluator evaluator.Evaluator
	logger    *slog.Logger
}

func NewService(cfg *config.Config, fetcher ArtifactFetcher, evaluator evaluator.Evaluator, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:       cfg,
		fetcher:   fetcher,
		evaluator: evaluator,
		logger:    logger,
	}
}

func (s *Service) Evaluate(ctx context.Context, req schema.EvaluateRequest) (schema.EvaluateResponse, error) {
	if err := s.validateRequest(req); err != nil {
		return schema.EvaluateResponse{}, err
	}
	images := make([]artifacts.Image, 0, len(req.Screenshots))
	for _, screenshot := range req.Screenshots {
		image, err := s.fetcher.Fetch(ctx, screenshot)
		if err != nil {
			return schema.EvaluateResponse{}, err
		}
		s.logImagePreflight(req.RequestID, image)
		images = append(images, image)
	}
	response, err := s.evaluator.Evaluate(ctx, req, toEvaluatorImages(images))
	if err != nil {
		return schema.EvaluateResponse{}, err
	}
	s.logger.Info("visual_inspect_result",
		"request_id", req.RequestID,
		"verdict", response.Verdict,
		"confidence", response.Confidence,
		"model", response.ModelInfo.Model,
		"prompt_profile", response.ModelInfo.PromptProfile,
		"schema_version", response.ModelInfo.SchemaVersion,
	)
	return response, nil
}

func (s *Service) validateRequest(req schema.EvaluateRequest) error {
	if len(req.Criteria) == 0 {
		return schema.BadRequest("criteria is required")
	}
	for index, criterion := range req.Criteria {
		if !schema.IsValidIdentifier(criterion.ID) {
			return schema.BadRequest("criteria.%d.id is required and must be an identifier", index)
		}
		if strings.TrimSpace(criterion.Statement) == "" {
			return schema.BadRequest("criteria.%d.statement is required", index)
		}
		if criterion.Weight != nil && *criterion.Weight <= 0 {
			return schema.BadRequest("criteria.%d.weight must be positive", index)
		}
	}
	if len(req.Screenshots) == 0 {
		return schema.BadRequest("screenshots is required")
	}
	if len(req.Screenshots) > s.cfg.Artifacts.MaxImages {
		return schema.PayloadTooLarge("screenshots exceeds artifacts.max_images")
	}
	for index, screenshot := range req.Screenshots {
		if !schema.IsValidIdentifier(screenshot.ID) {
			return schema.BadRequest("screenshots.%d.id is required and must be an identifier", index)
		}
		if strings.TrimSpace(screenshot.Ref) == "" {
			return schema.BadRequest("screenshots.%d.ref is required", index)
		}
		if !supportedMimeType(screenshot.MimeType) {
			return schema.BadRequest("screenshots.%d.mime_type is unsupported: %s", index, screenshot.MimeType)
		}
	}
	if req.Options != nil {
		if req.Options.MinConfidenceForPass != nil && invalidConfidence(*req.Options.MinConfidenceForPass) {
			return schema.BadRequest("options.min_confidence_for_pass must be between 0 and 1")
		}
		if req.Options.MinConfidenceForFail != nil && invalidConfidence(*req.Options.MinConfidenceForFail) {
			return schema.BadRequest("options.min_confidence_for_fail must be between 0 and 1")
		}
		if req.Options.Profile != "" {
			if _, ok := s.cfg.Prompts.Profiles[strings.TrimSpace(req.Options.Profile)]; !ok {
				return schema.BadRequest("options.profile is not configured: %s", req.Options.Profile)
			}
		}
	}
	return nil
}

func (s *Service) logImagePreflight(requestID string, image artifacts.Image) {
	s.logger.Info("visual_inspect_image_preflight",
		"request_id", requestID,
		"screenshot_id", image.ScreenshotID,
		"ref_scheme", image.RefScheme,
		"mime_type", image.MimeType,
		"byte_count", image.ByteCount,
		"width", image.Width,
		"height", image.Height,
		"sha256", image.SHA256,
		"sensitive", image.Sensitive,
	)
}

func supportedMimeType(mimeType string) bool {
	switch mimeType {
	case artifacts.MimePNG, artifacts.MimeJPEG:
		return true
	}
	return false
}

func invalidConfidence(value float64) bool {
	return value < 0 || value > 1
}

func toEvaluatorImages(images []artifacts.Image) []evaluator.Image {
	result := make([]evaluator.Image, 0, len(images))
	for _, image := range images {
		result = append(result, evaluator.Image{
			ScreenshotID: image.ScreenshotID,
			MimeType:     image.MimeType,
			Bytes:        image.Bytes,
			Width:        image.Width,
			Height:       image.Height,
			SHA256:       image.SHA256,
			Sensitive:    image.Sensitive,
		})
	}
	return result
}
