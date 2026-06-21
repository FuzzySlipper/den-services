package visualcontract

import (
	"errors"
	"fmt"
	"net/http"
)

const SchemaVersion = "layered-visual-contract/v0.1"

var (
	ErrInvalidContract = errors.New("invalid visual contract") //nolint:gochecknoglobals
	ErrInvalidRequest  = errors.New("invalid request")         //nolint:gochecknoglobals
	ErrNotFound        = errors.New("not found")               //nolint:gochecknoglobals
)

type ServiceError struct {
	err    error
	code   string
	status int
}

func newServiceError(err error, code string, status int) *ServiceError {
	return &ServiceError{err: err, code: code, status: status}
}

func (e *ServiceError) Error() string {
	return e.err.Error()
}

func (e *ServiceError) Unwrap() error {
	return e.err
}

func (e *ServiceError) Code() string {
	return e.code
}

func (e *ServiceError) HTTPStatus() int {
	return e.status
}

func invalidContract(message string) error {
	return newServiceError(fmt.Errorf("%w: %s", ErrInvalidContract, message), "invalid_visual_contract", http.StatusBadRequest)
}

func invalidRequest(message string) error {
	return newServiceError(fmt.Errorf("%w: %s", ErrInvalidRequest, message), "invalid_visual_contract_request", http.StatusBadRequest)
}

type Contract struct {
	Schema      string       `json:"schema"`
	Scene       Scene        `json:"scene"`
	Project     *Project     `json:"project,omitempty"`
	Spaces      []Space      `json:"spaces"`
	Layers      []Layer      `json:"layers"`
	Objects     []Object     `json:"objects"`
	Relations   []Relation   `json:"relations,omitempty"`
	Constraints []Constraint `json:"constraints,omitempty"`
	Evidence    EvidenceSet  `json:"evidence"`
}

type Scene struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Viewport       Viewport `json:"viewport"`
	CoordinateMode string   `json:"coordinate_mode"`
}

type Viewport struct {
	WidthPX  int `json:"width_px"`
	HeightPX int `json:"height_px"`
}

type Project struct {
	ID         string   `json:"id"`
	Vocabulary string   `json:"vocabulary,omitempty"`
	Roles      []string `json:"roles,omitempty"`
}

type Space struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Bounds Bounds `json:"bounds"`
}

type Layer struct {
	ID       string   `json:"id"`
	Z        int      `json:"z"`
	Contains []string `json:"contains,omitempty"`
}

type Object struct {
	ID                  string            `json:"id"`
	Kind                string            `json:"kind"`
	Role                string            `json:"role"`
	DomainRole          *string           `json:"domain_role,omitempty"`
	Parent              string            `json:"parent"`
	Layer               string            `json:"layer"`
	Text                string            `json:"text,omitempty"`
	Bounds              Bounds            `json:"bounds"`
	Children            []string          `json:"children,omitempty"`
	Importance          Importance        `json:"importance"`
	Confidence          float64           `json:"confidence"`
	EvidenceRefs        []string          `json:"evidence_refs,omitempty"`
	Style               map[string]string `json:"style,omitempty"`
	SemanticDescription string            `json:"semantic_description,omitempty"`
}

type Bounds struct {
	Space string       `json:"space,omitempty"`
	X     float64      `json:"x"`
	Y     float64      `json:"y"`
	W     float64      `json:"w"`
	H     float64      `json:"h"`
	PX    *PixelBounds `json:"px,omitempty"`
}

type PixelBounds struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type Relation struct {
	Type        RelationType `json:"type"`
	A           string       `json:"a,omitempty"`
	B           string       `json:"b,omitempty"`
	Items       []string     `json:"items,omitempty"`
	Confidence  float64      `json:"confidence"`
	EvidenceRef string       `json:"evidence_ref,omitempty"`
}

type Constraint struct {
	ID                   string         `json:"id"`
	Type                 ConstraintType `json:"type"`
	Object               string         `json:"object,omitempty"`
	Role                 string         `json:"role,omitempty"`
	DomainRole           string         `json:"domain_role,omitempty"`
	A                    string         `json:"a,omitempty"`
	B                    string         `json:"b,omitempty"`
	Relation             RelationType   `json:"relation,omitempty"`
	Items                []string       `json:"items,omitempty"`
	Edge                 Edge           `json:"edge,omitempty"`
	Importance           Importance     `json:"importance"`
	ToleranceNorm        *float64       `json:"tolerance_norm,omitempty"`
	MinViewportAreaRatio *float64       `json:"min_viewport_area_ratio,omitempty"`
	MaxDeltaNorm         *float64       `json:"max_delta_norm,omitempty"`
}

type EvidenceSet struct {
	SourceType        string           `json:"source_type"`
	SourceRef         string           `json:"source_ref,omitempty"`
	GeneratedBy       string           `json:"generated_by"`
	OverallConfidence float64          `json:"overall_confidence"`
	Records           []EvidenceRecord `json:"records,omitempty"`
}

type EvidenceRecord struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	SourceRef  string   `json:"source_ref,omitempty"`
	ObjectRefs []string `json:"object_refs,omitempty"`
	Confidence float64  `json:"confidence"`
}

