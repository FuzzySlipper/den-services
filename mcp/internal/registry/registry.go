package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type Registry struct {
	tools  []ToolDefinition
	byName map[string]registeredTool
}

// WorkflowTier describes how a tool participates in a managed workflow.
// Primitive tools are low-level orchestration building blocks; operator tools
// are normal Den reads/writes; green-path tools are the canonical managed
// workflow entry points supplied by an owning runtime.
type WorkflowTier string

const (
	WorkflowTierPrimitive WorkflowTier = "primitive"
	WorkflowTierOperator  WorkflowTier = "operator"
	WorkflowTierGreenPath WorkflowTier = "green_path"
)

// ToolProfile selects the model-facing discovery projection. It never changes
// backend authority or whether a registered operation can be called directly.
type ToolProfile string

const (
	ToolProfileDirect         ToolProfile = "direct"
	ToolProfileManagedRuntime ToolProfile = "managed-runtime"
	ToolCatalogRevision                   = "mcp-catalog-v3"
)

// CatalogMetadata is returned alongside tools/list so managed runtimes can
// verify that they received the intended discovery projection without relying
// on tool-name ordering or prose descriptions.
type CatalogMetadata struct {
	Profile             ToolProfile          `json:"toolProfile"`
	Revision            string               `json:"catalogRevision"`
	VisibleToolCount    int                  `json:"visibleToolCount"`
	WorkflowTiers       map[WorkflowTier]int `json:"workflowTiers"`
	HiddenToolCount     int                  `json:"hiddenToolCount"`
	HiddenWorkflowTiers []WorkflowTier       `json:"hiddenWorkflowTiers,omitempty"`
}

type ToolDefinition struct {
	Name               string
	Description        string
	InputSchema        Schema
	Backend            string
	Operation          string
	Execution          json.RawMessage
	WorkflowTier       WorkflowTier
	Hidden             bool
	TombstoneMessage   string
	Deprecated         bool
	DeprecationMessage string
	Aliases            []ToolAlias
}

type ToolAlias struct {
	Name               string
	Description        string
	Deprecated         bool
	DeprecationMessage string
}

type registeredTool struct {
	tool  ToolDefinition
	alias *ToolAlias
}

type ListedTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  Schema          `json:"inputSchema"`
	Execution    json.RawMessage `json:"execution,omitempty"`
	WorkflowTier WorkflowTier    `json:"workflowTier"`
	Annotations  *Annotations    `json:"annotations,omitempty"`
}

type Annotations struct {
	Deprecated         bool   `json:"deprecated,omitempty"`
	DeprecationMessage string `json:"deprecationMessage,omitempty"`
	CanonicalName      string `json:"canonicalName,omitempty"`
}

var (
	ErrUnknownTool        = errors.New("unknown tool")
	ErrUnknownToolProfile = errors.New("unknown MCP tool profile")
	toolNameRegex         = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
)

// ParseToolProfile validates a discovery profile supplied by configuration or
// an MCP caller. An empty profile intentionally means the unrestricted direct
// Codex/CLI projection.
func ParseToolProfile(raw string) (ToolProfile, error) {
	profile := ToolProfile(strings.TrimSpace(strings.ToLower(raw)))
	if profile == "" {
		return ToolProfileDirect, nil
	}
	switch profile {
	case ToolProfileDirect, ToolProfileManagedRuntime:
		return profile, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownToolProfile, profile)
	}
}

