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

func TestWebEvidenceAdapterPreservesNearestCollectorParentAndRelations(t *testing.T) {
	service := newTestService(t)
	evidence := WebEvidence{
		SceneID: "sample_page",
		Viewport: Viewport{
			WidthPX:  1440,
			HeightPX: 900,
		},
		ScreenshotRef: "sample-page.png",
		Nodes: []WebNode{
			{
				ID:   "hero_section",
				Role: "hero",
				Tag:  "main",
				BoundsPX: PixelBounds{
					X: 86,
					Y: 72,
					W: 1268,
					H: 288,
				},
				Attributes: map[string]string{
					"data-visual-id":   "hero_section",
					"data-visual-role": "hero",
				},
			},
			{
				ID:       "node-1",
				ParentID: "hero_section",
				Tag:      "section",
				BoundsPX: PixelBounds{
					X: 86,
					Y: 72,
					W: 602,
					H: 288,
				},
			},
			{
				ID:       "hero_title",
				ParentID: "node-1",
				Tag:      "h1",
				BoundsPX: PixelBounds{
					X: 86,
					Y: 72,
					W: 602,
					H: 101,
				},
				Attributes: map[string]string{
					"data-visual-id": "hero_title",
				},
			},
			{
				ID:       "primary_cta",
				ParentID: "node-1",
				Tag:      "button",
				BoundsPX: PixelBounds{
					X: 86,
					Y: 205,
					W: 230,
					H: 54,
				},
				Attributes: map[string]string{
					"data-visual-id": "primary_cta",
				},
			},
			{
				ID:       "preview_card",
				ParentID: "hero_section",
				Role:     "visual_preview",
				Tag:      "aside",
				BoundsPX: PixelBounds{
					X: 752,
					Y: 72,
					W: 602,
					H: 288,
				},
				Attributes: map[string]string{
					"data-visual-id":   "preview_card",
					"data-visual-role": "visual_preview",
				},
			},
		},
	}

	contract, err := service.FromWebEvidence(context.Background(), &evidence)
	if err != nil {
		t.Fatalf("FromWebEvidence() error = %v", err)
	}
	if err := ValidateContract(contract); err != nil {
		t.Fatalf("ValidateContract(adapter output) error = %v", err)
	}
	heroTitle := objectByID(t, contract, "hero_title")
	if heroTitle.Parent != "node_1" {
		t.Fatalf("hero_title parent = %s, want node_1", heroTitle.Parent)
	}
	primaryCTA := objectByID(t, contract, "primary_cta")
	if primaryCTA.Parent != "node_1" {
		t.Fatalf("primary_cta parent = %s, want node_1", primaryCTA.Parent)
	}
	if !hasRelation(contract.Relations, RelationRightOf, "preview_card", "hero_title") {
		t.Fatal("web adapter should infer preview_card right_of hero_title after collector conversion")
	}
	if !hasRelation(contract.Relations, RelationLeftOf, "hero_title", "preview_card") {
		t.Fatal("web adapter should infer complementary hero_title left_of preview_card after collector conversion")
	}
}

func TestWebEvidenceAdapterAcceptsClippedTallViewportEvidence(t *testing.T) {
	service := newTestService(t)
	evidence := WebEvidence{
		SceneID:         "tall_page",
		CoordinateSpace: WebCoordinateSpaceViewport,
		CaptureMode:     WebCaptureModeViewportClipped,
		Viewport: Viewport{
			WidthPX:  1920,
			HeightPX: 1080,
		},
		PageSize: &Viewport{WidthPX: 1920, HeightPX: 1800},
		Nodes: []WebNode{
			{
				ID:   "asha_shell",
				Role: "app_shell",
				Tag:  "main",
				BoundsPX: PixelBounds{
					X: 0,
					Y: 0,
					W: 1920,
					H: 1080,
				},
				OriginalBounds: &PixelBounds{
					X: 0,
					Y: 0,
					W: 1920,
					H: 1800,
				},
				BoundsClipped: true,
				Attributes: map[string]string{
					"data-visual-id":   "asha_shell",
					"data-visual-role": "app_shell",
				},
			},
			{
				ID:       "central_viewport",
				ParentID: "asha_shell",
				Role:     "central_3d_viewport",
				Tag:      "section",
				BoundsPX: PixelBounds{
					X: 384,
					Y: 24,
					W: 1040,
					H: 760,
				},
				Attributes: map[string]string{
					"data-visual-id":   "central_viewport",
					"data-visual-role": "central_3d_viewport",
				},
			},
			{
				ID:       "evidence_dock",
				ParentID: "asha_shell",
				Role:     "evidence_dock",
				Tag:      "aside",
				BoundsPX: PixelBounds{
					X: 1456,
					Y: 1000,
					W: 400,
					H: 80,
				},
				OriginalBounds: &PixelBounds{
					X: 1456,
					Y: 1000,
					W: 400,
					H: 260,
				},
				BoundsClipped: true,
				Attributes: map[string]string{
					"data-visual-id":   "evidence_dock",
					"data-visual-role": "evidence_dock",
				},
			},
		},
	}

	contract, err := service.FromWebEvidence(context.Background(), &evidence)
	if err != nil {
		t.Fatalf("FromWebEvidence(clipped tall evidence) error = %v", err)
	}
	if err := ValidateContract(contract); err != nil {
		t.Fatalf("ValidateContract(clipped tall output) error = %v", err)
	}
	dock := objectByID(t, contract, "evidence_dock")
	if dock.Bounds.Y+dock.Bounds.H > 1 {
		t.Fatalf("evidence_dock normalized bounds exceed viewport: %+v", dock.Bounds)
	}
}

