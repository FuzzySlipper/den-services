package visualcontract

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

type Service struct {
	artifactBaseURL string
	artifacts       ArtifactStore
}

func NewService(artifactBaseURL string, artifacts ArtifactStore) (*Service, error) {
	if artifactBaseURL == "" {
		return nil, invalidRequest("artifact base url is required")
	}
	if artifacts == nil {
		return nil, invalidRequest("artifact store is required")
	}
	return &Service{artifactBaseURL: artifactBaseURL, artifacts: artifacts}, nil
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

func (s *Service) Compare(ctx context.Context, reference *Contract, candidate *Contract) (*ComparisonReport, error) {
	if err := ValidateContract(reference); err != nil {
		return nil, fmt.Errorf("reference: %w", err)
	}
	if err := ValidateContract(candidate); err != nil {
		return nil, fmt.Errorf("candidate: %w", err)
	}
	report := compareContracts(reference, candidate)
	referenceOverlay, err := RenderContractOverlay(reference, nil)
	if err != nil {
		return nil, fmt.Errorf("rendering reference overlay: %w", err)
	}
	candidateOverlay, err := RenderContractOverlay(candidate, report)
	if err != nil {
		return nil, fmt.Errorf("rendering candidate overlay: %w", err)
	}
	diffOverlay, err := RenderDiffOverlay(candidate, report)
	if err != nil {
		return nil, fmt.Errorf("rendering diff overlay: %w", err)
	}
	run, err := s.persistRun(ctx, reference, candidate, report, referenceOverlay, candidateOverlay, diffOverlay)
	if err != nil {
		return nil, err
	}
	applyRunArtifacts(report, s.artifactBaseURL, run.RunID)
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
	var candidateOverlay string
	var diffOverlay string
	if candidate != nil {
		candidateOverlay, err = RenderContractOverlay(candidate, report)
		if err != nil {
			return nil, fmt.Errorf("rendering candidate overlay: %w", err)
		}
		response.CandidateSVG = candidateOverlay
	}
	if candidate != nil && report != nil {
		diffOverlay, err = RenderDiffOverlay(candidate, report)
		if err != nil {
			return nil, fmt.Errorf("rendering diff overlay: %w", err)
		}
		response.DiffSVG = diffOverlay
	}
	if candidate != nil && report != nil {
		run, err := s.persistRun(ctx, reference, candidate, report, referenceOverlay, candidateOverlay, diffOverlay)
		if err != nil {
			return nil, err
		}
		applyRunArtifacts(report, s.artifactBaseURL, run.RunID)
		response.RunID = run.RunID
		response.Artifacts = report.Artifacts
	}
	return response, nil
}

func (s *Service) GetRun(ctx context.Context, runID string) (*VisualContractRun, error) {
	run, err := s.artifacts.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	run.Artifacts = artifactRefsMap(s.artifactBaseURL, run.RunID, run.Names)
	return run, nil
}

func (s *Service) GetArtifact(ctx context.Context, runID string, name string) (*StoredArtifact, error) {
	return s.artifacts.GetArtifact(ctx, runID, name)
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

func (s *Service) BuildAuthored(_ context.Context, req AuthoredBuildRequest) (*AuthoredBuildResponse, error) {
	contract, err := BuildAuthoredContract(req)
	if err != nil {
		return nil, err
	}
	return &AuthoredBuildResponse{Contract: *contract}, nil
}

func (s *Service) PromoteContract(_ context.Context, req ContractPromotionRequest) (*ContractPromotionResponse, error) {
	return PromoteContract(req)
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
	report.Groups = buildDiagnosticGroups(report.Failures, report.Warnings)
	return report
}

func buildDiagnosticGroups(failures []CheckResult, warnings []CheckResult) []DiagnosticGroup {
	byKey := map[string]*DiagnosticGroup{}
	for _, result := range append(append([]CheckResult(nil), failures...), warnings...) {
		key := string(result.Severity) + ":" + result.MatchStrategy
		group := byKey[key]
		if group == nil {
			group = &DiagnosticGroup{Key: key, Severity: result.Severity}
			byKey[key] = group
		}
		group.Count++
		group.Constraints = append(group.Constraints, result.Constraint)
	}
	groups := make([]DiagnosticGroup, 0, len(byKey))
	for _, group := range byKey {
		groups = append(groups, *group)
	}
	return groups
}

func (s *Service) persistRun(ctx context.Context, reference *Contract, candidate *Contract, report *ComparisonReport, referenceOverlay string, candidateOverlay string, diffOverlay string) (*VisualContractRun, error) {
	runID, err := newRunID()
	if err != nil {
		return nil, err
	}
	applyRunArtifacts(report, s.artifactBaseURL, runID)
	referenceJSON, err := json.MarshalIndent(reference, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding reference contract artifact: %w", err)
	}
	candidateJSON, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding candidate contract artifact: %w", err)
	}
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding report artifact: %w", err)
	}
	return s.artifacts.CreateRun(ctx, RunArtifacts{
		RunID:             runID,
		ReferenceContract: referenceJSON,
		CandidateContract: candidateJSON,
		Report:            reportJSON,
		ReferenceOverlay:  []byte(referenceOverlay),
		CandidateOverlay:  []byte(candidateOverlay),
		DiffOverlay:       []byte(diffOverlay),
	})
}

