package visualcontract

import "fmt"

type ContractPromotionRequest struct {
	Contract      *Contract             `json:"contract,omitempty"`
	Evidence      *WebEvidence          `json:"evidence,omitempty"`
	Project       *Project              `json:"project,omitempty"`
	Vocabulary    AuthoredVocabulary    `json:"vocabulary,omitempty"`
	Objects       []ObjectPromotionRule `json:"objects,omitempty"`
	IgnoreObjects []string              `json:"ignore_objects,omitempty"`
	Constraints   []AuthoredConstraint  `json:"constraints,omitempty"`
}

type ObjectPromotionRule struct {
	SourceID            string     `json:"source_id"`
	TargetID            string     `json:"target_id,omitempty"`
	Role                string     `json:"role,omitempty"`
	DomainRole          string     `json:"domain_role,omitempty"`
	ParentID            string     `json:"parent_id,omitempty"`
	Kind                string     `json:"kind,omitempty"`
	Importance          Importance `json:"importance,omitempty"`
	SemanticDescription string     `json:"semantic_description,omitempty"`
	Ignore              bool       `json:"ignore,omitempty"`
}

type ContractPromotionResponse struct {
	Contract    Contract                  `json:"contract"`
	Diagnostics []ContractDraftDiagnostic `json:"diagnostics,omitempty"`
}

type ContractDraftDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	SourceID string `json:"source_id,omitempty"`
	TargetID string `json:"target_id,omitempty"`
}

func PromoteContract(req ContractPromotionRequest) (*ContractPromotionResponse, error) {
	source, err := promotionSourceContract(req)
	if err != nil {
		return nil, err
	}
	if err := ValidateContract(source); err != nil {
		return nil, err
	}
	sourceIndex, err := buildContractIndex(source)
	if err != nil {
		return nil, err
	}

	rules, ignored, err := promotionRules(req, sourceIndex)
	if err != nil {
		return nil, err
	}
	targetBySource, err := promotionTargetIDs(source.Objects, rules, ignored)
	if err != nil {
		return nil, err
	}

	diagnostics := make([]ContractDraftDiagnostic, 0)
	project := source.Project
	if req.Project != nil {
		project = req.Project
	}
	objects := make([]Object, 0, len(source.Objects))
	for _, object := range source.Objects {
		if ignored[object.ID] {
			continue
		}
		rule := rules[object.ID]
		promoted := promoteObject(object, rule, targetBySource, ignored, req.Vocabulary)
		promoted.EvidenceRefs = []string{"promotion:" + promoted.ID}
		if rule == nil && isImportantGeneratedObject(object) {
			diagnostics = append(diagnostics, ContractDraftDiagnostic{
				Severity: "warn",
				Code:     "unmapped_important_node",
				Message:  "important generated object was kept with its source id; consider promoting or ignoring it",
				SourceID: object.ID,
				TargetID: promoted.ID,
			})
		}
		if promoted.DomainRole != nil && project != nil && !projectHasRole(project, *promoted.DomainRole) {
			diagnostics = append(diagnostics, ContractDraftDiagnostic{
				Severity: "warn",
				Code:     "unknown_domain_role",
				Message:  "promoted object domain_role is not listed in the supplied project vocabulary",
				SourceID: object.ID,
				TargetID: promoted.ID,
			})
		}
		objects = append(objects, promoted)
	}
	if len(objects) == 0 {
		return nil, invalidRequest("promotion would remove every object")
	}
	childrenByParent := promotedChildren(source.Objects, targetBySource, ignored)
	for index := range objects {
		objects[index].Children = childrenByParent[objects[index].ID]
	}

	promoted := &Contract{
		Schema:    source.Schema,
		Scene:     source.Scene,
		Project:   project,
		Spaces:    append([]Space(nil), source.Spaces...),
		Layers:    promoteLayers(source.Layers, targetBySource, ignored),
		Objects:   objects,
		Relations: inferRelations(objects),
		Evidence:  promotionEvidence(objects),
	}
	if len(promoted.Layers) == 0 {
		return nil, invalidRequest("promotion would remove every layer")
	}
	if len(req.Constraints) > 0 {
		authored, err := BuildAuthoredContract(AuthoredBuildRequest{
			Contract:    *promoted,
			Vocabulary:  req.Vocabulary,
			Constraints: req.Constraints,
		})
		if err != nil {
			return nil, err
		}
		promoted = authored
	} else if err := ValidateContract(promoted); err != nil {
		return nil, err
	}
	return &ContractPromotionResponse{Contract: *promoted, Diagnostics: diagnostics}, nil
}

func promotionSourceContract(req ContractPromotionRequest) (*Contract, error) {
	switch {
	case req.Contract != nil && req.Evidence != nil:
		return nil, invalidRequest("provide contract or evidence, not both")
	case req.Contract != nil:
		contract := *req.Contract
		return &contract, nil
	case req.Evidence != nil:
		return BuildContractFromWebEvidence(req.Evidence)
	default:
		return nil, invalidRequest("promotion requires contract or evidence")
	}
}

