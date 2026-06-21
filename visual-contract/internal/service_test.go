package visualcontract

import (
	"context"
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

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, err := NewService("http://127.0.0.1:8086/artifacts")
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