func applyRunArtifacts(report *ComparisonReport, baseURL string, runID string) {
	report.RunID = runID
	report.Artifacts = ArtifactRefs{
		ReferenceContract: artifactURL(baseURL, runID, ArtifactReferenceContract),
		CandidateContract: artifactURL(baseURL, runID, ArtifactCandidateContract),
		Report:            artifactURL(baseURL, runID, ArtifactReport),
		ReferenceOverlay:  artifactURL(baseURL, runID, ArtifactReferenceOverlay),
		CandidateOverlay:  artifactURL(baseURL, runID, ArtifactCandidateOverlay),
		DiffOverlay:       artifactURL(baseURL, runID, ArtifactDiffOverlay),
	}
}

func artifactRefsMap(baseURL string, runID string, names []string) map[string]string {
	refs := make(map[string]string, len(names))
	for _, name := range names {
		refs[name] = artifactURL(baseURL, runID, name)
	}
	return refs
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
		return failResult(constraint, "unsupported constraint type", string(constraint.Type), "unsupported", 0, "unsupported").withRepairHint("use a supported typed visual-contract constraint")
	}
}

func checkObjectExists(index *contractIndex, constraint Constraint) CheckResult {
	if constraint.Object != "" {
		if index.hasObject(constraint.Object) {
			return passResult(constraint, fmt.Sprintf("%s exists", constraint.Object), 1, "exact_id").withObjects(constraint.Object)
		}
		return failResult(constraint, "required object is missing", constraint.Object, "missing", 0, "exact_id_missing").withObjects(constraint.Object).withRepairHint("add or correctly label the missing visual object")
	}
	for _, object := range index.objects {
		if constraint.Role != "" && object.Role != constraint.Role {
			continue
		}
		if constraint.DomainRole != "" && (object.DomainRole == nil || *object.DomainRole != constraint.DomainRole) {
			continue
		}
		return passResult(constraint, "matching object exists", object.Confidence, "role_domain_role").withObjects(object.ID)
	}
	return failResult(constraint, "no matching object exists", "matching role/domain_role", "missing", 0, "role_domain_role_missing").withRepairHint("add an object with the required role or domain_role")
}

func checkRelation(index *contractIndex, constraint Constraint) CheckResult {
	a, aOK := index.objects[constraint.A]
	b, bOK := index.objects[constraint.B]
	if !aOK || !bOK {
		return failResult(constraint, "relation object is missing", relationExpected(constraint), "missing object", 0, "exact_id_missing").withObjects(constraint.A, constraint.B).withRepairHint("ensure both related objects exist before checking layout")
	}
	matchConfidence := minFloat(a.Confidence, b.Confidence)
	if relationHolds(a.Bounds, b.Bounds, constraint.Relation, toleranceOrDefault(constraint.ToleranceNorm, 0.01)) {
		return passResult(constraint, fmt.Sprintf("%s is %s %s", constraint.A, constraint.Relation, constraint.B), matchConfidence, "exact_id").withObjects(constraint.A, constraint.B).withCandidateBounds(&a.Bounds)
	}
	actual := describeRelativePosition(a.Bounds, b.Bounds)
	return failResult(constraint, "layout relation does not hold", relationExpected(constraint), actual, matchConfidence, "exact_id").withObjects(constraint.A, constraint.B).withCandidateBounds(&a.Bounds).withMeasured(relationMeasurements(a.Bounds, b.Bounds)).withRepairHint(repairHintForRelation(constraint.Relation, constraint.A, constraint.B))
}

