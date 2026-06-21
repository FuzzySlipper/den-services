package visualcontract

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestCompareGoldenFixtures(t *testing.T) {
	service := newTestService(t)
	reference := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")

	passCandidate := loadContractFixture(t, "../testdata/contracts/candidate.pass.web-ui.json")
	passReport, err := service.Compare(context.Background(), &reference, &passCandidate)
	if err != nil {
		t.Fatalf("Compare(pass) error = %v", err)
	}
	if passReport.Verdict != VerdictPass {
		t.Fatalf("pass verdict = %s, want pass", passReport.Verdict)
	}
	if len(passReport.Failures) != 0 {
		t.Fatalf("pass failures = %d, want 0", len(passReport.Failures))
	}
	if len(passReport.Passes) == 0 {
		t.Fatal("pass report should include passes")
	}

	failCandidate := loadContractFixture(t, "../testdata/contracts/candidate.fail.web-ui.json")
	failReport, err := service.Compare(context.Background(), &reference, &failCandidate)
	if err != nil {
		t.Fatalf("Compare(fail) error = %v", err)
	}
	if failReport.Verdict != VerdictFail {
		t.Fatalf("fail verdict = %s, want fail", failReport.Verdict)
	}
	if len(failReport.Failures) == 0 {
		t.Fatal("fail report should include failures")
	}
	if failReport.Score >= passReport.Score {
		t.Fatalf("fail score %.2f should be less than pass score %.2f", failReport.Score, passReport.Score)
	}
	assertDiagnostic(t, failReport, "hero_title_above_cta", "a_right")
	assertDiagnostic(t, failReport, "hero_cluster_left_aligned", "edge_delta")
	assertDiagnostic(t, failReport, "preview_card_large_enough", "area_ratio")
	if len(failReport.Groups) == 0 {
		t.Fatal("fail report should include diagnostic groups")
	}
}

func TestOverlayGeneration(t *testing.T) {
	service := newTestService(t)
	reference := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	candidate := loadContractFixture(t, "../testdata/contracts/candidate.fail.web-ui.json")
	report, err := service.Compare(context.Background(), &reference, &candidate)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	overlays, err := service.Overlays(context.Background(), &reference, &candidate, report)
	if err != nil {
		t.Fatalf("Overlays() error = %v", err)
	}
	assertContains(t, overlays.ReferenceSVG, `data-object-id="hero_title"`)
	assertContains(t, overlays.CandidateSVG, `data-object-id="preview_card"`)
	assertContains(t, overlays.DiffSVG, `data-overlay-section="failures"`)
	assertContains(t, overlays.DiffSVG, `hero_title_above_cta`)
	if overlays.RunID == "" {
		t.Fatal("overlay response should include run_id")
	}
	if overlays.Artifacts.DiffOverlay == "" {
		t.Fatal("overlay response should include diff overlay artifact ref")
	}
}

func TestComparePersistsRetrievableArtifacts(t *testing.T) {
	service := newTestService(t)
	reference := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	candidate := loadContractFixture(t, "../testdata/contracts/candidate.fail.web-ui.json")
	report, err := service.Compare(context.Background(), &reference, &candidate)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if report.RunID == "" {
		t.Fatal("Compare() should return run_id")
	}
	run, err := service.GetRun(context.Background(), report.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.Artifacts[ArtifactReport] == "" {
		t.Fatalf("run artifacts missing report ref: %+v", run.Artifacts)
	}
	storedReport, err := service.GetArtifact(context.Background(), report.RunID, ArtifactReport)
	if err != nil {
		t.Fatalf("GetArtifact(report) error = %v", err)
	}
	var fetchedReport ComparisonReport
	if err := json.Unmarshal(storedReport.Body, &fetchedReport); err != nil {
		t.Fatalf("Unmarshal(report) error = %v", err)
	}
	if fetchedReport.RunID != report.RunID || fetchedReport.Verdict != report.Verdict {
		t.Fatalf("stored report mismatch: got run=%s verdict=%s want run=%s verdict=%s", fetchedReport.RunID, fetchedReport.Verdict, report.RunID, report.Verdict)
	}
	storedOverlay, err := service.GetArtifact(context.Background(), report.RunID, ArtifactDiffOverlay)
	if err != nil {
		t.Fatalf("GetArtifact(diff overlay) error = %v", err)
	}
	assertContains(t, string(storedOverlay.Body), `data-overlay-section="failures"`)
	_, err = service.GetArtifact(context.Background(), report.RunID, "missing.svg")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing artifact error = %v, want ErrNotFound", err)
	}
}

