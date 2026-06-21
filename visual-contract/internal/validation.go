package visualcontract

import "fmt"

func ValidateContract(contract *Contract) error {
	if contract == nil {
		return invalidContract("contract is required")
	}
	if contract.Schema != SchemaVersion {
		return invalidContract(fmt.Sprintf("schema must be %s", SchemaVersion))
	}
	if contract.Scene.ID == "" {
		return invalidContract("scene.id is required")
	}
	if contract.Scene.Viewport.WidthPX <= 0 || contract.Scene.Viewport.HeightPX <= 0 {
		return invalidContract("scene.viewport dimensions must be positive")
	}
	if len(contract.Spaces) == 0 {
		return invalidContract("at least one space is required")
	}
	if len(contract.Layers) == 0 {
		return invalidContract("at least one layer is required")
	}
	if len(contract.Objects) == 0 {
		return invalidContract("at least one object is required")
	}
	if contract.Evidence.SourceType == "" || contract.Evidence.GeneratedBy == "" {
		return invalidContract("evidence.source_type and evidence.generated_by are required")
	}
	if !validConfidence(contract.Evidence.OverallConfidence) {
		return invalidContract("evidence.overall_confidence must be between 0 and 1")
	}

	index, err := buildContractIndex(contract)
	if err != nil {
		return err
	}
	for _, relation := range contract.Relations {
		if err := validateRelation(relation, index); err != nil {
			return err
		}
	}
	for _, constraint := range contract.Constraints {
		if err := validateConstraint(constraint, index); err != nil {
			return err
		}
	}
	for _, evidence := range contract.Evidence.Records {
		if evidence.ID == "" {
			return invalidContract("evidence record id is required")
		}
		if !validConfidence(evidence.Confidence) {
			return invalidContract(fmt.Sprintf("evidence %s confidence must be between 0 and 1", evidence.ID))
		}
		for _, objectID := range evidence.ObjectRefs {
			if !index.hasObject(objectID) {
				return invalidContract(fmt.Sprintf("evidence %s references unknown object %s", evidence.ID, objectID))
			}
		}
	}
	return nil
}

type contractIndex struct {
	spaces   map[string]Space
	layers   map[string]Layer
	objects  map[string]Object
	evidence map[string]EvidenceRecord
}

func buildContractIndex(contract *Contract) (*contractIndex, error) {
	index := &contractIndex{
		spaces:   make(map[string]Space, len(contract.Spaces)),
		layers:   make(map[string]Layer, len(contract.Layers)),
		objects:  make(map[string]Object, len(contract.Objects)),
		evidence: make(map[string]EvidenceRecord, len(contract.Evidence.Records)),
	}
	for _, space := range contract.Spaces {
		if space.ID == "" {
			return nil, invalidContract("space id is required")
		}
		if _, exists := index.spaces[space.ID]; exists {
			return nil, invalidContract(fmt.Sprintf("duplicate space id %s", space.ID))
		}
		if err := validateBounds("space "+space.ID, space.Bounds, true); err != nil {
			return nil, err
		}
		index.spaces[space.ID] = space
	}
	for _, layer := range contract.Layers {
		if layer.ID == "" {
			return nil, invalidContract("layer id is required")
		}
		if _, exists := index.layers[layer.ID]; exists {
			return nil, invalidContract(fmt.Sprintf("duplicate layer id %s", layer.ID))
		}
		index.layers[layer.ID] = layer
	}
	for _, object := range contract.Objects {
		if object.ID == "" {
			return nil, invalidContract("object id is required")
		}
		if _, exists := index.objects[object.ID]; exists {
			return nil, invalidContract(fmt.Sprintf("duplicate object id %s", object.ID))
		}
		if object.Kind == "" || object.Role == "" {
			return nil, invalidContract(fmt.Sprintf("object %s kind and role are required", object.ID))
		}
		if object.Parent != "" && !index.hasSpace(object.Parent) && !index.hasObject(object.Parent) {
			return nil, invalidContract(fmt.Sprintf("object %s references unknown parent %s", object.ID, object.Parent))
		}
		if !index.hasLayer(object.Layer) {
			return nil, invalidContract(fmt.Sprintf("object %s references unknown layer %s", object.ID, object.Layer))
		}
		if !object.Importance.IsValid() {
			return nil, invalidContract(fmt.Sprintf("object %s has invalid importance %s", object.ID, object.Importance))
		}
		if !validConfidence(object.Confidence) {
			return nil, invalidContract(fmt.Sprintf("object %s confidence must be between 0 and 1", object.ID))
		}
		if err := validateBounds("object "+object.ID, object.Bounds, false); err != nil {
			return nil, err
		}
		if object.Bounds.Space != "" && !index.hasSpace(object.Bounds.Space) && !index.hasObject(object.Bounds.Space) {
			return nil, invalidContract(fmt.Sprintf("object %s bounds references unknown space %s", object.ID, object.Bounds.Space))
		}
		index.objects[object.ID] = object
	}
	for _, layer := range contract.Layers {
		for _, objectID := range layer.Contains {
			if !index.hasObject(objectID) {
				return nil, invalidContract(fmt.Sprintf("layer %s references unknown object %s", layer.ID, objectID))
			}
		}
	}
	for _, object := range contract.Objects {
		for _, childID := range object.Children {
			if !index.hasObject(childID) {
				return nil, invalidContract(fmt.Sprintf("object %s references unknown child %s", object.ID, childID))
			}
		}
	}
	for _, evidence := range contract.Evidence.Records {
		if evidence.ID != "" {
			if _, exists := index.evidence[evidence.ID]; exists {
				return nil, invalidContract(fmt.Sprintf("duplicate evidence id %s", evidence.ID))
			}
			index.evidence[evidence.ID] = evidence
		}
	}
	for _, object := range contract.Objects {
		for _, evidenceRef := range object.EvidenceRefs {
			if !index.hasEvidence(evidenceRef) {
				return nil, invalidContract(fmt.Sprintf("object %s references unknown evidence %s", object.ID, evidenceRef))
			}
		}
	}
	return index, nil
}