type Importance string

const (
	ImportanceCritical Importance = "critical"
	ImportanceMajor    Importance = "major"
	ImportanceMinor    Importance = "minor"
	ImportanceAdvisory Importance = "advisory"
)

func (i Importance) IsValid() bool {
	switch i {
	case ImportanceCritical, ImportanceMajor, ImportanceMinor, ImportanceAdvisory:
		return true
	}
	return false
}

type RelationType string

const (
	RelationLeftOf        RelationType = "left_of"
	RelationRightOf       RelationType = "right_of"
	RelationAbove         RelationType = "above"
	RelationBelow         RelationType = "below"
	RelationInside        RelationType = "inside"
	RelationContains      RelationType = "contains"
	RelationOverlaps      RelationType = "overlaps"
	RelationAlignedLeft   RelationType = "aligned_left"
	RelationAlignedRight  RelationType = "aligned_right"
	RelationAlignedTop    RelationType = "aligned_top"
	RelationAlignedBottom RelationType = "aligned_bottom"
	RelationDominantOver  RelationType = "dominant_over"
)

func (r RelationType) IsValid() bool {
	switch r {
	case RelationLeftOf, RelationRightOf, RelationAbove, RelationBelow, RelationInside,
		RelationContains, RelationOverlaps, RelationAlignedLeft, RelationAlignedRight,
		RelationAlignedTop, RelationAlignedBottom, RelationDominantOver:
		return true
	}
	return false
}

type ConstraintType string

const (
	ConstraintObjectExists     ConstraintType = "object_exists"
	ConstraintLayoutRelation   ConstraintType = "layout_relation"
	ConstraintRelativePosition ConstraintType = "relative_position"
	ConstraintAlignment        ConstraintType = "alignment"
	ConstraintAreaRatio        ConstraintType = "area_ratio"
	ConstraintBoundsTolerance  ConstraintType = "bounds_tolerance"
	ConstraintContainment      ConstraintType = "containment"
)

func (c ConstraintType) IsValid() bool {
	switch c {
	case ConstraintObjectExists, ConstraintLayoutRelation, ConstraintRelativePosition,
		ConstraintAlignment, ConstraintAreaRatio, ConstraintBoundsTolerance, ConstraintContainment:
		return true
	}
	return false
}

type Edge string

const (
	EdgeLeft   Edge = "left"
	EdgeRight  Edge = "right"
	EdgeTop    Edge = "top"
	EdgeBottom Edge = "bottom"
)

func (e Edge) IsValid() bool {
	switch e {
	case EdgeLeft, EdgeRight, EdgeTop, EdgeBottom:
		return true
	}
	return false
}

type Verdict string

const (
	VerdictPass          Verdict = "pass"
	VerdictNeedsRevision Verdict = "needs_revision"
	VerdictFail          Verdict = "fail"
)

type CheckStatus string

const (
	CheckStatusPass CheckStatus = "pass"
	CheckStatusFail CheckStatus = "fail"
	CheckStatusWarn CheckStatus = "warn"
)

type ComparisonReport struct {
	Schema    string            `json:"schema"`
	RunID     string            `json:"run_id,omitempty"`
	Score     float64           `json:"score"`
	Verdict   Verdict           `json:"verdict"`
	Passes    []CheckResult     `json:"passes,omitempty"`
	Failures  []CheckResult     `json:"failures,omitempty"`
	Warnings  []CheckResult     `json:"warnings,omitempty"`
	Groups    []DiagnosticGroup `json:"groups,omitempty"`
	Artifacts ArtifactRefs      `json:"artifacts,omitempty"`
}

type DiagnosticGroup struct {
	Key         string     `json:"key"`
	Severity    Importance `json:"severity"`
	Count       int        `json:"count"`
	Constraints []string   `json:"constraints"`
}

type CheckResult struct {
	Status          CheckStatus        `json:"status"`
	Severity        Importance         `json:"severity"`
	Constraint      string             `json:"constraint"`
	Message         string             `json:"message"`
	Expected        string             `json:"expected,omitempty"`
	Actual          string             `json:"actual,omitempty"`
	MatchConfidence float64            `json:"match_confidence"`
	MatchStrategy   string             `json:"match_strategy"`
	Evidence        map[string]string  `json:"evidence,omitempty"`
	InvolvedObjects []string           `json:"involved_objects,omitempty"`
	Measured        map[string]float64 `json:"measured,omitempty"`
	ReferenceBounds *Bounds            `json:"reference_bounds,omitempty"`
	CandidateBounds *Bounds            `json:"candidate_bounds,omitempty"`
	RepairHint      string             `json:"repair_hint,omitempty"`
}

type ArtifactRefs struct {
	ReferenceOverlay  string `json:"reference_overlay,omitempty"`
	CandidateOverlay  string `json:"candidate_overlay,omitempty"`
	DiffOverlay       string `json:"diff_overlay,omitempty"`
	ReferenceContract string `json:"reference_contract,omitempty"`
	CandidateContract string `json:"candidate_contract,omitempty"`
	Report            string `json:"report,omitempty"`
}
