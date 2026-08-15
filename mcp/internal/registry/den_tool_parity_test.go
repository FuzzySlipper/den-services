package registry_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"den-services/mcp/internal/backend"
	"den-services/mcp/internal/registry"
)

type denToolCatalog struct {
	Revision string                `json:"revision"`
	Tools    []denToolCatalogEntry `json:"tools"`
}

type denToolCatalogEntry struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Backend      string          `json:"backend"`
	Operation    string          `json:"operation"`
	WorkflowTier string          `json:"workflow_tier"`
	Risk         string          `json:"risk"`
	InputSchema  json.RawMessage `json:"input_schema"`
}

func TestDenToolCatalogMatchesDirectMCPRegistry(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "cmd", "den-tool", "mcp_catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cli denToolCatalog
	if err := json.Unmarshal(data, &cli); err != nil {
		t.Fatal(err)
	}
	if cli.Revision != registry.ToolCatalogRevision {
		t.Fatalf("den-tool revision = %q, want %q; regenerate with go run ./mcp/cmd/den-tool-catalog -output ./cmd/den-tool/mcp_catalog.json", cli.Revision, registry.ToolCatalogRevision)
	}

	reg, err := registry.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	routes, err := backend.LoadRouteTable(filepath.Join("..", "..", "routes.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	listed := reg.Tools()
	wantNames := make([]string, 0, len(listed))
	wantByName := make(map[string]registry.ListedTool, len(listed))
	for _, item := range listed {
		wantNames = append(wantNames, item.Name)
		wantByName[item.Name] = item
	}
	gotNames := make([]string, 0, len(cli.Tools))
	for _, item := range cli.Tools {
		gotNames = append(gotNames, item.Name)
		listedTool, exists := wantByName[item.Name]
		if !exists {
			continue
		}
		definition, err := reg.Resolve(item.Name)
		if err != nil {
			t.Fatal(err)
		}
		route, err := routes.Resolve(definition.Operation)
		if err != nil && !errors.Is(err, backend.ErrRouteNotFound) {
			t.Fatal(err)
		}
		owningBackend := definition.Backend
		if err == nil {
			owningBackend = route.Backend
		}
		if item.Description != listedTool.Description || item.Backend != owningBackend || item.Operation != definition.Operation || item.WorkflowTier != string(listedTool.WorkflowTier) || !jsonBytesEqual(item.InputSchema, json.RawMessage(listedTool.InputSchema)) {
			t.Errorf("den-tool metadata for %s is stale", item.Name)
		}
		if item.Risk != "read" && item.Risk != "write" && item.Risk != "destructive" {
			t.Errorf("den-tool risk for %s = %q", item.Name, item.Risk)
		}
	}
	sort.Strings(wantNames)
	sort.Strings(gotNames)
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("den-tool MCP names do not match direct registry\n got: %v\nwant: %v\nregenerate with go run ./mcp/cmd/den-tool-catalog -output ./cmd/den-tool/mcp_catalog.json", gotNames, wantNames)
	}
}

func jsonBytesEqual(left, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && reflect.DeepEqual(leftValue, rightValue)
}
