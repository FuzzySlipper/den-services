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
	if len(mcpCatalog.Tools) != 94 {
		t.Fatalf("MCP catalog contains %d tools, want complete callable count 94", len(mcpCatalog.Tools))
	}
	for _, operation := range []string{"compose_assignment_manifest", "den_knowledge_card", "den_knowledge_read", "create_board_post", "create_task", "get_task_context", "wait_for_messages"} {
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

func TestEmbeddedMCPCatalogProjectsExposeRepositoryURL(t *testing.T) {
	catalog, err := ParseMCPCatalog(embeddedMCPCatalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"create_project", "update_project"} {
		var tool *mcpCatalogTool
		for index := range catalog.Tools {
			if catalog.Tools[index].Name == name {
				tool = &catalog.Tools[index]
				break
			}
		}
		if tool == nil {
			t.Fatalf("MCP catalog is missing %s", name)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("Unmarshal(%s schema) error = %v", name, err)
		}
		property, ok := schema.Properties["repository_url"]
		if !ok {
			t.Fatalf("%s schema is missing repository_url", name)
		}
		for _, required := range schema.Required {
			if required == "repository_url" {
				t.Fatalf("%s requires optional repository_url", name)
			}
		}
		var propertySchema struct {
			Type []string `json:"type"`
		}
		if err := json.Unmarshal(property, &propertySchema); err != nil {
			t.Fatalf("Unmarshal(%s.repository_url) error = %v", name, err)
		}
		containsString := false
		for _, value := range propertySchema.Type {
			if value == "string" {
				containsString = true
			}
		}
		if !containsString {
			t.Fatalf("%s.repository_url schema type = %#v, want string", name, propertySchema.Type)
		}
	}
}
