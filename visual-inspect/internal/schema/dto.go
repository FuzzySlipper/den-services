package schema

import (
	"strings"
	"unicode"
)

type Verdict string

const (
	VerdictPass      Verdict = "pass"
	VerdictFail      Verdict = "fail"
	VerdictUncertain Verdict = "uncertain"
)

func (v Verdict) IsValid() bool {
	switch v {
	case VerdictPass, VerdictFail, VerdictUncertain:
		return true
	}
	return false
}

type EvaluateRequest struct {
	RequestID   string             `json:"request_id,omitempty"`
	TaskRef     *TaskRef           `json:"task_ref,omitempty"`
	ReviewRef   *ReviewRef         `json:"review_ref,omitempty"`
	Criteria    []CriterionRequest `json:"criteria"`
	Screenshots []ScreenshotRef    `json:"screenshots"`
	Context     *EvaluateContext   `json:"context,omitempty"`
	Options     *EvaluateOptions   `json:"options,omitempty"`
}

type TaskRef struct {
	ProjectID string `json:"project_id"`
	TaskID    int64  `json:"task_id"`
}

type ReviewRef struct {
	ReviewRoundID int64 `json:"review_round_id,omitempty"`
	FindingID     int64 `json:"finding_id,omitempty"`
}

type CriterionRequest struct {
	ID        string   `json:"id"`
	Statement string   `json:"statement"`
	Required  bool     `json:"required"`
	Weight    *float64 `json:"weight,omitempty"`
}

type ScreenshotRef struct {
	ID          string `json:"id"`
	Ref         string `json:"ref"`
	MimeType    string `json:"mime_type"`
	Description string `json:"description,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
}

type EvaluateContext struct {
	TaskTitle         string `json:"task_title,omitempty"`
	AcceptanceSummary string `json:"acceptance_summary,omitempty"`
	UISurface         string `json:"ui_surface,omitempty"`
}

type EvaluateOptions struct {
	Profile              string   `json:"profile,omitempty"`
	MinConfidenceForPass *float64 `json:"min_confidence_for_pass,omitempty"`
	MinConfidenceForFail *float64 `json:"min_confidence_for_fail,omitempty"`
	ReturnRegions        bool     `json:"return_regions,omitempty"`
}

type EvaluateResponse struct {
	RequestID       string            `json:"request_id,omitempty"`
	Verdict         Verdict           `json:"verdict"`
	Confidence      float64           `json:"confidence"`
	CriteriaResults []CriterionResult `json:"criteria_results"`
	FollowUpHints   []string          `json:"follow_up_hints"`
	ModelInfo       ModelInfo         `json:"model_info"`
	Warnings        []string          `json:"warnings"`
}

type CriterionResult struct {
	CriterionID  string        `json:"criterion_id"`
	Verdict      Verdict       `json:"verdict"`
	Confidence   float64       `json:"confidence"`
	Explanation  string        `json:"explanation"`
	Observations []Observation `json:"observations"`
}

type Observation struct {
	ScreenshotID string       `json:"screenshot_id"`
	Label        string       `json:"label"`
	Region       *ImageRegion `json:"region,omitempty"`
	Confidence   float64      `json:"confidence"`
}

type ImageRegion struct {
	X               int    `json:"x"`
	Y               int    `json:"y"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	CoordinateSpace string `json:"coordinate_space"`
}

type ModelInfo struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	PromptProfile string `json:"prompt_profile"`
	SchemaVersion string `json:"schema_version,omitempty"`
}

func (r EvaluateRequest) ProfileOrDefault(defaultProfile string) string {
	if r.Options == nil || strings.TrimSpace(r.Options.Profile) == "" {
		return defaultProfile
	}
	return strings.TrimSpace(r.Options.Profile)
}

func IsValidIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for index, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if index > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return false
	}
	return true
}
