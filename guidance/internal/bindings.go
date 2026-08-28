package guidance

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	DefaultBindingListLimit = 50
	MaxBindingListLimit     = 200
	MaxResolvedBindings     = 100
	MaxInlineBindingBudget  = 4096
)

type KnowledgeReader interface {
	GetKnowledge(ctx context.Context, slug string) (*KnowledgeMetadata, error)
}

type KnowledgeMetadata struct {
	Slug            string
	Title           string
	Summary         string
	Kind            string
	Status          string
	CurationState   string
	ReplacementSlug string
	UpdatedAt       time.Time
	LastReviewedAt  *time.Time
	Revision        int
	RevisionKnown   bool
}

type BindingScope struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type BindingResolveQuery struct {
	Scopes       []BindingScope
	Audience     []string
	InlineBudget int
}

type ResolvedKnowledgeBinding struct {
	Binding         KnowledgeBinding
	SelectionSource string
	Target          *KnowledgeMetadata
	TargetState     string
	InlineAdmitted  bool
}

type BindingResolution struct {
	ResolvedAt       time.Time
	Selections       []ResolvedKnowledgeBinding
	ExcludedBindings []ResolvedKnowledgeBinding
	InlineBudget     int
	InlineUsed       int
	Truncated        bool
}

type (
	BindingListQuery  struct{ Limit, Offset int }
	BindingListResult struct {
		Bindings   []KnowledgeBinding
		NextOffset *int
	}
)

type BindingStore interface {
	CreateBinding(ctx context.Context, binding *KnowledgeBinding) (*KnowledgeBinding, error)
	GetBinding(ctx context.Context, bindingID int64) (*KnowledgeBinding, error)
	ListBindings(ctx context.Context, query BindingListQuery) (BindingListResult, error)
	UpdateBinding(ctx context.Context, binding *KnowledgeBinding) (*KnowledgeBinding, error)
	DeleteBinding(ctx context.Context, bindingID int64) (bool, error)
}

