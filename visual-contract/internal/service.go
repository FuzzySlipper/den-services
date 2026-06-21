package visualcontract

import (
	"context"
	"fmt"
	"math"
)

type Service struct {
	artifactBaseURL string
}

func NewService(artifactBaseURL string) (*Service, error) {
	if artifactBaseURL == "" {
		return nil, invalidRequest("artifact base url is required")
	}
	return &Service{artifactBaseURL: artifactBaseURL}, nil
}

func (s *Service) Validate(_ context.Context, contract *Contract) (*ValidationResponse, error) {
	if err := ValidateContract(contract); err != nil {
		return nil, err
	}
	return &ValidationResponse{
		Schema:  contract.Schema,
		Valid:   true,
		SceneID: contract.Scene.ID,
		Counts: ContractCounts{
			Spaces:      len(contract.Spaces),
			Layers:      len(contract.Layers),
			Objects:     len(contract.Objects),
			Relations:   len(contract.Relations),
			Constraints: len(contract.Constraints),
			Evidence:    len(contract.Evidence.Records),
		},
	}, nil
}

func (s *Service) Compare(_ context.Context, reference *Contract, candidate *Contract) (*ComparisonReport, error) {
	if err := ValidateContract(reference); err != nil {
		return nil, fmt.Errorf("reference: %w", err)
	}
	if err := ValidateContract(candidate); err != nil {
		return nil, fmt.Errorf("candidate: %w", err)
	}
	report := compareContracts(reference, candidate)
	report.Artifacts = ArtifactRefs{
		ReferenceOverlay: s.artifactBaseURL + "/reference.overlay.svg",
		CandidateOverlay: s.artifactBaseURL + "/candidate.overlay.svg",
		DiffOverlay:      s.artifactBaseURL + "/diff.overlay.svg",
	}
	return report, nil
}

func (s *Service) Overlays(ctx context.Context, reference *Contract, candidate *Contract, report *ComparisonReport) (*OverlayResponse, error) {
	if err := ValidateContract(reference); err != nil {
		return nil, fmt.Errorf("reference: %w", err)
	}
	if candidate != nil {
		if err := ValidateContract(candidate); err != nil {
			return nil, fmt.Errorf("candidate: %w", err)
		}
	}
	if report == nil && candidate != nil {
		generated, err := s.Compare(ctx, reference, candidate)
		if err != nil {
			return nil, err
		}
		report = generated
	}
	referenceOverlay, err := RenderContractOverlay(reference, nil)
	if err != nil {
		return nil, fmt.Errorf("rendering reference overlay: %w", err)
	}
	response := &OverlayResponse{ReferenceSVG: referenceOverlay}
	if candidate != nil {
		candidateOverlay, err := RenderContractOverlay(candidate, report)
		if err != nil {
			return nil, fmt.Errorf("rendering candidate overlay: %w", err)
		}
		response.CandidateSVG = candidateOverlay
	}
	if candidate != nil && report != nil {
		diffOverlay, err := RenderDiffOverlay(candidate, report)
		if err != nil {
			return nil, fmt.Errorf("rendering diff overlay: %w", err)
		}
		response.DiffSVG = diffOverlay
	}
	return response, nil
}

func (s *Service) FromWebEvidence(_ context.Context, evidence *WebEvidence) (*Contract, error) {
	contract, err := BuildContractFromWebEvidence(evidence)
	if err != nil {
		return nil, err
	}
	if err := ValidateContract(contract); err != nil {
		return nil, err
	}
	return contract, nil
}

