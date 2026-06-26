package evaluator

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"den-services/visual-inspect/internal/schema"
)

type renderedPrompt struct {
	messages []ChatMessage
}

type promptContext struct {
	RequestID   string                    `json:"request_id,omitempty"`
	TaskRef     *schema.TaskRef           `json:"task_ref,omitempty"`
	ReviewRef   *schema.ReviewRef         `json:"review_ref,omitempty"`
	Criteria    []schema.CriterionRequest `json:"criteria"`
	Screenshots []screenshotPrompt        `json:"screenshots"`
	Context     *schema.EvaluateContext   `json:"context,omitempty"`
	Options     *schema.EvaluateOptions   `json:"options,omitempty"`
}

type screenshotPrompt struct {
	ID          string `json:"id"`
	MimeType    string `json:"mime_type"`
	Description string `json:"description,omitempty"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SHA256      string `json:"sha256"`
	Sensitive   bool   `json:"sensitive"`
}

func renderPrompt(profile PromptProfile, req schema.EvaluateRequest, images []Image) (renderedPrompt, error) {
	systemPrompt, err := readPromptFile("system prompt", profile.SystemPromptFile)
	if err != nil {
		return renderedPrompt{}, err
	}
	developerPrompt, err := readPromptFile("developer prompt", profile.DeveloperPromptFile)
	if err != nil {
		return renderedPrompt{}, err
	}
	contextJSON, err := json.MarshalIndent(toPromptContext(req, images), "", "  ")
	if err != nil {
		return renderedPrompt{}, fmt.Errorf("rendering prompt context: %w", err)
	}
	userParts := []ContentPart{{
		Type: "text",
		Text: "Evaluate only the attached screenshots against the listed criteria. Return JSON only.\n\n" + string(contextJSON),
	}}
	for _, image := range images {
		userParts = append(userParts, ContentPart{
			Type:     "image",
			MimeType: image.MimeType,
			Data:     image.Bytes,
		})
	}
	return renderedPrompt{
		messages: []ChatMessage{
			{Role: "system", Content: []ContentPart{{Type: "text", Text: systemPrompt}}},
			{Role: "developer", Content: []ContentPart{{Type: "text", Text: developerPrompt}}},
			{Role: "user", Content: userParts},
		},
	}, nil
}

func renderDescribePrompt(profile PromptProfile, req schema.DescribeRequest, images []Image) (renderedPrompt, error) {
	contextJSON, err := json.MarshalIndent(toDescribePromptContext(req, images), "", "  ")
	if err != nil {
		return renderedPrompt{}, fmt.Errorf("rendering describe prompt context: %w", err)
	}
	detail := "concise"
	if req.Options != nil && strings.TrimSpace(req.Options.Detail) != "" {
		detail = strings.TrimSpace(req.Options.Detail)
	}
	focus := strings.TrimSpace(req.Prompt)
	if focus == "" {
		focus = "Describe the visible contents of the attached screenshot images for an agent that cannot inspect images directly."
	}
	userParts := []ContentPart{{
		Type: "text",
		Text: "Describe only what is visible in the attached screenshots. Do not score criteria, do not return verdicts, and do not claim hidden state. If relevant details are ambiguous, say so. Detail level: " + detail + ".\n\nFocus: " + focus + "\n\nImage context:\n" + string(contextJSON),
	}}
	for _, image := range images {
		userParts = append(userParts, ContentPart{
			Type:     "image",
			MimeType: image.MimeType,
			Data:     image.Bytes,
		})
	}
	return renderedPrompt{
		messages: []ChatMessage{
			{Role: "system", Content: []ContentPart{{Type: "text", Text: "You describe images for Den agents. Return plain text only."}}},
			{Role: "user", Content: userParts},
		},
	}, nil
}

func readPromptFile(label string, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s file path is required", label)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s file %s: %w", label, path, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("%s file %s is empty", label, path)
	}
	return value, nil
}

func toPromptContext(req schema.EvaluateRequest, images []Image) promptContext {
	screenshots := make([]screenshotPrompt, 0, len(images))
	for _, image := range images {
		screenshots = append(screenshots, screenshotPrompt{
			ID:        image.ScreenshotID,
			MimeType:  image.MimeType,
			Width:     image.Width,
			Height:    image.Height,
			SHA256:    image.SHA256,
			Sensitive: image.Sensitive,
		})
	}
	for index, screenshot := range req.Screenshots {
		if index >= len(screenshots) {
			break
		}
		screenshots[index].Description = screenshot.Description
	}
	return promptContext{
		RequestID:   req.RequestID,
		TaskRef:     req.TaskRef,
		ReviewRef:   req.ReviewRef,
		Criteria:    req.Criteria,
		Screenshots: screenshots,
		Context:     req.Context,
		Options:     req.Options,
	}
}

func toDescribePromptContext(req schema.DescribeRequest, images []Image) promptContext {
	screenshots := make([]screenshotPrompt, 0, len(images))
	for _, image := range images {
		screenshots = append(screenshots, screenshotPrompt{
			ID:        image.ScreenshotID,
			MimeType:  image.MimeType,
			Width:     image.Width,
			Height:    image.Height,
			SHA256:    image.SHA256,
			Sensitive: image.Sensitive,
		})
	}
	for index, screenshot := range req.Screenshots {
		if index >= len(screenshots) {
			break
		}
		screenshots[index].Description = screenshot.Description
	}
	return promptContext{
		RequestID:   req.RequestID,
		TaskRef:     req.TaskRef,
		ReviewRef:   req.ReviewRef,
		Screenshots: screenshots,
		Context:     req.Context,
	}
}