func validateRelation(relation Relation, index *contractIndex) error {
	if !relation.Type.IsValid() {
		return invalidContract(fmt.Sprintf("relation has invalid type %s", relation.Type))
	}
	if relation.A != "" && !index.hasObject(relation.A) {
		return invalidContract(fmt.Sprintf("relation references unknown object %s", relation.A))
	}
	if relation.B != "" && !index.hasObject(relation.B) {
		return invalidContract(fmt.Sprintf("relation references unknown object %s", relation.B))
	}
	for _, item := range relation.Items {
		if !index.hasObject(item) {
			return invalidContract(fmt.Sprintf("relation references unknown item %s", item))
		}
	}
	if relation.A == "" && len(relation.Items) == 0 {
		return invalidContract("relation requires a or items")
	}
	if !validConfidence(relation.Confidence) {
		return invalidContract("relation confidence must be between 0 and 1")
	}
	if relation.EvidenceRef != "" && !index.hasEvidence(relation.EvidenceRef) {
		return invalidContract(fmt.Sprintf("relation references unknown evidence %s", relation.EvidenceRef))
	}
	return nil
}

func validateConstraint(constraint Constraint, index *contractIndex) error {
	if constraint.ID == "" {
		return invalidContract("constraint id is required")
	}
	if !constraint.Type.IsValid() {
		return invalidContract(fmt.Sprintf("constraint %s has invalid type %s", constraint.ID, constraint.Type))
	}
	if !constraint.Importance.IsValid() {
		return invalidContract(fmt.Sprintf("constraint %s has invalid importance %s", constraint.ID, constraint.Importance))
	}
	if constraint.Object != "" && !index.hasObject(constraint.Object) {
		return invalidContract(fmt.Sprintf("constraint %s references unknown object %s", constraint.ID, constraint.Object))
	}
	if constraint.A != "" && !index.hasObject(constraint.A) {
		return invalidContract(fmt.Sprintf("constraint %s references unknown object %s", constraint.ID, constraint.A))
	}
	if constraint.B != "" && !index.hasObject(constraint.B) {
		return invalidContract(fmt.Sprintf("constraint %s references unknown object %s", constraint.ID, constraint.B))
	}
	for _, item := range constraint.Items {
		if !index.hasObject(item) {
			return invalidContract(fmt.Sprintf("constraint %s references unknown item %s", constraint.ID, item))
		}
	}
	if constraint.Relation != "" && !constraint.Relation.IsValid() {
		return invalidContract(fmt.Sprintf("constraint %s has invalid relation %s", constraint.ID, constraint.Relation))
	}
	if constraint.Edge != "" && !constraint.Edge.IsValid() {
		return invalidContract(fmt.Sprintf("constraint %s has invalid edge %s", constraint.ID, constraint.Edge))
	}
	if constraint.ToleranceNorm != nil && *constraint.ToleranceNorm < 0 {
		return invalidContract(fmt.Sprintf("constraint %s tolerance_norm must be non-negative", constraint.ID))
	}
	if constraint.MinViewportAreaRatio != nil && (*constraint.MinViewportAreaRatio < 0 || *constraint.MinViewportAreaRatio > 1) {
		return invalidContract(fmt.Sprintf("constraint %s min_viewport_area_ratio must be between 0 and 1", constraint.ID))
	}
	if constraint.MaxDeltaNorm != nil && *constraint.MaxDeltaNorm < 0 {
		return invalidContract(fmt.Sprintf("constraint %s max_delta_norm must be non-negative", constraint.ID))
	}
	return nil
}

func validateBounds(label string, bounds Bounds, allowEmptySpace bool) error {
	if !allowEmptySpace && bounds.Space == "" {
		return invalidContract(fmt.Sprintf("%s bounds.space is required", label))
	}
	if bounds.X < 0 || bounds.Y < 0 || bounds.W <= 0 || bounds.H <= 0 || bounds.X+bounds.W > 1 || bounds.Y+bounds.H > 1 {
		return invalidContract(fmt.Sprintf("%s bounds must fit inside normalized 0..1 space", label))
	}
	if bounds.PX != nil && (bounds.PX.W <= 0 || bounds.PX.H <= 0) {
		return invalidContract(fmt.Sprintf("%s pixel bounds width and height must be positive", label))
	}
	return nil
}

func (i *contractIndex) hasSpace(id string) bool {
	_, ok := i.spaces[id]
	return ok
}

func (i *contractIndex) hasLayer(id string) bool {
	_, ok := i.layers[id]
	return ok
}

func (i *contractIndex) hasObject(id string) bool {
	_, ok := i.objects[id]
	return ok
}

func (i *contractIndex) hasEvidence(id string) bool {
	_, ok := i.evidence[id]
	return ok
}

func validConfidence(confidence float64) bool {
	return confidence >= 0 && confidence <= 1
}