func compareContracts(reference *Contract, candidate *Contract) *ComparisonReport {
	candidateIndex, _ := buildContractIndex(candidate)
	results := make([]CheckResult, 0, len(reference.Constraints))
	for _, constraint := range reference.Constraints {
		results = append(results, evaluateConstraint(reference, candidateIndex, constraint))
	}

	report := &ComparisonReport{
		Schema:  SchemaVersion,
		Verdict: VerdictPass,
	}
	totalWeight := 0.0
	lostWeight := 0.0
	for _, result := range results {
		weight := importanceWeight(result.Severity)
		totalWeight += weight
		switch result.Status {
		case CheckStatusPass:
			report.Passes = append(report.Passes, result)
		case CheckStatusWarn:
			report.Warnings = append(report.Warnings, result)
			lostWeight += weight * 0.5
		case CheckStatusFail:
			report.Failures = append(report.Failures, result)
			lostWeight += weight
			if result.Severity == ImportanceCritical {
				report.Verdict = VerdictFail
			}
		}
	}
	if totalWeight == 0 {
		report.Score = 1
		return report
	}
	report.Score = math.Max(0, 1-lostWeight/totalWeight)
	if report.Verdict != VerdictFail && len(report.Failures) > 0 {
		report.Verdict = VerdictNeedsRevision
	}
	if report.Verdict == VerdictPass && len(report.Warnings) > 0 {
		report.Verdict = VerdictNeedsRevision
	}
	return report
}

func evaluateConstraint(reference *Contract, candidateIndex *contractIndex, constraint Constraint) CheckResult {
	switch constraint.Type {
	case ConstraintObjectExists:
		return checkObjectExists(candidateIndex, constraint)
	case ConstraintLayoutRelation, ConstraintRelativePosition:
		return checkRelation(candidateIndex, constraint)
	case ConstraintAlignment:
		return checkAlignment(candidateIndex, constraint)
	case ConstraintAreaRatio:
		return checkAreaRatio(candidateIndex, constraint)
	case ConstraintBoundsTolerance:
		return checkBoundsTolerance(reference, candidateIndex, constraint)
	case ConstraintContainment:
		return checkContainment(candidateIndex, constraint)
	default:
		return failResult(constraint, "unsupported constraint type", string(constraint.Type), "unsupported", 0, "unsupported")
	}
}

func checkObjectExists(index *contractIndex, constraint Constraint) CheckResult {
	if constraint.Object != "" {
		if index.hasObject(constraint.Object) {
			return passResult(constraint, fmt.Sprintf("%s exists", constraint.Object), 1, "exact_id")
		}
		return failResult(constraint, "required object is missing", constraint.Object, "missing", 0, "exact_id_missing")
	}
	for _, object := range index.objects {
		if constraint.Role != "" && object.Role != constraint.Role {
			continue
		}
		if constraint.DomainRole != "" && (object.DomainRole == nil || *object.DomainRole != constraint.DomainRole) {
			continue
		}
		return passResult(constraint, "matching object exists", object.Confidence, "role_domain_role")
	}
	return failResult(constraint, "no matching object exists", "matching role/domain_role", "missing", 0, "role_domain_role_missing")
}

func checkRelation(index *contractIndex, constraint Constraint) CheckResult {
	a, aOK := index.objects[constraint.A]
	b, bOK := index.objects[constraint.B]
	if !aOK || !bOK {
		return failResult(constraint, "relation object is missing", relationExpected(constraint), "missing object", 0, "exact_id_missing")
	}
	matchConfidence := minFloat(a.Confidence, b.Confidence)
	if relationHolds(a.Bounds, b.Bounds, constraint.Relation, toleranceOrDefault(constraint.ToleranceNorm, 0.01)) {
		return passResult(constraint, fmt.Sprintf("%s is %s %s", constraint.A, constraint.Relation, constraint.B), matchConfidence, "exact_id")
	}
	actual := describeRelativePosition(a.Bounds, b.Bounds)
	return failResult(constraint, "layout relation does not hold", relationExpected(constraint), actual, matchConfidence, "exact_id")
}

