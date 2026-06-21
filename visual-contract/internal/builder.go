package visualcontract

import "fmt"

type AuthoredBuildRequest struct {
	Contract    Contract             `json:"contract"`
	Vocabulary  AuthoredVocabulary   `json:"vocabulary,omitempty"`
	Constraints []AuthoredConstraint `json:"constraints"`
}

type AuthoredVocabulary struct {
	RoleAliases map[string]string `json:"role_aliases,omitempty"`
}

type AuthoredConstraint struct {
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

type AuthoredBuildResponse struct {
	Contract Contract `json:"contract"`
}

func BuildAuthoredContract(req AuthoredBuildRequest) (*Contract, error) {
	if err := ValidateContract(&req.Contract); err != nil {
		return nil, err
	}
	if len(req.Constraints) == 0 {
		return nil, invalidRequest("at least one authored constraint is required")
	}
	index, err := buildContractIndex(&req.Contract)
	if err != nil {
		return nil, err
	}
	compiled := make([]Constraint, 0, len(req.Constraints))
	for _, authored := range req.Constraints {
		constraint, err := compileAuthoredConstraint(authored, req.Vocabulary, index)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, constraint)
	}
	output := req.Contract
	output.Constraints = append(append([]Constraint(nil), req.Contract.Constraints...), compiled...)
	if err := ValidateContract(&output); err != nil {
		return nil, err
	}
	return &output, nil
}

func compileAuthoredConstraint(authored AuthoredConstraint, vocabulary AuthoredVocabulary, index *contractIndex) (Constraint, error) {
	if authored.ID == "" {
		return Constraint{}, invalidRequest("authored constraint id is required")
	}
	if !authored.Type.IsValid() {
		return Constraint{}, invalidRequest(fmt.Sprintf("authored constraint %s has unsupported type %s", authored.ID, authored.Type))
	}
	importance := authored.Importance
	if importance == "" {
		importance = ImportanceMajor
	}
	if !importance.IsValid() {
		return Constraint{}, invalidRequest(fmt.Sprintf("authored constraint %s has invalid importance %s", authored.ID, authored.Importance))
	}
	constraint := Constraint{
		ID:                   authored.ID,
		Type:                 authored.Type,
		Object:               authored.Object,
		Role:                 resolveRoleAlias(authored.Role, vocabulary),
		DomainRole:           resolveRoleAlias(authored.DomainRole, vocabulary),
		A:                    authored.A,
		B:                    authored.B,
		Relation:             authored.Relation,
		Items:                append([]string(nil), authored.Items...),
		Edge:                 authored.Edge,
		Importance:           importance,
		ToleranceNorm:        authored.ToleranceNorm,
		MinViewportAreaRatio: authored.MinViewportAreaRatio,
		MaxDeltaNorm:         authored.MaxDeltaNorm,
	}
	if err := validateAuthoredReferences(constraint, index); err != nil {
		return Constraint{}, err
	}
	return constraint, nil
}

func validateAuthoredReferences(constraint Constraint, index *contractIndex) error {
	switch constraint.Type {
	case ConstraintObjectExists:
		if constraint.Object != "" && !index.hasObject(constraint.Object) {
			return invalidRequest(fmt.Sprintf("authored constraint %s references unknown object %s", constraint.ID, constraint.Object))
		}
		if constraint.Object == "" && constraint.Role == "" && constraint.DomainRole == "" {
			return invalidRequest(fmt.Sprintf("authored constraint %s requires object, role, or domain_role", constraint.ID))
		}
		if constraint.Role != "" && !index.hasRole(constraint.Role) {
			return invalidRequest(fmt.Sprintf("authored constraint %s references unknown role %s", constraint.ID, constraint.Role))
		}
		if constraint.DomainRole != "" && !index.hasDomainRole(constraint.DomainRole) {
			return invalidRequest(fmt.Sprintf("authored constraint %s references unknown domain_role %s", constraint.ID, constraint.DomainRole))
		}
	case ConstraintLayoutRelation, ConstraintRelativePosition:
		if !index.hasObject(constraint.A) || !index.hasObject(constraint.B) {
			return invalidRequest(fmt.Sprintf("authored constraint %s relation objects must exist", constraint.ID))
		}
		if !constraint.Relation.IsValid() {
			return invalidRequest(fmt.Sprintf("authored constraint %s has invalid relation", constraint.ID))
		}
	case ConstraintAlignment:
		if len(constraint.Items) < 2 {
			return invalidRequest(fmt.Sprintf("authored constraint %s requires at least two alignment items", constraint.ID))
		}
		if !constraint.Edge.IsValid() {
			return invalidRequest(fmt.Sprintf("authored constraint %s has invalid alignment edge", constraint.ID))
		}
		for _, item := range constraint.Items {
			if !index.hasObject(item) {
				return invalidRequest(fmt.Sprintf("authored constraint %s references unknown item %s", constraint.ID, item))
			}
		}
	case ConstraintAreaRatio:
		if !index.hasObject(constraint.Object) {
			return invalidRequest(fmt.Sprintf("authored constraint %s references unknown object %s", constraint.ID, constraint.Object))
		}
		if constraint.MinViewportAreaRatio == nil {
			return invalidRequest(fmt.Sprintf("authored constraint %s requires min_viewport_area_ratio", constraint.ID))
		}
	case ConstraintBoundsTolerance:
		if !index.hasObject(constraint.Object) {
			return invalidRequest(fmt.Sprintf("authored constraint %s references unknown object %s", constraint.ID, constraint.Object))
		}
		if constraint.MaxDeltaNorm == nil {
			return invalidRequest(fmt.Sprintf("authored constraint %s requires max_delta_norm", constraint.ID))
		}
	case ConstraintContainment:
		if !index.hasObject(constraint.Object) {
			return invalidRequest(fmt.Sprintf("authored constraint %s references unknown object %s", constraint.ID, constraint.Object))
		}
	}
	return nil
}

func resolveRoleAlias(value string, vocabulary AuthoredVocabulary) string {
	if value == "" {
		return ""
	}
	if resolved := vocabulary.RoleAliases[value]; resolved != "" {
		return resolved
	}
	return value
}

func (i *contractIndex) hasRole(role string) bool {
	for _, object := range i.objects {
		if object.Role == role {
			return true
		}
	}
	return false
}

func (i *contractIndex) hasDomainRole(role string) bool {
	for _, object := range i.objects {
		if object.DomainRole != nil && *object.DomainRole == role {
			return true
		}
	}
	return false
}
