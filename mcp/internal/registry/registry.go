package registry

import (
	"errors"
	"fmt"
	"regexp"
)

type Registry struct {
	tools  []ToolDefinition
	byName map[string]registeredTool
}

type ToolDefinition struct {
	Name               string
	Description        string
	InputSchema        Schema
	Backend            string
	Operation          string
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
	Name        string       `json:"name"`
	Description string       `json:"description"`
	InputSchema Schema       `json:"inputSchema"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

type Annotations struct {
	Deprecated         bool   `json:"deprecated,omitempty"`
	DeprecationMessage string `json:"deprecationMessage,omitempty"`
	CanonicalName      string `json:"canonicalName,omitempty"`
}

var (
	ErrUnknownTool = errors.New("unknown tool")
	toolNameRegex  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
)

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
	listed := make([]ListedTool, 0, len(r.byName))
	for _, tool := range r.tools {
		listed = append(listed, listedTool(tool, nil))
		for index := range tool.Aliases {
			alias := tool.Aliases[index]
			listed = append(listed, listedTool(tool, &alias))
		}
	}
	return listed
}

func (r *Registry) Resolve(name string) (ToolDefinition, error) {
	registered, ok := r.byName[name]
	if !ok {
		return ToolDefinition{}, fmt.Errorf("%w: %s", ErrUnknownTool, name)
	}
	return registered.tool, nil
}

func (r *Registry) addTool(tool ToolDefinition) error {
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
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Annotations: annotations(tool.Deprecated, tool.DeprecationMessage, ""),
		}
	}
	description := alias.Description
	if description == "" {
		description = "Compatibility alias for " + tool.Name + "."
	}
	return ListedTool{
		Name:        alias.Name,
		Description: description,
		InputSchema: tool.InputSchema,
		Annotations: annotations(alias.Deprecated, alias.DeprecationMessage, tool.Name),
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