func checkAlignment(index *contractIndex, constraint Constraint) CheckResult {
	if len(constraint.Items) < 2 {
		return failResult(constraint, "alignment requires at least two items", "two or more items", "too few items", 0, "insufficient_items")
	}
	first, ok := index.objects[constraint.Items[0]]
	if !ok {
		return failResult(constraint, "alignment item is missing", constraint.Items[0], "missing", 0, "exact_id_missing")
	}
	tolerance := toleranceOrDefault(constraint.ToleranceNorm, 0.02)
	matchConfidence := first.Confidence
	for _, itemID := range constraint.Items[1:] {
		item, exists := index.objects[itemID]
		if !exists {
			return failResult(constraint, "alignment item is missing", itemID, "missing", 0, "exact_id_missing")
		}
		matchConfidence = minFloat(matchConfidence, item.Confidence)
		if math.Abs(edgeValue(first.Bounds, constraint.Edge)-edgeValue(item.Bounds, constraint.Edge)) > tolerance {
			return failResult(constraint, "items are not aligned", fmt.Sprintf("%s edges aligned", constraint.Edge), fmt.Sprintf("%s differs from %s", itemID, constraint.Items[0]), matchConfidence, "exact_id")
		}
	}
	return passResult(constraint, "items are aligned", matchConfidence, "exact_id")
}

func checkAreaRatio(index *contractIndex, constraint Constraint) CheckResult {
	object, ok := index.objects[constraint.Object]
	if !ok {
		return failResult(constraint, "area-ratio object is missing", constraint.Object, "missing", 0, "exact_id_missing")
	}
	minRatio := 0.0
	if constraint.MinViewportAreaRatio != nil {
		minRatio = *constraint.MinViewportAreaRatio
	}
	ratio := object.Bounds.W * object.Bounds.H
	if ratio >= minRatio {
		return passResult(constraint, fmt.Sprintf("%s area ratio %.3f >= %.3f", constraint.Object, ratio, minRatio), object.Confidence, "exact_id")
	}
	return failResult(constraint, "object area ratio is too small", fmt.Sprintf("area >= %.3f", minRatio), fmt.Sprintf("area %.3f", ratio), object.Confidence, "exact_id")
}

func checkBoundsTolerance(reference *Contract, index *contractIndex, constraint Constraint) CheckResult {
	referenceObject, ok := findObject(reference, constraint.Object)
	if !ok {
		return failResult(constraint, "reference bounds object is missing", constraint.Object, "missing reference object", 0, "exact_id_missing")
	}
	candidateObject, ok := index.objects[constraint.Object]
	if !ok {
		return failResult(constraint, "candidate bounds object is missing", constraint.Object, "missing", 0, "exact_id_missing")
	}
	maxDelta := toleranceOrDefault(constraint.MaxDeltaNorm, 0.03)
	delta := boundsDelta(referenceObject.Bounds, candidateObject.Bounds)
	if delta <= maxDelta {
		return passResult(constraint, fmt.Sprintf("bounds delta %.3f <= %.3f", delta, maxDelta), candidateObject.Confidence, "exact_id")
	}
	return failResult(constraint, "bounds changed beyond tolerance", fmt.Sprintf("delta <= %.3f", maxDelta), fmt.Sprintf("delta %.3f", delta), candidateObject.Confidence, "exact_id")
}

func checkContainment(index *contractIndex, constraint Constraint) CheckResult {
	object, ok := index.objects[constraint.Object]
	if !ok {
		return failResult(constraint, "contained object is missing", constraint.Object, "missing", 0, "exact_id_missing")
	}
	parentBounds, parentConfidence, ok := containmentParent(index, object.Parent)
	if !ok {
		return failResult(constraint, "object parent is missing", object.Parent, "missing", 0, "exact_id_missing")
	}
	matchConfidence := minFloat(object.Confidence, parentConfidence)
	if relationHolds(object.Bounds, parentBounds, RelationInside, toleranceOrDefault(constraint.ToleranceNorm, 0.01)) {
		return passResult(constraint, fmt.Sprintf("%s is inside %s", object.ID, object.Parent), matchConfidence, "exact_id")
	}
	return failResult(constraint, "object escapes parent bounds", fmt.Sprintf("%s inside %s", object.ID, object.Parent), "escapes parent", matchConfidence, "exact_id")
}