func (s *Service) CreateKnowledgeBinding(ctx context.Context, req CreateKnowledgeBindingRequest) (*KnowledgeBinding, error) {
	if err := s.requireBindings(true); err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	binding, err := NewKnowledgeBinding(KnowledgeBindingParams{
		TargetKind: req.TargetKind, TargetRef: req.TargetRef, ScopeKind: req.ScopeKind, ScopeRef: req.ScopeRef,
		Audience: req.Audience, Priority: req.Priority, SortOrder: req.SortOrder, ReadPolicy: req.ReadPolicy,
		ReadWhen: req.ReadWhen, ShadowsGlobal: req.ShadowsGlobal, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	if binding.ScopeKind == ScopeProject || binding.ScopeKind == ScopeGlobal {
		if err := s.projects.AssertWritable(ctx, binding.ScopeRef); err != nil {
			return nil, err
		}
	}
	if _, err := s.knowledge.GetKnowledge(ctx, binding.TargetRef); err != nil {
		return nil, err
	}
	created, err := s.bindings.CreateBinding(ctx, binding)
	if errors.Is(err, ErrDuplicateBinding) {
		return nil, NewServiceError(err, "knowledge_binding_conflict", 409)
	}
	return created, err
}

func (s *Service) GetKnowledgeBinding(ctx context.Context, bindingID int64) (*KnowledgeBinding, error) {
	if err := s.requireBindings(false); err != nil {
		return nil, err
	}
	if bindingID <= 0 {
		return nil, validationFailed(ErrBindingNotFound)
	}
	binding, err := s.bindings.GetBinding(ctx, bindingID)
	if errors.Is(err, ErrBindingNotFound) {
		return nil, bindingNotFound(bindingID)
	}
	return binding, err
}

func (s *Service) ListKnowledgeBindings(ctx context.Context, query BindingListQuery) (BindingListResult, error) {
	if err := s.requireBindings(false); err != nil {
		return BindingListResult{}, err
	}
	if query.Offset < 0 {
		return BindingListResult{}, validationFailed(errors.New("offset must not be negative"))
	}
	if query.Limit <= 0 {
		query.Limit = DefaultBindingListLimit
	}
	if query.Limit > MaxBindingListLimit {
		return BindingListResult{}, validationFailed(fmt.Errorf("limit must be at most %d", MaxBindingListLimit))
	}
	return s.bindings.ListBindings(ctx, query)
}

func (s *Service) UpdateKnowledgeBinding(ctx context.Context, bindingID int64, req UpdateKnowledgeBindingRequest) (*KnowledgeBinding, error) {
	if err := s.requireBindings(false); err != nil {
		return nil, err
	}
	if bindingID <= 0 {
		return nil, validationFailed(ErrBindingNotFound)
	}
	existing, err := s.bindings.GetBinding(ctx, bindingID)
	if errors.Is(err, ErrBindingNotFound) {
		return nil, bindingNotFound(bindingID)
	}
	if err != nil {
		return nil, err
	}
	if err := s.assertScopeWritable(ctx, existing.ScopeKind, existing.ScopeRef); err != nil {
		return nil, err
	}
	next, err := NewKnowledgeBinding(KnowledgeBindingParams{
		ID: existing.ID, TargetKind: existing.TargetKind, TargetRef: existing.TargetRef, ScopeKind: req.ScopeKind, ScopeRef: req.ScopeRef,
		Audience: req.Audience, Priority: req.Priority, SortOrder: req.SortOrder, ReadPolicy: req.ReadPolicy, ReadWhen: req.ReadWhen,
		ShadowsGlobal: req.ShadowsGlobal, CreatedAt: existing.CreatedAt, UpdatedAt: s.clock().UTC(),
	})
	if err != nil {
		return nil, validationFailed(err)
	}
	if err := s.assertScopeWritable(ctx, next.ScopeKind, next.ScopeRef); err != nil {
		return nil, err
	}
	updated, err := s.bindings.UpdateBinding(ctx, next)
	if errors.Is(err, ErrDuplicateBinding) {
		return nil, NewServiceError(err, "knowledge_binding_conflict", 409)
	}
	return updated, err
}

func (s *Service) DeleteKnowledgeBinding(ctx context.Context, bindingID int64) error {
	if err := s.requireBindings(false); err != nil {
		return err
	}
	if bindingID <= 0 {
		return validationFailed(ErrBindingNotFound)
	}
	existing, err := s.bindings.GetBinding(ctx, bindingID)
	if errors.Is(err, ErrBindingNotFound) {
		return bindingNotFound(bindingID)
	}
	if err != nil {
		return err
	}
	if err := s.assertScopeWritable(ctx, existing.ScopeKind, existing.ScopeRef); err != nil {
		return err
	}
	deleted, err := s.bindings.DeleteBinding(ctx, bindingID)
	if err != nil {
		return err
	}
	if !deleted {
		return bindingNotFound(bindingID)
	}
	return nil
}

func (s *Service) ResolveKnowledgeBindings(ctx context.Context, query BindingResolveQuery) (BindingResolution, error) {
	if err := s.requireBindings(true); err != nil {
		return BindingResolution{}, err
	}
	scopes, err := normalizedScopes(query.Scopes)
	if err != nil {
		return BindingResolution{}, validationFailed(err)
	}
	audience, err := normalizeAudience(query.Audience)
	if err != nil {
		return BindingResolution{}, validationFailed(err)
	}
	budget := query.InlineBudget
	if budget < 0 || budget > MaxInlineBindingBudget {
		return BindingResolution{}, validationFailed(fmt.Errorf("inline_budget must be between 0 and %d", MaxInlineBindingBudget))
	}
	// A zero store limit means scan all rows: shadowing must see every applicable
	// global/local pair before the response ceiling is applied.
	listed, err := s.bindings.ListBindings(ctx, BindingListQuery{})
	if err != nil {
		return BindingResolution{}, err
	}
	applicable := make([]KnowledgeBinding, 0, len(listed.Bindings))
	for _, binding := range listed.Bindings {
		if _, ok := scopes[binding.ScopeKind+"\x00"+binding.ScopeRef]; ok && audienceMatches(binding.Audience, audience) {
			applicable = append(applicable, binding)
		}
	}
	sortBindings(applicable)
	shadowed := globalShadowedTargets(applicable)
	resolution := BindingResolution{ResolvedAt: s.clock().UTC(), InlineBudget: budget, Selections: []ResolvedKnowledgeBinding{}, ExcludedBindings: []ResolvedKnowledgeBinding{}, Truncated: len(applicable) > MaxResolvedBindings}
	applicable = capApplicableBindings(applicable, MaxResolvedBindings)
	metadataByTarget := make(map[string]*KnowledgeMetadata)
	for _, binding := range applicable {
		if binding.ScopeKind == ScopeGlobal && shadowed[binding.TargetRef] {
			resolution.ExcludedBindings = append(resolution.ExcludedBindings, ResolvedKnowledgeBinding{Binding: binding, SelectionSource: "knowledge_binding", TargetState: "shadowed_by_applicable_binding"})
			continue
		}
		metadata, found := metadataByTarget[binding.TargetRef]
		var err error
		if !found {
			metadata, err = s.knowledge.GetKnowledge(ctx, binding.TargetRef)
			if err == nil {
				metadataByTarget[binding.TargetRef] = metadata
			}
		}
		if err != nil {
			if errors.Is(err, ErrKnowledgeTargetNotFound) {
				resolution.ExcludedBindings = append(resolution.ExcludedBindings, ResolvedKnowledgeBinding{Binding: binding, SelectionSource: "knowledge_binding", TargetState: "missing"})
				continue
			}
			return BindingResolution{}, err
		}
		state := knowledgeTargetState(metadata)
		item := ResolvedKnowledgeBinding{Binding: binding, SelectionSource: "knowledge_binding", Target: metadata, TargetState: state}
		if state == "archived" || state == "superseded" {
			resolution.ExcludedBindings = append(resolution.ExcludedBindings, item)
			continue
		}
		if binding.ReadPolicy == ReadPolicyInline {
			cost := len(metadata.Summary)
			if cost > 0 && cost <= budget-resolution.InlineUsed {
				item.InlineAdmitted = true
				resolution.InlineUsed += cost
			}
		}
		resolution.Selections = append(resolution.Selections, item)
	}
	sortResolvedBindings(resolution.Selections)
	sortResolvedBindings(resolution.ExcludedBindings)
	return resolution, nil
}

func capApplicableBindings(items []KnowledgeBinding, limit int) []KnowledgeBinding {
	if len(items) <= limit {
		return items
	}
	selected := append([]KnowledgeBinding(nil), items[:limit]...)
	nextReplace := len(selected) - 1
	for _, candidate := range items[limit:] {
		if candidate.ReadPolicy != ReadPolicyMustRead {
			continue
		}
		for nextReplace >= 0 && selected[nextReplace].ReadPolicy == ReadPolicyMustRead {
			nextReplace--
		}
		if nextReplace < 0 {
			break
		}
		selected[nextReplace] = candidate
		nextReplace--
	}
	sortBindings(selected)
	return selected
}

// ResolveContext is the incremental migration seam. It keeps legacy document
// Guidance rows in their current table and projects them into the same
// inspectable ordering without calling them Knowledge.
func (s *Service) ResolveContext(ctx context.Context, projectID string, query BindingResolveQuery) (BindingResolution, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return BindingResolution{}, validationFailed(ErrMissingProjectID)
	}
	for _, scope := range query.Scopes {
		if strings.TrimSpace(scope.Kind) == ScopeProject && strings.TrimSpace(scope.Ref) != projectID {
			return BindingResolution{}, validationFailed(errors.New("project scope must match project_id"))
		}
	}
	query.Scopes = append(query.Scopes, BindingScope{Kind: ScopeProject, Ref: projectID})
	resolution, err := s.ResolveKnowledgeBindings(ctx, query)
	if err != nil {
		return BindingResolution{}, err
	}
	entries, err := s.store.ListEntries(ctx, projectID, projectID != GlobalProjectID)
	if err != nil {
		return BindingResolution{}, err
	}
	for _, entry := range entries {
		if !audienceMatches(entry.Audience, query.Audience) {
			continue
		}
		policy := ReadPolicyOnDemand
		if entry.Importance == ImportanceRequired {
			policy = ReadPolicyMustRead
		}
		resolution.Selections = append(resolution.Selections, ResolvedKnowledgeBinding{
			Binding:         KnowledgeBinding{ID: entry.ID, TargetKind: "document", TargetRef: entry.DocumentProjectID + "/" + entry.DocumentSlug, ScopeKind: legacyScopeKind(entry.ProjectID), ScopeRef: entry.ProjectID, Audience: append([]string(nil), entry.Audience...), SortOrder: entry.SortOrder, ReadPolicy: policy, ReadWhen: entry.Notes, CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt},
			SelectionSource: "legacy_document_guidance", TargetState: "active",
		})
	}
	sortResolvedBindings(resolution.Selections)
	if len(resolution.Selections) > MaxResolvedBindings {
		resolution.Selections = capResolvedBindings(resolution.Selections, MaxResolvedBindings)
		resolution.Truncated = true
	}
	return resolution, nil
}

func capResolvedBindings(items []ResolvedKnowledgeBinding, limit int) []ResolvedKnowledgeBinding {
	selected := append([]ResolvedKnowledgeBinding(nil), items[:limit]...)
	nextReplace := len(selected) - 1
	for _, candidate := range items[limit:] {
		if candidate.Binding.ReadPolicy != ReadPolicyMustRead {
			continue
		}
		for nextReplace >= 0 && selected[nextReplace].Binding.ReadPolicy == ReadPolicyMustRead {
			nextReplace--
		}
		if nextReplace < 0 {
			break
		}
		selected[nextReplace] = candidate
		nextReplace--
	}
	sortResolvedBindings(selected)
	return selected
}

func (s *Service) requireBindings(requireKnowledge bool) error {
	if s.bindings == nil || (requireKnowledge && s.knowledge == nil) {
		return NewServiceError(errors.New("knowledge bindings are not configured"), "knowledge_bindings_unconfigured", 500)
	}
	return nil
}

func (s *Service) assertScopeWritable(ctx context.Context, kind, ref string) error {
	if kind == ScopeProject || kind == ScopeGlobal {
		return s.projects.AssertWritable(ctx, ref)
	}
	return nil
} // task/profile/capability stay at the trusted service-token boundary.

func legacyScopeKind(projectID string) string {
	if projectID == GlobalProjectID {
		return ScopeGlobal
	}
	return ScopeProject
}

func normalizedScopes(requested []BindingScope) (map[string]struct{}, error) {
	result := map[string]struct{}{ScopeGlobal + "\x00" + GlobalScopeRef: {}}
	for _, scope := range requested {
		kind, ref := strings.TrimSpace(scope.Kind), strings.TrimSpace(scope.Ref)
		if err := validateBindingScope(kind, ref, false); err != nil {
			return nil, err
		}
		result[kind+"\x00"+ref] = struct{}{}
	}
	return result, nil
}

func globalShadowedTargets(bindings []KnowledgeBinding) map[string]bool {
	global := make(map[string]bool)
	for _, binding := range bindings {
		if binding.ScopeKind == ScopeGlobal {
			global[binding.TargetRef] = true
		}
	}
	result := make(map[string]bool)
	for _, binding := range bindings {
		if binding.ScopeKind != ScopeGlobal && binding.ShadowsGlobal && global[binding.TargetRef] {
			result[binding.TargetRef] = true
		}
	}
	return result
}

func sortResolvedBindings(items []ResolvedKnowledgeBinding) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].Binding, items[j].Binding
		if scopeSpecificity(left.ScopeKind) != scopeSpecificity(right.ScopeKind) {
			return scopeSpecificity(left.ScopeKind) > scopeSpecificity(right.ScopeKind)
		}
		if readPolicyRank(left.ReadPolicy) != readPolicyRank(right.ReadPolicy) {
			return readPolicyRank(left.ReadPolicy) < readPolicyRank(right.ReadPolicy)
		}
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		if left.TargetRef != right.TargetRef {
			return left.TargetRef < right.TargetRef
		}
		return left.ID < right.ID
	})
}

