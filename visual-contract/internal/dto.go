package visualcontract

type ValidateRequest struct {
	Contract Contract `json:"contract"`
}

func (r ValidateRequest) Validate() error {
	return ValidateContract(&r.Contract)
}

type ValidationResponse struct {
	Schema  string         `json:"schema"`
	Valid   bool           `json:"valid"`
	SceneID string         `json:"scene_id"`
	Counts  ContractCounts `json:"counts"`
}

type ContractCounts struct {
	Spaces      int `json:"spaces"`
	Layers      int `json:"layers"`
	Objects     int `json:"objects"`
	Relations   int `json:"relations"`
	Constraints int `json:"constraints"`
	Evidence    int `json:"evidence"`
}

type CompareRequest struct {
	Reference Contract `json:"reference"`
	Candidate Contract `json:"candidate"`
}

func (r CompareRequest) Validate() error {
	if err := ValidateContract(&r.Reference); err != nil {
		return err
	}
	return ValidateContract(&r.Candidate)
}

type OverlayRequest struct {
	Reference Contract          `json:"reference"`
	Candidate *Contract         `json:"candidate,omitempty"`
	Report    *ComparisonReport `json:"report,omitempty"`
}

func (r OverlayRequest) Validate() error {
	if err := ValidateContract(&r.Reference); err != nil {
		return err
	}
	if r.Candidate != nil {
		return ValidateContract(r.Candidate)
	}
	return nil
}

type OverlayResponse struct {
	ReferenceSVG string `json:"reference_svg"`
	CandidateSVG string `json:"candidate_svg,omitempty"`
	DiffSVG      string `json:"diff_svg,omitempty"`
}

type WebEvidenceRequest struct {
	Evidence WebEvidence `json:"evidence"`
}

func (r WebEvidenceRequest) Validate() error {
	if r.Evidence.SceneID == "" {
		return invalidRequest("evidence.scene_id is required")
	}
	return nil
}