func TestWebEvidenceAdapterRejectsDishonestPageAndViewportEvidence(t *testing.T) {
	service := newTestService(t)
	tests := []struct {
		name     string
		evidence WebEvidence
		want     string
	}{
		{
			name: "page space",
			evidence: WebEvidence{
				SceneID:         "page_space",
				CoordinateSpace: WebCoordinateSpacePage,
				CaptureMode:     WebCaptureModePage,
				Viewport:        Viewport{WidthPX: 1920, HeightPX: 1080},
				Nodes: []WebNode{
					{ID: "offscreen_log", Tag: "section", Role: "diagnostic_log", BoundsPX: PixelBounds{X: 384, Y: 1360, W: 1040, H: 280}},
				},
			},
			want: "coordinate_space page is not accepted",
		},
		{
			name: "viewport overflow",
			evidence: WebEvidence{
				SceneID:         "viewport_overflow",
				CoordinateSpace: WebCoordinateSpaceViewport,
				CaptureMode:     WebCaptureModeViewport,
				Viewport:        Viewport{WidthPX: 1920, HeightPX: 1080},
				Nodes: []WebNode{
					{ID: "evidence_dock", Tag: "aside", Role: "evidence_dock", BoundsPX: PixelBounds{X: 1456, Y: 1000, W: 400, H: 260}},
				},
			},
			want: "web node evidence_dock viewport bounds exceed viewport",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.FromWebEvidence(context.Background(), &tt.evidence)
			if err == nil {
				t.Fatal("FromWebEvidence() error = nil, want error")
			}
			assertContains(t, err.Error(), tt.want)
		})
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

func TestPromoteContractFromGenericASHAFixture(t *testing.T) {
	var req ContractPromotionRequest
	loadJSONFixture(t, "../testdata/authored/asha-promotion.json", &req)
	generic := genericASHALikeContract()
	req.Contract = &generic

	response, err := PromoteContract(req)
	if err != nil {
		t.Fatalf("PromoteContract() error = %v", err)
	}
	contract := response.Contract
	if err := ValidateContract(&contract); err != nil {
		t.Fatalf("ValidateContract(promoted output) error = %v", err)
	}
	if len(response.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", response.Diagnostics)
	}
	if contract.Project == nil || contract.Project.ID != "asha" {
		t.Fatalf("project = %+v, want asha", contract.Project)
	}
	central := objectByID(t, &contract, "central_3d_viewport")
	if central.DomainRole == nil || *central.DomainRole != "central_3d_viewport" {
		t.Fatalf("central domain_role = %v, want central_3d_viewport", central.DomainRole)
	}
	if central.SemanticDescription == "" {
		t.Fatal("central viewport should carry promotion semantic description")
	}
	if _, found := findObject(&contract, "node_noise"); found {
		t.Fatal("ignored noisy node should not be present")
	}
	if !hasRelation(contract.Relations, RelationRightOf, "selected_target_inspector", "central_3d_viewport") {
		t.Fatal("promoted contract should infer inspector right_of central viewport")
	}
	report := compareContracts(&contract, &contract)
	if report.Verdict != VerdictPass {
		t.Fatalf("promoted contract should compare against itself; verdict=%s failures=%+v", report.Verdict, report.Failures)
	}
	assertPassed(t, report, "central_viewport_exists")
	assertPassed(t, report, "central_viewport_dominant")
	assertPassed(t, report, "inspector_right_of_viewport")
	assertPassed(t, report, "timeline_below_viewport")
}

func TestPromoteContractDiagnostics(t *testing.T) {
	generic := genericASHALikeContract()
	t.Run("unmapped important", func(t *testing.T) {
		response, err := PromoteContract(ContractPromotionRequest{Contract: &generic})
		if err != nil {
			t.Fatalf("PromoteContract() error = %v", err)
		}
		if !hasDraftDiagnostic(response.Diagnostics, "unmapped_important_node") {
			t.Fatalf("diagnostics = %+v, want unmapped_important_node", response.Diagnostics)
		}
	})
	t.Run("duplicate target", func(t *testing.T) {
		_, err := PromoteContract(ContractPromotionRequest{
			Contract: &generic,
			Objects: []ObjectPromotionRule{
				{SourceID: "node_1", TargetID: "duplicate"},
				{SourceID: "node_2", TargetID: "duplicate"},
			},
		})
		if err == nil {
			t.Fatal("PromoteContract() error = nil, want duplicate target error")
		}
		assertContains(t, err.Error(), "duplicate target_id duplicate")
	})
	t.Run("unknown domain role diagnostic", func(t *testing.T) {
		response, err := PromoteContract(ContractPromotionRequest{
			Contract: &generic,
			Project:  &Project{ID: "asha", Roles: []string{"known_role"}},
			Objects: []ObjectPromotionRule{
				{SourceID: "node_1", TargetID: "scene_hierarchy", DomainRole: "unknown_role"},
			},
		})
		if err != nil {
			t.Fatalf("PromoteContract() error = %v", err)
		}
		if !hasDraftDiagnostic(response.Diagnostics, "unknown_domain_role") {
			t.Fatalf("diagnostics = %+v, want unknown_domain_role", response.Diagnostics)
		}
	})
	t.Run("unknown constraint object", func(t *testing.T) {
		_, err := PromoteContract(ContractPromotionRequest{
			Contract: &generic,
			Constraints: []AuthoredConstraint{
				{ID: "missing", Type: ConstraintObjectExists, Object: "missing_object", Importance: ImportanceCritical},
			},
		})
		if err == nil {
			t.Fatal("PromoteContract() error = nil, want unknown object error")
		}
		assertContains(t, err.Error(), "unknown object missing_object")
	})
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

func hasDraftDiagnostic(diagnostics []ContractDraftDiagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func genericASHALikeContract() Contract {
	return Contract{
		Schema: SchemaVersion,
		Scene: Scene{
			ID:             "asha_generic",
			Type:           "web_ui",
			Viewport:       Viewport{WidthPX: 1920, HeightPX: 1080},
			CoordinateMode: "normalized_with_pixel_evidence",
		},
		Spaces: []Space{
			{
				ID:   "viewport",
				Kind: "root",
				Bounds: Bounds{
					X: 0,
					Y: 0,
					W: 1,
					H: 1,
				},
			},
		},
		Layers: []Layer{
			{ID: "z_0", Z: 0, Contains: []string{"node_1", "node_2", "node_3", "node_4", "node_5", "node_noise"}},
		},
		Objects: []Object{
			genericObject("node_1", "viewport", 0.02, 0.02, 0.16, 0.72),
			genericObject("node_2", "viewport", 0.20, 0.02, 0.54, 0.72),
			genericObject("node_3", "viewport", 0.76, 0.02, 0.21, 0.72),
			genericObject("node_4", "viewport", 0.20, 0.78, 0.54, 0.16),
			genericObject("node_5", "viewport", 0.76, 0.78, 0.21, 0.16),
			genericObject("node_noise", "viewport", 0.01, 0.95, 0.08, 0.03),
		},
		Evidence: EvidenceSet{
			SourceType:        "fixture",
			GeneratedBy:       "visual-contract-test",
			OverallConfidence: 0.9,
			Records: []EvidenceRecord{
				genericEvidence("node_1"),
				genericEvidence("node_2"),
				genericEvidence("node_3"),
				genericEvidence("node_4"),
				genericEvidence("node_5"),
				genericEvidence("node_noise"),
			},
		},
	}
}

func genericObject(id string, parent string, x float64, y float64, w float64, h float64) Object {
	return Object{
		ID:           id,
		Kind:         "panel",
		Role:         "generic",
		Parent:       parent,
		Layer:        "z_0",
		Bounds:       Bounds{Space: "viewport", X: x, Y: y, W: w, H: h},
		Importance:   ImportanceMajor,
		Confidence:   0.9,
		EvidenceRefs: []string{"fixture:" + id},
	}
}

func genericEvidence(id string) EvidenceRecord {
	return EvidenceRecord{
		ID:         "fixture:" + id,
		Kind:       "fixture",
		ObjectRefs: []string{id},
		Confidence: 0.9,
	}
}
