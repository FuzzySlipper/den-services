package registry

import (
	"errors"
	"testing"
)

func TestDefaultRegistryListsRepresentativeCoreSubset(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	tools := registry.Tools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}

	want := []string{
		"get_project",
		"list_tasks",
		"get_task",
		"create_task",
		"update_task",
		"send_message",
		"get_messages",
		"get_document",
		"store_document",
	}
	if len(names) != len(want) {
		t.Fatalf("tool count = %d, want %d: %v", len(names), len(want), names)
	}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("tool[%d] = %q, want %q", index, names[index], name)
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