func New(tools []ToolDefinition) (*Registry, error) {
	registry := &Registry{
		tools:  make([]ToolDefinition, 0, len(tools)),
		byName: make(map[string]registeredTool),
	}
	for _, tool := range tools {
		if err := registry.addTool(tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Tools() []ListedTool {
	listed, _ := r.ToolsForProfile(ToolProfileDirect)
	return listed
}

// ToolsForProfile returns the model-facing discovery projection for profile.
// Hidden definitions remain in the registry and can still be resolved for
// trusted service adapters.
func (r *Registry) ToolsForProfile(profile ToolProfile) ([]ListedTool, error) {
	profile, err := ParseToolProfile(string(profile))
	if err != nil {
		return nil, err
	}
	listed := make([]ListedTool, 0, len(r.byName))
	for _, tool := range r.tools {
		if !visibleForProfile(tool, profile) {
			continue
		}
		listed = append(listed, listedTool(tool, nil))
		for index := range tool.Aliases {
			alias := tool.Aliases[index]
			if !visibleForProfile(tool, profile) {
				continue
			}
			listed = append(listed, listedTool(tool, &alias))
		}
	}
	return listed, nil
}

// Catalog returns stable discovery metadata for profile.
func (r *Registry) Catalog(profile ToolProfile) (CatalogMetadata, error) {
	profile, err := ParseToolProfile(string(profile))
	if err != nil {
		return CatalogMetadata{}, err
	}
	listed, err := r.ToolsForProfile(profile)
	if err != nil {
		return CatalogMetadata{}, err
	}
	direct, err := r.ToolsForProfile(ToolProfileDirect)
	if err != nil {
		return CatalogMetadata{}, err
	}
	counts := map[WorkflowTier]int{
		WorkflowTierPrimitive: 0,
		WorkflowTierOperator:  0,
		WorkflowTierGreenPath: 0,
	}
	for _, tool := range listed {
		counts[tool.WorkflowTier]++
	}
	hiddenTiers := make([]WorkflowTier, 0, 1)
	if profile == ToolProfileManagedRuntime {
		hiddenTiers = append(hiddenTiers, WorkflowTierPrimitive)
	}
	return CatalogMetadata{
		Profile:             profile,
		Revision:            ToolCatalogRevision,
		VisibleToolCount:    len(listed),
		WorkflowTiers:       counts,
		HiddenToolCount:     len(direct) - len(listed),
		HiddenWorkflowTiers: hiddenTiers,
	}, nil
}

func (r *Registry) Resolve(name string) (ToolDefinition, error) {
	registered, ok := r.byName[name]
	if !ok {
		return ToolDefinition{}, fmt.Errorf("%w: %s", ErrUnknownTool, name)
	}
	return registered.tool, nil
}

func (r *Registry) addTool(tool ToolDefinition) error {
	if tool.WorkflowTier == "" {
		tool.WorkflowTier = WorkflowTierOperator
	}
	if !validWorkflowTier(tool.WorkflowTier) {
		return fmt.Errorf("tool %s has invalid workflow tier %q", tool.Name, tool.WorkflowTier)
	}
	if err := validateTool(tool); err != nil {
		return err
	}
	if err := r.registerName(tool.Name, registeredTool{tool: tool}); err != nil {
		return err
	}
	for index := range tool.Aliases {
		alias := tool.Aliases[index]
		if err := validateAlias(tool.Name, alias); err != nil {
			return err
		}
		if err := r.registerName(alias.Name, registeredTool{tool: tool, alias: &alias}); err != nil {
			return err
		}
	}
	r.tools = append(r.tools, tool)
	return nil
}

func (r *Registry) registerName(name string, tool registeredTool) error {
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("duplicate tool name %q", name)
	}
	r.byName[name] = tool
	return nil
}

func validateTool(tool ToolDefinition) error {
	if !toolNameRegex.MatchString(tool.Name) {
		return fmt.Errorf("invalid tool name %q", tool.Name)
	}
	if tool.Description == "" {
		return fmt.Errorf("tool %s description is required", tool.Name)
	}
	if tool.Backend == "" {
		return fmt.Errorf("tool %s backend is required", tool.Name)
	}
	if tool.Operation == "" {
		return fmt.Errorf("tool %s operation is required", tool.Name)
	}
	return nil
}

func validateAlias(canonicalName string, alias ToolAlias) error {
	if !toolNameRegex.MatchString(alias.Name) {
		return fmt.Errorf("invalid alias name %q for %s", alias.Name, canonicalName)
	}
	if alias.Name == canonicalName {
		return fmt.Errorf("alias %q duplicates canonical tool name", alias.Name)
	}
	return nil
}

func listedTool(tool ToolDefinition, alias *ToolAlias) ListedTool {
	if alias == nil {
		return ListedTool{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			Execution:    tool.Execution,
			WorkflowTier: tool.WorkflowTier,
			Annotations:  annotations(tool.Deprecated, tool.DeprecationMessage, ""),
		}
	}
	description := alias.Description
	if description == "" {
		description = "Compatibility alias for " + tool.Name + "."
	}
	return ListedTool{
		Name:         alias.Name,
		Description:  description,
		InputSchema:  tool.InputSchema,
		Execution:    tool.Execution,
		WorkflowTier: tool.WorkflowTier,
		Annotations:  annotations(alias.Deprecated, alias.DeprecationMessage, tool.Name),
	}
}

func visibleForProfile(tool ToolDefinition, profile ToolProfile) bool {
	if tool.Hidden {
		return false
	}
	return !(profile == ToolProfileManagedRuntime && tool.WorkflowTier == WorkflowTierPrimitive)
}

func validWorkflowTier(tier WorkflowTier) bool {
	switch tier {
	case WorkflowTierPrimitive, WorkflowTierOperator, WorkflowTierGreenPath:
		return true
	default:
		return false
	}
}

func annotations(deprecated bool, message string, canonicalName string) *Annotations {
	if !deprecated && message == "" && canonicalName == "" {
		return nil
	}
	return &Annotations{
		Deprecated:         deprecated,
		DeprecationMessage: message,
		CanonicalName:      canonicalName,
	}
}
