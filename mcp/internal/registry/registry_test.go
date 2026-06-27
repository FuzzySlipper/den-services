package registry

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDefaultRegistryListsLiveCompatibilitySurface(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	tools := registry.Tools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}

	if len(names) != 136 {
		t.Fatalf("tool count = %d, want 136", len(names))
	}
	for _, name := range []string{
		"search_documents",
		"den_knowledge_search",
		"comment_on_document",
		"get_task",
		"store_document",
	} {
		if _, err := registry.Resolve(name); err != nil {
			t.Fatalf("Resolve(%s) error = %v", name, err)
		}
	}
}

func TestDefaultRegistryResolvesToolsWithoutBackendLiveness(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	tool, err := registry.Resolve("create_task")
	if err != nil {
		t.Fatalf("Resolve(create_task) error = %v", err)
	}
	if tool.Backend != denCoreBackend {
		t.Fatalf("Backend = %q, want %q", tool.Backend, denCoreBackend)
	}
}

func TestDefaultRegistryMatchesCapturedLiveSnapshot(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	var snapshot liveToolSnapshot
	if err := json.Unmarshal(liveToolsSnapshot, &snapshot); err != nil {
		t.Fatalf("Unmarshal(snapshot) error = %v", err)
	}
	listed := registry.Tools()
	if len(listed) != len(snapshot.Tools) {
		t.Fatalf("listed count = %d, want %d", len(listed), len(snapshot.Tools))
	}
	for index := range listed {
		if listed[index].Name != snapshot.Tools[index].Name {
			t.Fatalf("tool[%d].Name = %q, want %q", index, listed[index].Name, snapshot.Tools[index].Name)
		}
		if listed[index].Description != snapshot.Tools[index].Description {
			t.Fatalf("tool[%d].Description differs for %s", index, listed[index].Name)
		}
		if string(listed[index].InputSchema) != string(snapshot.Tools[index].InputSchema) {
			t.Fatalf("tool[%d].InputSchema differs for %s", index, listed[index].Name)
		}
		if string(listed[index].Execution) != string(snapshot.Tools[index].Execution) {
			t.Fatalf("tool[%d].Execution differs for %s", index, listed[index].Name)
		}
	}
}

func TestRegistrySupportsCompatibilityAliases(t *testing.T) {
	registry, err := New([]ToolDefinition{
		{
			Name:        "get_task",
			Description: "Get a task.",
			Backend:     denCoreBackend,
			Operation:   "get_task",
			InputSchema: ObjectSchema(map[string]Schema{
				"task_id": IntegerSchema("Task ID."),
			}, "task_id"),
			Aliases: []ToolAlias{
				{
					Name:               "task_get",
					Deprecated:         true,
					DeprecationMessage: "Use get_task.",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tool, err := registry.Resolve("task_get")
	if err != nil {
		t.Fatalf("Resolve(alias) error = %v", err)
	}
	if tool.Name != "get_task" {
		t.Fatalf("resolved tool name = %q, want get_task", tool.Name)
	}
	listed := registry.Tools()
	if len(listed) != 2 {
		t.Fatalf("listed count = %d, want 2", len(listed))
	}
	if listed[1].Annotations == nil || !listed[1].Annotations.Deprecated || listed[1].Annotations.CanonicalName != "get_task" {
		t.Fatalf("alias annotations = %#v", listed[1].Annotations)
	}
}

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	_, err := New([]ToolDefinition{
		minimalTool("get_task"),
		minimalTool("get_task"),
	})
	if err == nil {
		t.Fatal("New() error = nil")
	}
}

func TestRegistryRejectsUnknownTool(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	_, err = registry.Resolve("missing")
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("Resolve(missing) error = %v, want %v", err, ErrUnknownTool)
	}
}

func minimalTool(name string) ToolDefinition {
	return ToolDefinition{
		Name:        name,
		Description: "Minimal test tool.",
		Backend:     denCoreBackend,
		Operation:   name,
		InputSchema: ObjectSchema(nil),
	}
}
