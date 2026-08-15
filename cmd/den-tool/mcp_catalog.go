package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
)

//go:embed mcp_catalog.json
var embeddedMCPCatalog []byte

type mcpCatalog struct {
	Revision string           `json:"revision"`
	Tools    []mcpCatalogTool `json:"tools"`
}

type mcpCatalogTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Backend      string          `json:"backend"`
	Operation    string          `json:"operation"`
	WorkflowTier string          `json:"workflow_tier"`
	Risk         Risk            `json:"risk"`
	InputSchema  json.RawMessage `json:"input_schema"`
}

func ParseMCPCatalog(data []byte) (mcpCatalog, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var catalog mcpCatalog
	if err := decoder.Decode(&catalog); err != nil {
		return mcpCatalog{}, fmt.Errorf("decode MCP catalog: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return mcpCatalog{}, fmt.Errorf("decode MCP catalog: trailing data")
	}
	if catalog.Revision == "" || len(catalog.Tools) == 0 {
		return mcpCatalog{}, fmt.Errorf("decode MCP catalog: revision and tools are required")
	}
	seen := make(map[string]struct{}, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		if !validToolID(tool.Name) || tool.Description == "" || tool.Backend == "" || tool.Operation == "" || tool.WorkflowTier == "" || !validRisk(tool.Risk) || !json.Valid(tool.InputSchema) {
			return mcpCatalog{}, fmt.Errorf("decode MCP catalog: invalid tool %q", tool.Name)
		}
		if _, exists := seen[tool.Name]; exists {
			return mcpCatalog{}, fmt.Errorf("decode MCP catalog: duplicate tool %q", tool.Name)
		}
		seen[tool.Name] = struct{}{}
	}
	return catalog, nil
}

func appendMCPTools(catalog Catalog) (Catalog, error) {
	mcpTools, err := ParseMCPCatalog(embeddedMCPCatalog)
	if err != nil {
		return Catalog{}, err
	}
	for _, definition := range mcpTools.Tools {
		catalog.Tools = append(catalog.Tools, Tool{
			ID:               "den." + definition.Name,
			Description:      definition.Description,
			Tags:             []string{"den", "mcp-parity", definition.Backend, definition.WorkflowTier},
			Repo:             "den-services",
			Root:             "/home/dev/den-services",
			WorkingDirectory: "/home/dev/den-services",
			Argv:             []string{"den-tool", "den", definition.Name},
			Risk:             definition.Risk,
			Source:           "mcp registry " + mcpTools.Revision,
			Requirements:     []string{"den-tool installed on PATH", "Den MCP endpoint reachable through DEN_MCP_URL or the LAN default"},
			Examples:         []string{"den-tool den " + definition.Name + " --args-json '{...}'", "den-tool run den." + definition.Name + " -- --args-json '{...}'"},
			Backend:          definition.Backend,
			Operation:        definition.Operation,
			WorkflowTier:     definition.WorkflowTier,
			InputSchema:      definition.InputSchema,
		})
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}
