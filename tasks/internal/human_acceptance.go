package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type HumanAcceptanceVerdict string

const HumanAcceptanceVerdictLooksGood HumanAcceptanceVerdict = "looks_good"

type HumanAcceptanceLifecycleEffect string

const (
	HumanAcceptanceRecordOnly            HumanAcceptanceLifecycleEffect = "record_only"
	HumanAcceptanceCompleteTask          HumanAcceptanceLifecycleEffect = "complete_task"
	HumanAcceptanceCompleteTaskAndParent HumanAcceptanceLifecycleEffect = "complete_task_and_parent"
)

var (
	ErrMissingReviewerIdentity          = errors.New("reviewer_identity is required")                                      //nolint:gochecknoglobals
	ErrMissingIdempotencyKey            = errors.New("idempotency_key is required")                                        //nolint:gochecknoglobals
	ErrInvalidAcceptanceVerdict         = errors.New("verdict must be looks_good")                                         //nolint:gochecknoglobals
	ErrInvalidAcceptanceEffect          = errors.New("invalid lifecycle_effect")                                           //nolint:gochecknoglobals
	ErrAcceptanceTaskChanged            = errors.New("task changed after the caller's readback")                           //nolint:gochecknoglobals
	ErrAcceptanceReconciliationRequired = errors.New("reviewed_revision requires expected_task_updated_at reconciliation") //nolint:gochecknoglobals
	ErrAcceptanceIdempotencyConflict    = errors.New("idempotency key was already used with other facts")                  //nolint:gochecknoglobals
	ErrAcceptanceTaskCancelled          = errors.New("cancelled task cannot be accepted")                                  //nolint:gochecknoglobals
	ErrAcceptanceParentMissing          = errors.New("task has no parent to complete")                                     //nolint:gochecknoglobals
	ErrAcceptanceParentIneligible       = errors.New("parent has unfinished children or dependencies")                     //nolint:gochecknoglobals
)

type HumanAcceptanceReview struct {
	ID                  int64
	TaskID              int64
	ProjectID           string
	IdempotencyKey      string
	RequestFingerprint  string
	ReviewerIdentity    string
	Verdict             HumanAcceptanceVerdict
	Rationale           string
	ReviewedRevision    string
	ReviewedBuild       string
	ReviewedEnvironment string
	EvidenceLinks       []string
	LifecycleEffect     HumanAcceptanceLifecycleEffect
	NoteMarkdown        string
	TaskStatusBefore    string
	TaskStatusAfter     string
	ParentTaskID        *int64
	ParentStatusBefore  string
	ParentStatusAfter   string
	CreatedAt           time.Time
}

type HumanAcceptanceMutation struct {
	Review           *HumanAcceptanceReview
	Task             *Task
	Parent           *Task
	ChangedTaskIDs   []int64
	UnchangedTaskIDs []int64
}

type humanAcceptanceFacts struct {
	ReviewerIdentity    string                         `json:"reviewer_identity"`
	Verdict             HumanAcceptanceVerdict         `json:"verdict"`
	Rationale           string                         `json:"rationale"`
	ReviewedRevision    string                         `json:"reviewed_revision,omitempty"`
	ReviewedBuild       string                         `json:"reviewed_build,omitempty"`
	ReviewedEnvironment string                         `json:"reviewed_environment,omitempty"`
	EvidenceLinks       []string                       `json:"evidence_links,omitempty"`
	LifecycleEffect     HumanAcceptanceLifecycleEffect `json:"lifecycle_effect"`
}

func normalizeHumanAcceptance(req RecordHumanAcceptanceRequest) (humanAcceptanceFacts, string, error) {
	facts := humanAcceptanceFacts{
		ReviewerIdentity:    strings.TrimSpace(req.ReviewerIdentity),
		Verdict:             req.Verdict,
		Rationale:           strings.TrimSpace(req.Rationale),
		ReviewedRevision:    strings.TrimSpace(req.ReviewedRevision),
		ReviewedBuild:       strings.TrimSpace(req.ReviewedBuild),
		ReviewedEnvironment: strings.TrimSpace(req.ReviewedEnvironment),
		EvidenceLinks:       normalizeEvidenceLinks(req.EvidenceLinks),
		LifecycleEffect:     req.LifecycleEffect,
	}
	if facts.ReviewerIdentity == "" {
		return humanAcceptanceFacts{}, "", ErrMissingReviewerIdentity
	}
	if facts.Verdict == "" {
		facts.Verdict = HumanAcceptanceVerdictLooksGood
	}
	if facts.Verdict != HumanAcceptanceVerdictLooksGood {
		return humanAcceptanceFacts{}, "", ErrInvalidAcceptanceVerdict
	}
	if facts.Rationale == "" {
		facts.Rationale = "Looks good."
	}
	if facts.LifecycleEffect == "" {
		facts.LifecycleEffect = HumanAcceptanceRecordOnly
	}
	switch facts.LifecycleEffect {
	case HumanAcceptanceRecordOnly, HumanAcceptanceCompleteTask, HumanAcceptanceCompleteTaskAndParent:
	default:
		return humanAcceptanceFacts{}, "", ErrInvalidAcceptanceEffect
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return humanAcceptanceFacts{}, "", fmt.Errorf("encoding human acceptance facts: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return facts, hex.EncodeToString(digest[:]), nil
}

func normalizeEvidenceLinks(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func humanAcceptanceNote(facts humanAcceptanceFacts) string {
	lines := []string{
		"## Human acceptance: Looks good",
		"",
		"- Reviewer: " + facts.ReviewerIdentity,
		"- Observation: " + facts.Rationale,
		"- Lifecycle effect: " + string(facts.LifecycleEffect),
	}
	if facts.ReviewedRevision != "" {
		lines = append(lines, "- Reviewed revision: "+facts.ReviewedRevision)
	}
	if facts.ReviewedBuild != "" {
		lines = append(lines, "- Reviewed build: "+facts.ReviewedBuild)
	}
	if facts.ReviewedEnvironment != "" {
		lines = append(lines, "- Environment: "+facts.ReviewedEnvironment)
	}
	if len(facts.EvidenceLinks) > 0 {
		lines = append(lines, "- Evidence: "+strings.Join(facts.EvidenceLinks, ", "))
	}
	return strings.Join(lines, "\n")
}
