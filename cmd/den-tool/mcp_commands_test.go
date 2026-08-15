package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseToolArgumentsValidatesRequiredFieldsAndTypes(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string"},"task_id":{"type":"integer"},"verbose":{"type":["boolean","null"]},"tags":{"type":"array"}},"required":["project_id","task_id"],"additionalProperties":false}`)
	arguments, err := parseToolArguments(schema, []string{"--project-id", "den-services", "--task_id=7011", "--verbose", "true", "--tags", `["cli"]`})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(arguments, &got); err != nil {
		t.Fatal(err)
	}
	if got["project_id"] != "den-services" || got["task_id"] != float64(7011) || got["verbose"] != true {
		t.Fatalf("arguments = %#v", got)
	}

	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--project-id", "den-services"}, "missing required arguments: task_id"},
		{[]string{"--project-id", "den-services", "--task-id", "nope"}, "must be an integer"},
		{[]string{"--project-id", "den-services", "--task-id", "1", "--surprise", "x"}, "unknown argument"},
		{[]string{"--args-json", `{"project_id":"den-services","task_id":"nope"}`}, "wrong JSON type"},
	} {
		if _, err := parseToolArguments(schema, test.args); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("parseToolArguments(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func TestEmbeddedMCPCatalogIsIncludedInDiscovery(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog(defaultRepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	mcpCatalog, err := ParseMCPCatalog(embeddedMCPCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(mcpCatalog.Tools) != 86 {
		t.Fatalf("MCP catalog contains %d tools, want current direct-profile count 86", len(mcpCatalog.Tools))
	}
	for _, operation := range []string{"create_board_post", "create_task", "get_task_context", "wait_for_messages"} {
		tool, found := catalog.Find("den." + operation)
		if !found {
			t.Errorf("catalog omitted den.%s", operation)
			continue
		}
		if tool.Operation != operation || len(tool.InputSchema) == 0 {
			t.Errorf("den.%s metadata = %#v", operation, tool)
		}
	}
}
