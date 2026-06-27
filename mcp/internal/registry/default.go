package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

const denCoreBackend = "den-core"

//go:embed testdata/live_tools_20260627.json
var liveToolsSnapshot []byte

type liveToolSnapshot struct {
	Tools []liveTool `json:"tools"`
}

type liveTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema Schema          `json:"inputSchema"`
	Execution   json.RawMessage `json:"execution,omitempty"`
}

func DefaultRegistry() (*Registry, error) {
	tools, err := DefaultTools()
	if err != nil {
		return nil, err
	}
	return New(tools)
}

// DefaultTools is the live den-mcp compatibility surface exposed by tools/list.
// Update testdata/live_tools_20260627.json intentionally whenever the old live
// MCP tool contract changes.
func DefaultTools() ([]ToolDefinition, error) {
	var snapshot liveToolSnapshot
	if err := json.Unmarshal(liveToolsSnapshot, &snapshot); err != nil {
		return nil, fmt.Errorf("parsing live MCP tool snapshot: %w", err)
	}
	tools := make([]ToolDefinition, 0, len(snapshot.Tools))
	for _, tool := range snapshot.Tools {
		tools = append(tools, ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Backend:     denCoreBackend,
			Operation:   tool.Name,
			InputSchema: tool.InputSchema,
			Execution:   tool.Execution,
		})
	}
	return tools, nil
}