func TestWebEvidenceAdapterProducesValidContract(t *testing.T) {
	var req WebEvidenceRequest
	loadJSONFixture(t, "../testdata/web/web-evidence.hero.json", &req)
	service := newTestService(t)
	contract, err := service.FromWebEvidence(context.Background(), &req.Evidence)
	if err != nil {
		t.Fatalf("FromWebEvidence() error = %v", err)
	}
	if contract.Schema != SchemaVersion {
		t.Fatalf("schema = %s, want %s", contract.Schema, SchemaVersion)
	}
	if len(contract.Objects) != len(req.Evidence.Nodes) {
		t.Fatalf("objects = %d, want %d", len(contract.Objects), len(req.Evidence.Nodes))
	}
	if err := ValidateContract(contract); err != nil {
		t.Fatalf("ValidateContract(adapter output) error = %v", err)
	}
	heroTitle := objectByID(t, contract, "hero_title")
	if heroTitle.Parent != "hero_section" {
		t.Fatalf("hero_title parent = %s, want hero_section", heroTitle.Parent)
	}
	if heroTitle.Bounds.Space != "viewport" {
		t.Fatalf("hero_title bounds.space = %s, want viewport for viewport-normalized web evidence", heroTitle.Bounds.Space)
	}
	if heroTitle.Bounds.X < 0.079 || heroTitle.Bounds.X > 0.081 {
		t.Fatalf("hero_title bounds.x = %.5f, want viewport-normalized value around 0.08", heroTitle.Bounds.X)
	}
	if !hasRelation(contract.Relations, RelationRightOf, "preview_card", "hero_title") {
		t.Fatal("web adapter should infer preview_card right_of hero_title regardless of input order")
	}
	if !hasRelation(contract.Relations, RelationLeftOf, "hero_title", "preview_card") {
		t.Fatal("web adapter should infer complementary hero_title left_of preview_card")
	}
}

func TestCompareReportsMatchConfidence(t *testing.T) {
	service := newTestService(t)
	reference := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	candidate := loadContractFixture(t, "../testdata/contracts/candidate.pass.web-ui.json")
	report, err := service.Compare(context.Background(), &reference, &candidate)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if len(report.Passes) == 0 {
		t.Fatal("expected pass results")
	}
	for _, pass := range report.Passes {
		if pass.MatchStrategy == "" {
			t.Fatalf("pass %s missing match strategy", pass.Constraint)
		}
		if pass.MatchConfidence <= 0 {
			t.Fatalf("pass %s match confidence = %.2f, want > 0", pass.Constraint, pass.MatchConfidence)
		}
	}
}

func TestBuildAuthoredContract(t *testing.T) {
	base := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	base.Constraints = nil
	tolerance := 0.02
	minArea := 0.08
	req := AuthoredBuildRequest{
		Contract: base,
		Vocabulary: AuthoredVocabulary{
			RoleAliases: map[string]string{"heroTitle": "heading_1"},
		},
		Constraints: []AuthoredConstraint{
			{ID: "title_exists_by_alias", Type: ConstraintObjectExists, Role: "heroTitle", Importance: ImportanceCritical},
			{ID: "preview_area", Type: ConstraintAreaRatio, Object: "preview_card", MinViewportAreaRatio: &minArea, Importance: ImportanceMajor},
			{ID: "preview_right", Type: ConstraintLayoutRelation, A: "preview_card", Relation: RelationRightOf, B: "hero_title", ToleranceNorm: &tolerance, Importance: ImportanceMajor},
			{ID: "hero_inside_viewport", Type: ConstraintContainment, Object: "hero_section", Importance: ImportanceCritical},
			{ID: "left_aligned", Type: ConstraintAlignment, Items: []string{"hero_title", "primary_cta"}, Edge: EdgeLeft, ToleranceNorm: &tolerance, Importance: ImportanceMajor},
		},
	}
	contract, err := BuildAuthoredContract(req)
	if err != nil {
		t.Fatalf("BuildAuthoredContract() error = %v", err)
	}
	if len(contract.Constraints) != len(req.Constraints) {
		t.Fatalf("constraints = %d, want %d", len(contract.Constraints), len(req.Constraints))
	}
	if err := ValidateContract(contract); err != nil {
		t.Fatalf("ValidateContract(builder output) error = %v", err)
	}
	report := compareContracts(contract, &base)
	if report.Verdict != VerdictPass {
		t.Fatalf("builder output compare verdict = %s, want pass; failures=%+v", report.Verdict, report.Failures)
	}
}

func TestBuildAuthoredContractFixture(t *testing.T) {
	base := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	base.Constraints = nil
	var req AuthoredBuildRequest
	loadJSONFixture(t, "../testdata/authored/demo-authored-constraints.json", &req)
	req.Contract = base
	contract, err := BuildAuthoredContract(req)
	if err != nil {
		t.Fatalf("BuildAuthoredContract(fixture) error = %v", err)
	}
	if err := ValidateContract(contract); err != nil {
		t.Fatalf("ValidateContract(fixture output) error = %v", err)
	}
}