func checkAlignment(index *contractIndex, constraint Constraint) CheckResult {
	if len(constraint.Items) < 2 {
		return failResult(constraint, "alignment requires at least two items", "two or more items", "too few items", 0, "insufficient_items").withRepairHint("provide at least two object IDs for an alignment constraint")
	}
	first, ok := index.objects[constraint.Items[0]]
	if !ok {
		return failResult(constraint, "alignment item is missing", constraint.Items[0], "missing", 0, "exact_id_missing").withObjects(constraint.Items[0]).withRepairHint("add or correctly label the missing alignment item")
	}
	tolerance := toleranceOrDefault(constraint.ToleranceNorm, 0.02)
	matchConfidence := first.Confidence
	for _, itemID := range constraint.Items[1:] {
		item, exists := index.objects[itemID]
		if !exists {
			return failResult(constraint, "alignment item is missing", itemID, "missing", 0, "exact_id_missing").withObjects(itemID).withRepairHint("add or correctly label the missing alignment item")
		}
		matchConfidence = minFloat(matchConfidence, item.Confidence)
		delta := math.Abs(edgeValue(first.Bounds, constraint.Edge) - edgeValue(item.Bounds, constraint.Edge))
		if delta > tolerance {
			return failResult(constraint, "items are not aligned", fmt.Sprintf("%s edges aligned", constraint.Edge), fmt.Sprintf("%s differs from %s", itemID, constraint.Items[0]), matchConfidence, "exact_id").withObjects(constraint.Items...).withMeasured(map[string]float64{"edge_delta": delta, "tolerance_norm": tolerance}).withRepairHint("move the misaligned item so the requested edges share the same normalized position")
		}
	}
	return passResult(constraint, "items are aligned", matchConfidence, "exact_id").withObjects(constraint.Items...)
}

func checkAreaRatio(index *contractIndex, constraint Constraint) CheckResult {
	object, ok := index.objects[constraint.Object]
	if !ok {
		return failResult(constraint, "area-ratio object is missing", constraint.Object, "missing", 0, "exact_id_missing").withObjects(constraint.Object).withRepairHint("add or correctly label the object whose area is constrained")
	}
	minRatio := 0.0
	if constraint.MinViewportAreaRatio != nil {
		minRatio = *constraint.MinViewportAreaRatio
	}
	ratio := object.Bounds.W * object.Bounds.H
	if ratio >= minRatio {
		return passResult(constraint, fmt.Sprintf("%s area ratio %.3f >= %.3f", constraint.Object, ratio, minRatio), object.Confidence, "exact_id").withObjects(constraint.Object).withMeasured(map[string]float64{"area_ratio": ratio, "min_area_ratio": minRatio})
	}
	return failResult(constraint, "object area ratio is too small", fmt.Sprintf("area >= %.3f", minRatio), fmt.Sprintf("area %.3f", ratio), object.Confidence, "exact_id").withObjects(constraint.Object).withCandidateBounds(&object.Bounds).withMeasured(map[string]float64{"area_ratio": ratio, "min_area_ratio": minRatio}).withRepairHint("increase the object's width or height, or reduce surrounding regions")
}

func checkBoundsTolerance(reference *Contract, index *contractIndex, constraint Constraint) CheckResult {
	referenceObject, ok := findObject(reference, constraint.Object)
	if !ok {
		return failResult(constraint, "reference bounds object is missing", constraint.Object, "missing reference object", 0, "exact_id_missing").withObjects(constraint.Object)
	}
	candidateObject, ok := index.objects[constraint.Object]
	if !ok {
		return failResult(constraint, "candidate bounds object is missing", constraint.Object, "missing", 0, "exact_id_missing").withObjects(constraint.Object).withRepairHint("add or correctly label the bounded object")
	}
	maxDelta := toleranceOrDefault(constraint.MaxDeltaNorm, 0.03)
	delta := boundsDelta(referenceObject.Bounds, candidateObject.Bounds)
	if delta <= maxDelta {
		return passResult(constraint, fmt.Sprintf("bounds delta %.3f <= %.3f", delta, maxDelta), candidateObject.Confidence, "exact_id").withObjects(constraint.Object).withMeasured(map[string]float64{"bounds_delta": delta, "max_delta_norm": maxDelta})
	}
	return failResult(constraint, "bounds changed beyond tolerance", fmt.Sprintf("delta <= %.3f", maxDelta), fmt.Sprintf("delta %.3f", delta), candidateObject.Confidence, "exact_id").withObjects(constraint.Object).withReferenceBounds(&referenceObject.Bounds).withCandidateBounds(&candidateObject.Bounds).withMeasured(map[string]float64{"bounds_delta": delta, "max_delta_norm": maxDelta}).withRepairHint("move or resize the object toward the reference bounds")
}