func promotionRules(req ContractPromotionRequest, index *contractIndex) (map[string]*ObjectPromotionRule, map[string]bool, error) {
	rules := make(map[string]*ObjectPromotionRule, len(req.Objects))
	ignored := make(map[string]bool, len(req.IgnoreObjects))
	for _, id := range req.IgnoreObjects {
		if !index.hasObject(id) {
			return nil, nil, invalidRequest(fmt.Sprintf("ignore_objects references unknown object %s", id))
		}
		ignored[id] = true
	}
	for ruleIndex := range req.Objects {
		rule := req.Objects[ruleIndex]
		if rule.SourceID == "" {
			return nil, nil, invalidRequest("promotion object rule source_id is required")
		}
		if !index.hasObject(rule.SourceID) {
			return nil, nil, invalidRequest(fmt.Sprintf("promotion object rule references unknown source_id %s", rule.SourceID))
		}
		if _, exists := rules[rule.SourceID]; exists {
			return nil, nil, invalidRequest(fmt.Sprintf("duplicate promotion rule for source_id %s", rule.SourceID))
		}
		rules[rule.SourceID] = &rule
		if rule.Ignore {
			ignored[rule.SourceID] = true
		}
	}
	return rules, ignored, nil
}

func promotionTargetIDs(objects []Object, rules map[string]*ObjectPromotionRule, ignored map[string]bool) (map[string]string, error) {
	targetBySource := make(map[string]string, len(objects))
	used := map[string]string{}
	for _, object := range objects {
		if ignored[object.ID] {
			continue
		}
		targetID := object.ID
		if rule := rules[object.ID]; rule != nil && rule.TargetID != "" {
			targetID = cleanID(rule.TargetID)
		}
		if previousSource := used[targetID]; previousSource != "" {
			return nil, invalidRequest(fmt.Sprintf("duplicate target_id %s for source_id %s and %s", targetID, previousSource, object.ID))
		}
		used[targetID] = object.ID
		targetBySource[object.ID] = targetID
	}
	return targetBySource, nil
}

func promoteObject(object Object, rule *ObjectPromotionRule, targetBySource map[string]string, ignored map[string]bool, vocabulary AuthoredVocabulary) Object {
	promoted := object
	promoted.ID = targetBySource[object.ID]
	promoted.Parent = promotedParent(object.Parent, rule, targetBySource, ignored)
	if rule != nil {
		if rule.Role != "" {
			promoted.Role = resolveRoleAlias(rule.Role, vocabulary)
		}
		if rule.DomainRole != "" {
			domainRole := resolveRoleAlias(rule.DomainRole, vocabulary)
			promoted.DomainRole = &domainRole
		}
		if rule.Kind != "" {
			promoted.Kind = rule.Kind
		}
		if rule.Importance != "" {
			promoted.Importance = rule.Importance
		}
		if rule.SemanticDescription != "" {
			promoted.SemanticDescription = rule.SemanticDescription
		}
	}
	return promoted
}

func promotedParent(parentID string, rule *ObjectPromotionRule, targetBySource map[string]string, ignored map[string]bool) string {
	if rule != nil && rule.ParentID != "" {
		return cleanID(rule.ParentID)
	}
	if targetID := targetBySource[parentID]; targetID != "" {
		return targetID
	}
	if ignored[parentID] || parentID == "" {
		return "viewport"
	}
	return parentID
}

func promotedChildren(objects []Object, targetBySource map[string]string, ignored map[string]bool) map[string][]string {
	children := map[string][]string{}
	for _, object := range objects {
		targetID := targetBySource[object.ID]
		if targetID == "" {
			continue
		}
		parentID := promotedParent(object.Parent, nil, targetBySource, ignored)
		if parentID != "" && parentID != "viewport" {
			children[parentID] = append(children[parentID], targetID)
		}
	}
	return children
}

func promoteLayers(layers []Layer, targetBySource map[string]string, ignored map[string]bool) []Layer {
	promoted := make([]Layer, 0, len(layers))
	for _, layer := range layers {
		copy := layer
		copy.Contains = nil
		for _, objectID := range layer.Contains {
			if ignored[objectID] {
				continue
			}
			if targetID := targetBySource[objectID]; targetID != "" {
				copy.Contains = append(copy.Contains, targetID)
			}
		}
		if len(copy.Contains) > 0 {
			promoted = append(promoted, copy)
		}
	}
	return promoted
}

func promotionEvidence(objects []Object) EvidenceSet {
	records := make([]EvidenceRecord, 0, len(objects))
	totalConfidence := 0.0
	for _, object := range objects {
		totalConfidence += object.Confidence
		records = append(records, EvidenceRecord{
			ID:         "promotion:" + object.ID,
			Kind:       "contract_promotion",
			SourceRef:  object.ID,
			ObjectRefs: []string{object.ID},
			Confidence: object.Confidence,
		})
	}
	return EvidenceSet{
		SourceType:        "contract_promotion",
		GeneratedBy:       "visual-contract-service",
		OverallConfidence: totalConfidence / float64(len(objects)),
		Records:           records,
	}
}

func isImportantGeneratedObject(object Object) bool {
	return object.Importance == ImportanceCritical || object.Importance == ImportanceMajor || object.Confidence >= 0.85
}

func projectHasRole(project *Project, role string) bool {
	for _, candidate := range project.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}