func TestBuildAuthoredContractDiagnostics(t *testing.T) {
	base := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	tests := []struct {
		name       string
		constraint AuthoredConstraint
		want       string
	}{
		{
			name:       "unknown object",
			constraint: AuthoredConstraint{ID: "bad_object", Type: ConstraintObjectExists, Object: "missing", Importance: ImportanceMajor},
			want:       "unknown object",
		},
		{
			name:       "unknown role",
			constraint: AuthoredConstraint{ID: "bad_role", Type: ConstraintObjectExists, Role: "missing_role", Importance: ImportanceMajor},
			want:       "unknown role",
		},
		{
			name:       "unsupported type",
			constraint: AuthoredConstraint{ID: "bad_type", Type: "magic", Object: "hero_title", Importance: ImportanceMajor},
			want:       "unsupported type",
		},
		{
			name:       "malformed area",
			constraint: AuthoredConstraint{ID: "bad_area", Type: ConstraintAreaRatio, Object: "hero_title", Importance: ImportanceMajor},
			want:       "requires min_viewport_area_ratio",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildAuthoredContract(AuthoredBuildRequest{
				Contract:    base,
				Constraints: []AuthoredConstraint{tt.constraint},
			})
			if err == nil {
				t.Fatal("BuildAuthoredContract() error = nil, want error")
			}
			assertContains(t, err.Error(), tt.want)
		})
	}
}

func TestContainmentConstraintSupportsSpaceAndObjectParents(t *testing.T) {
	service := newTestService(t)
	reference := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
	reference.Constraints = append(reference.Constraints,
		Constraint{
			ID:         "hero_section_inside_viewport",
			Type:       ConstraintContainment,
			Object:     "hero_section",
			Importance: ImportanceCritical,
		},
		Constraint{
			ID:         "hero_title_inside_hero_section",
			Type:       ConstraintContainment,
			Object:     "hero_title",
			Importance: ImportanceCritical,
		},
	)
	report, err := service.Compare(context.Background(), &reference, &reference)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if report.Verdict != VerdictPass {
		t.Fatalf("verdict = %s, want pass; failures=%v", report.Verdict, report.Failures)
	}
	assertPassed(t, report, "hero_section_inside_viewport")
	assertPassed(t, report, "hero_title_inside_hero_section")
}

func TestASHAStudioPilotFixtures(t *testing.T) {
	service := newTestService(t)
	reference := loadContractFixture(t, "../testdata/asha/asha-studio.contract.json")
	passReport, err := service.Compare(context.Background(), &reference, &reference)
	if err != nil {
		t.Fatalf("Compare(asha reference) error = %v", err)
	}
	if passReport.Verdict != VerdictPass {
		t.Fatalf("ASHA reference verdict = %s, want pass; failures=%+v", passReport.Verdict, passReport.Failures)
	}
	candidate := loadContractFixture(t, "../testdata/asha/asha-studio.candidate.fail.contract.json")
	failReport, err := service.Compare(context.Background(), &reference, &candidate)
	if err != nil {
		t.Fatalf("Compare(asha candidate) error = %v", err)
	}
	if failReport.Verdict != VerdictFail {
		t.Fatalf("ASHA fail verdict = %s, want fail", failReport.Verdict)
	}
	assertResultPresent(t, failReport.Failures, "central_viewport_is_dominant")
	assertResultPresent(t, failReport.Failures, "selection_outline_visible")
	assertResultPresent(t, failReport.Failures, "preview_ghost_visible")
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, err := NewService("http://127.0.0.1:8086/visual-contracts", NewFileArtifactStore(t.TempDir()))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func objectByID(t *testing.T, contract *Contract, id string) Object {
	t.Helper()
	for _, object := range contract.Objects {
		if object.ID == id {
			return object
		}
	}
	t.Fatalf("object %s not found", id)
	return Object{}
}

func hasRelation(relations []Relation, relationType RelationType, a string, b string) bool {
	for _, relation := range relations {
		if relation.Type == relationType && relation.A == a && relation.B == b {
			return true
		}
	}
	return false
}

func assertPassed(t *testing.T, report *ComparisonReport, constraintID string) {
	t.Helper()
	for _, pass := range report.Passes {
		if pass.Constraint == constraintID {
			return
		}
	}
	t.Fatalf("constraint %s not found in passes: %+v", constraintID, report.Passes)
}

func assertResultPresent(t *testing.T, results []CheckResult, constraintID string) {
	t.Helper()
	for _, result := range results {
		if result.Constraint == constraintID {
			return
		}
	}
	t.Fatalf("constraint %s not found in results: %+v", constraintID, results)
}

func assertDiagnostic(t *testing.T, report *ComparisonReport, constraintID string, measuredKey string) {
	t.Helper()
	for _, result := range append(append([]CheckResult(nil), report.Failures...), report.Warnings...) {
		if result.Constraint != constraintID {
			continue
		}
		if len(result.InvolvedObjects) == 0 {
			t.Fatalf("%s missing involved objects", constraintID)
		}
		if result.RepairHint == "" {
			t.Fatalf("%s missing repair hint", constraintID)
		}
		if _, ok := result.Measured[measuredKey]; !ok {
			t.Fatalf("%s missing measured key %s: %+v", constraintID, measuredKey, result.Measured)
		}
		return
	}
	t.Fatalf("diagnostic %s not found", constraintID)
}