func checkContainment(index *contractIndex, constraint Constraint) CheckResult {
	object, ok := index.objects[constraint.Object]
	if !ok {
		return failResult(constraint, "contained object is missing", constraint.Object, "missing", 0, "exact_id_missing").withObjects(constraint.Object).withRepairHint("add or correctly label the contained object")
	}
	parentBounds, parentConfidence, ok := containmentParent(index, object.Parent)
	if !ok {
		return failResult(constraint, "object parent is missing", object.Parent, "missing", 0, "exact_id_missing").withObjects(constraint.Object, object.Parent).withRepairHint("add the parent object or space referenced by the contained object")
	}
	matchConfidence := minFloat(object.Confidence, parentConfidence)
	if relationHolds(object.Bounds, parentBounds, RelationInside, toleranceOrDefault(constraint.ToleranceNorm, 0.01)) {
		return passResult(constraint, fmt.Sprintf("%s is inside %s", object.ID, object.Parent), matchConfidence, "exact_id").withObjects(object.ID, object.Parent)
	}
	return failResult(constraint, "object escapes parent bounds", fmt.Sprintf("%s inside %s", object.ID, object.Parent), "escapes parent", matchConfidence, "exact_id").withObjects(object.ID, object.Parent).withCandidateBounds(&object.Bounds).withMeasured(containmentMeasurements(object.Bounds, parentBounds)).withRepairHint("move or resize the object so it fits inside its parent bounds")
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

func (r CheckResult) withObjects(ids ...string) CheckResult {
	for _, id := range ids {
		if id != "" {
			r.InvolvedObjects = append(r.InvolvedObjects, id)
		}
	}
	return r
}

func (r CheckResult) withMeasured(values map[string]float64) CheckResult {
	r.Measured = values
	return r
}

func (r CheckResult) withReferenceBounds(bounds *Bounds) CheckResult {
	if bounds != nil {
		copy := *bounds
		r.ReferenceBounds = &copy
	}
	return r
}

func (r CheckResult) withCandidateBounds(bounds *Bounds) CheckResult {
	if bounds != nil {
		copy := *bounds
		r.CandidateBounds = &copy
	}
	return r
}

func (r CheckResult) withRepairHint(hint string) CheckResult {
	r.RepairHint = hint
	return r
}

func relationExpected(constraint Constraint) string {
	return fmt.Sprintf("%s %s %s", constraint.A, constraint.Relation, constraint.B)
}

func relationMeasurements(a Bounds, b Bounds) map[string]float64 {
	return map[string]float64{
		"a_left":   a.X,
		"a_right":  a.X + a.W,
		"a_top":    a.Y,
		"a_bottom": a.Y + a.H,
		"b_left":   b.X,
		"b_right":  b.X + b.W,
		"b_top":    b.Y,
		"b_bottom": b.Y + b.H,
	}
}

func containmentMeasurements(object Bounds, parent Bounds) map[string]float64 {
	return map[string]float64{
		"object_left":   object.X,
		"object_right":  object.X + object.W,
		"object_top":    object.Y,
		"object_bottom": object.Y + object.H,
		"parent_left":   parent.X,
		"parent_right":  parent.X + parent.W,
		"parent_top":    parent.Y,
		"parent_bottom": parent.Y + parent.H,
	}
}

func repairHintForRelation(relation RelationType, a string, b string) string {
	switch relation {
	case RelationRightOf:
		return fmt.Sprintf("move %s to the right of %s or move %s left", a, b, b)
	case RelationLeftOf:
		return fmt.Sprintf("move %s to the left of %s or move %s right", a, b, b)
	case RelationAbove:
		return fmt.Sprintf("move %s above %s or move %s lower", a, b, b)
	case RelationBelow:
		return fmt.Sprintf("move %s below %s or move %s higher", a, b, b)
	default:
		return "adjust the involved object bounds until the requested relation holds"
	}
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