func sortBindings(items []KnowledgeBinding) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if scopeSpecificity(left.ScopeKind) != scopeSpecificity(right.ScopeKind) {
			return scopeSpecificity(left.ScopeKind) > scopeSpecificity(right.ScopeKind)
		}
		if readPolicyRank(left.ReadPolicy) != readPolicyRank(right.ReadPolicy) {
			return readPolicyRank(left.ReadPolicy) < readPolicyRank(right.ReadPolicy)
		}
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		if left.TargetRef != right.TargetRef {
			return left.TargetRef < right.TargetRef
		}
		return left.ID < right.ID
	})
}

func scopeSpecificity(scope string) int {
	switch scope {
	case ScopeTask:
		return 5
	case ScopeAgentProfile:
		return 4
	case ScopeCapability:
		return 3
	case ScopeProject:
		return 2
	default:
		return 1
	}
}

func readPolicyRank(policy string) int {
	switch policy {
	case ReadPolicyMustRead:
		return 0
	case ReadPolicyInline:
		return 1
	case ReadPolicyOnDemand:
		return 2
	default:
		return 3
	}
}

func knowledgeTargetState(target *KnowledgeMetadata) string {
	if target.Status == "archived" {
		return "archived"
	}
	if target.ReplacementSlug != "" {
		return "superseded"
	}
	if target.Status == "deprecated" {
		return "deprecated"
	}
	return "active"
}

func bindingNotFound(bindingID int64) error {
	return NewServiceError(fmt.Errorf("%w: %d", ErrBindingNotFound, bindingID), "knowledge_binding_not_found", 404)
}

var ErrKnowledgeTargetNotFound = errors.New("knowledge target not found") //nolint:gochecknoglobals
