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

	if len(names) != 60 {
		t.Fatalf("tool count = %d, want 60", len(names))
	}
	for _, name := range []string{
		"search_documents",
		"den_knowledge_search",
		"comment_on_document",
		"get_task",
		"store_document",
	} {
		if !containsName(names, name) {
			t.Fatalf("visible tools missing %s", name)
		}
	}
	for _, name := range []string{
		"legacy_get_dispatch",
		"store_blackboard_entry",
		"lease_worker",
		"invoke_capability",
		"list_topics",
		"delete_space",
	} {
		if containsName(names, name) {
			t.Fatalf("hidden tool %s is visible", name)
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

func TestDefaultRegistryResolvesHiddenRetiredTools(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	tool, err := registry.Resolve("lease_worker")
	if err != nil {
		t.Fatalf("Resolve(lease_worker) error = %v", err)
	}
	if !tool.Hidden {
		t.Fatal("lease_worker Hidden = false, want true")
	}
	if tool.TombstoneMessage == "" {
		t.Fatal("lease_worker TombstoneMessage is empty")
	}
	if !tool.Deprecated {
		t.Fatal("lease_worker Deprecated = false, want true")
	}
}

func TestDefaultRegistryResolvesHiddenAdminToolsWithoutTombstone(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	tool, err := registry.Resolve("delete_space")
	if err != nil {
		t.Fatalf("Resolve(delete_space) error = %v", err)
	}
	if !tool.Hidden {
		t.Fatal("delete_space Hidden = false, want true")
	}
	if tool.TombstoneMessage != "" {
		t.Fatalf("delete_space TombstoneMessage = %q, want empty", tool.TombstoneMessage)
	}
	if !tool.Deprecated || tool.DeprecationMessage == "" {
		t.Fatalf("delete_space deprecation = %t/%q", tool.Deprecated, tool.DeprecationMessage)
	}
}

func TestDefaultRegistryMatchesCapturedVisibleSnapshotSubset(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	var snapshot liveToolSnapshot
	if err := json.Unmarshal(liveToolsSnapshot, &snapshot); err != nil {
		t.Fatalf("Unmarshal(snapshot) error = %v", err)
	}
	listed := registry.Tools()
	visibleIndex := 0
	for _, snapshotTool := range snapshot.Tools {
		if _, retired := retiredToolPolicies[snapshotTool.Name]; retired {
			continue
		}
		if _, hiddenAdmin := hiddenAdminToolPolicies[snapshotTool.Name]; hiddenAdmin {
			continue
		}
		if visibleIndex >= len(listed) {
			t.Fatalf("visible snapshot exhausted before %s", snapshotTool.Name)
		}
		if listed[visibleIndex].Name != snapshotTool.Name {
			t.Fatalf("visible tool[%d].Name = %q, want %q", visibleIndex, listed[visibleIndex].Name, snapshotTool.Name)
		}
		if listed[visibleIndex].Description != snapshotTool.Description {
			t.Fatalf("visible tool[%d].Description differs for %s", visibleIndex, listed[visibleIndex].Name)
		}
		if string(listed[visibleIndex].InputSchema) != string(snapshotTool.InputSchema) {
			t.Fatalf("visible tool[%d].InputSchema differs for %s", visibleIndex, listed[visibleIndex].Name)
		}
		if string(listed[visibleIndex].Execution) != string(snapshotTool.Execution) {
			t.Fatalf("visible tool[%d].Execution differs for %s", visibleIndex, listed[visibleIndex].Name)
		}
		visibleIndex++
	}
	if visibleIndex != len(listed) {
		t.Fatalf("visible count = %d, listed count = %d", visibleIndex, len(listed))
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

func containsName(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}