func containmentParent(index *contractIndex, parentID string) (Bounds, float64, bool) {
	if parent, ok := index.objects[parentID]; ok {
		return parent.Bounds, parent.Confidence, true
	}
	if parent, ok := index.spaces[parentID]; ok {
		return parent.Bounds, 1, true
	}
	return Bounds{}, 0, false
}

func relationHolds(a Bounds, b Bounds, relation RelationType, tolerance float64) bool {
	switch relation {
	case RelationLeftOf:
		return a.X+a.W <= b.X+tolerance
	case RelationRightOf:
		return a.X >= b.X+b.W-tolerance
	case RelationAbove:
		return a.Y+a.H <= b.Y+tolerance
	case RelationBelow:
		return a.Y >= b.Y+b.H-tolerance
	case RelationInside:
		return a.X >= b.X-tolerance && a.Y >= b.Y-tolerance && a.X+a.W <= b.X+b.W+tolerance && a.Y+a.H <= b.Y+b.H+tolerance
	case RelationContains:
		return relationHolds(b, a, RelationInside, tolerance)
	case RelationOverlaps:
		return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
	case RelationDominantOver:
		return a.W*a.H > b.W*b.H*1.25
	default:
		return false
	}
}

func describeRelativePosition(a Bounds, b Bounds) string {
	switch {
	case relationHolds(a, b, RelationRightOf, 0.01):
		return string(RelationRightOf)
	case relationHolds(a, b, RelationLeftOf, 0.01):
		return string(RelationLeftOf)
	case relationHolds(a, b, RelationBelow, 0.01):
		return string(RelationBelow)
	case relationHolds(a, b, RelationAbove, 0.01):
		return string(RelationAbove)
	case relationHolds(a, b, RelationOverlaps, 0.01):
		return string(RelationOverlaps)
	default:
		return "not related"
	}
}

func passResult(constraint Constraint, message string, matchConfidence float64, matchStrategy string) CheckResult {
	return CheckResult{
		Status:          CheckStatusPass,
		Severity:        constraint.Importance,
		Constraint:      constraint.ID,
		Message:         message,
		MatchConfidence: matchConfidence,
		MatchStrategy:   matchStrategy,
	}
}

func failResult(constraint Constraint, message string, expected string, actual string, matchConfidence float64, matchStrategy string) CheckResult {
	status := CheckStatusFail
	if constraint.Importance == ImportanceMinor || constraint.Importance == ImportanceAdvisory {
		status = CheckStatusWarn
	}
	return CheckResult{
		Status:          status,
		Severity:        constraint.Importance,
		Constraint:      constraint.ID,
		Message:         message,
		Expected:        expected,
		Actual:          actual,
		MatchConfidence: matchConfidence,
		MatchStrategy:   matchStrategy,
		Evidence: map[string]string{
			"a":      constraint.A,
			"b":      constraint.B,
			"object": constraint.Object,
		},
	}
}

func relationExpected(constraint Constraint) string {
	return fmt.Sprintf("%s %s %s", constraint.A, constraint.Relation, constraint.B)
}

func edgeValue(bounds Bounds, edge Edge) float64 {
	switch edge {
	case EdgeRight:
		return bounds.X + bounds.W
	case EdgeTop:
		return bounds.Y
	case EdgeBottom:
		return bounds.Y + bounds.H
	default:
		return bounds.X
	}
}

func toleranceOrDefault(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func boundsDelta(a Bounds, b Bounds) float64 {
	return math.Max(math.Max(math.Abs(a.X-b.X), math.Abs(a.Y-b.Y)), math.Max(math.Abs(a.W-b.W), math.Abs(a.H-b.H)))
}

func findObject(contract *Contract, id string) (Object, bool) {
	for _, object := range contract.Objects {
		if object.ID == id {
			return object, true
		}
	}
	return Object{}, false
}

func importanceWeight(importance Importance) float64 {
	switch importance {
	case ImportanceCritical:
		return 8
	case ImportanceMajor:
		return 4
	case ImportanceMinor:
		return 2
	default:
		return 1
	}
}

func minFloat(a float64, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
