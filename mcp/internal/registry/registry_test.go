package registry

import (
	"encoding/json"
	"errors"
	"slices"
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

	if len(names) != 77 {
		t.Fatalf("tool count = %d, want 77", len(names))
	}
	for _, name := range []string{
		"search_documents",
		"den_knowledge_search",
		"den_knowledge_delete",
		"comment_on_document",
		"get_task",
		"store_document",
		"await_github_checks",
		"discover_github_checks",
		"watch_github_checks",
		"get_github_check_gate",
		"wait_for_github_checks",
		"get_task_context",
		"get_review_context",
		"list_review_pipeline",
		"get_details",
		"mark_project_notifications_read",
		"mark_task_notifications_read",
		"ensure_document_discussion",
		"set_handoff",
		"get_handoff",
		"record_human_acceptance_review",
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

func TestTaskContextToolAcceptsOnlyCanonicalTaskID(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	tool, err := registry.Resolve("get_task_context")
	if err != nil {
		t.Fatalf("Resolve(get_task_context) error = %v", err)
	}

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatalf("Unmarshal(input schema) error = %v", err)
	}
	if len(schema.Properties) != 1 || schema.Properties["task_id"] == nil {
		t.Fatalf("properties = %#v, want only task_id", schema.Properties)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "task_id" {
		t.Fatalf("required = %#v, want [task_id]", schema.Required)
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

func TestDefaultRegistryHidesLowLevelReviewVerdictCompatibilityTool(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	tool, err := registry.Resolve("set_review_verdict")
	if err != nil {
		t.Fatalf("Resolve(set_review_verdict) error = %v", err)
	}
	if !tool.Hidden || !tool.Deprecated || tool.TombstoneMessage != "" {
		t.Fatalf("set_review_verdict policy = hidden:%t deprecated:%t tombstone:%q",
			tool.Hidden, tool.Deprecated, tool.TombstoneMessage)
	}
	for _, listed := range registry.Tools() {
		if listed.Name == "set_review_verdict" {
			t.Fatal("set_review_verdict remains in normal tool discovery")
		}
	}
}

func TestDefaultRegistryExposesFinalizeReviewGreenPath(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	tool, err := registry.Resolve("finalize_review")
	if err != nil {
		t.Fatalf("Resolve(finalize_review) error = %v", err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties) != 9 {
		t.Fatalf("finalize_review properties = %v", schema.Properties)
	}
	for _, required := range []string{"review_round_id", "verdict", "decided_by"} {
		if !slices.Contains(schema.Required, required) {
			t.Fatalf("finalize_review required = %v, missing %s", schema.Required, required)
		}
	}
}

func TestReviewContextSupportsOpaqueDetailReference(t *testing.T) {
	if !SupportsDetails("get_review_context") || !DetailArgumentAllowed("get_review_context", "task_id") {
		t.Fatal("get_review_context must support task-scoped get_details expansion")
	}
}

func TestManagedRuntimeProfileHidesReviewPrimitivesButKeepsDirectAuthority(t *testing.T) {
	toolRegistry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}

	direct, err := toolRegistry.ToolsForProfile(ToolProfileDirect)
	if err != nil {
		t.Fatalf("ToolsForProfile(direct) error = %v", err)
	}
	managed, err := toolRegistry.ToolsForProfile(ToolProfileManagedRuntime)
	if err != nil {
		t.Fatalf("ToolsForProfile(managed-runtime) error = %v", err)
	}
	if len(direct) != 77 {
		t.Fatalf("direct tool count = %d, want 77", len(direct))
	}
	if len(managed) >= len(direct) {
		t.Fatalf("managed tool count = %d, want fewer than direct %d", len(managed), len(direct))
	}
	for _, name := range []string{"discover_github_checks", "watch_github_checks", "get_github_check_gate", "wait_for_github_checks", "await_github_checks", "request_review", "create_review_round"} {
		if containsListedTool(managed, name) {
			t.Fatalf("managed profile exposes primitive %s", name)
		}
		if _, err := toolRegistry.Resolve(name); err != nil {
			t.Fatalf("Resolve(%s) error = %v; profile filtering must not remove authority", name, err)
		}
	}
	if !containsListedTool(managed, "finalize_review") {
		t.Fatal("managed profile hides finalize_review green path")
	}
	for _, name := range []string{"get_task", "get_task_context", "list_tasks", "store_document", "update_task", "record_human_acceptance_review"} {
		if !containsListedTool(managed, name) {
			t.Fatalf("managed profile hides normal operator tool %s", name)
		}
	}

	catalog, err := toolRegistry.Catalog(ToolProfileManagedRuntime)
	if err != nil {
		t.Fatalf("Catalog(managed-runtime) error = %v", err)
	}
	if catalog.Profile != ToolProfileManagedRuntime || catalog.Revision == "" {
		t.Fatalf("catalog identity = %#v", catalog)
	}
	if catalog.HiddenToolCount == 0 || catalog.WorkflowTiers[WorkflowTierPrimitive] != 0 {
		t.Fatalf("managed catalog = %#v", catalog)
	}
	if !slices.Contains(catalog.HiddenWorkflowTiers, WorkflowTierPrimitive) {
		t.Fatalf("managed hidden tiers = %#v", catalog.HiddenWorkflowTiers)
	}
}

func TestManagedRuntimeProfileKeepsGreenPathToolsVisible(t *testing.T) {
	toolRegistry, err := New([]ToolDefinition{
		{Name: "submit_task_for_review", Description: "Managed review entry point.", Backend: "review", Operation: "submit_task_for_review", WorkflowTier: WorkflowTierGreenPath, InputSchema: ObjectSchema(nil)},
		{Name: "watch_github_checks", Description: "Low-level gate.", Backend: "review", Operation: "watch_github_checks", WorkflowTier: WorkflowTierPrimitive, InputSchema: ObjectSchema(nil)},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	managed, err := toolRegistry.ToolsForProfile(ToolProfileManagedRuntime)
	if err != nil {
		t.Fatalf("ToolsForProfile(managed-runtime) error = %v", err)
	}
	if !containsListedTool(managed, "submit_task_for_review") || containsListedTool(managed, "watch_github_checks") {
		t.Fatalf("managed tools = %#v", managed)
	}
}

func TestParseToolProfileRejectsUnknownProfiles(t *testing.T) {
	if _, err := ParseToolProfile("managed-crew"); !errors.Is(err, ErrUnknownToolProfile) {
		t.Fatalf("ParseToolProfile(managed-crew) error = %v, want %v", err, ErrUnknownToolProfile)
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
		if _, hiddenCompatibility := hiddenCompatibilityToolPolicies[snapshotTool.Name]; hiddenCompatibility {
			continue
		}
		if visibleIndex >= len(listed) {
			t.Fatalf("visible snapshot exhausted before %s", snapshotTool.Name)
		}
		if listed[visibleIndex].Name != snapshotTool.Name {
			t.Fatalf("visible tool[%d].Name = %q, want %q", visibleIndex, listed[visibleIndex].Name, snapshotTool.Name)
		}
		if listed[visibleIndex].Description != modernizeDescription(snapshotTool.Name, snapshotTool.Description) {
			t.Fatalf("visible tool[%d].Description differs for %s", visibleIndex, listed[visibleIndex].Name)
		}
		if string(listed[visibleIndex].InputSchema) != string(modernizeInputSchema(snapshotTool.Name, snapshotTool.InputSchema)) {
			t.Fatalf("visible tool[%d].InputSchema differs for %s", visibleIndex, listed[visibleIndex].Name)
		}
		if string(listed[visibleIndex].Execution) != string(snapshotTool.Execution) {
			t.Fatalf("visible tool[%d].Execution differs for %s", visibleIndex, listed[visibleIndex].Name)
		}
		visibleIndex++
	}
	if visibleIndex > len(listed) {
		t.Fatalf("visible count = %d, listed count = %d", visibleIndex, len(listed))
	}
	for _, tool := range listed[visibleIndex:] {
		if tool.Name != "await_github_checks" && tool.Name != "discover_github_checks" && tool.Name != "watch_github_checks" &&
			tool.Name != "get_github_check_gate" && tool.Name != "wait_for_github_checks" && tool.Name != "get_task_context" &&
			tool.Name != "get_review_context" &&
			tool.Name != "list_review_pipeline" &&
			tool.Name != "finalize_review" && tool.Name != "request_campaign_review" &&
			tool.Name != "get_details" && tool.Name != "mark_project_notifications_read" &&
			tool.Name != "mark_task_notifications_read" && tool.Name != "ensure_document_discussion" &&
			tool.Name != "record_human_acceptance_review" && tool.Name != "set_handoff" &&
			tool.Name != "get_handoff" && tool.Name != "den_knowledge_delete" {
			t.Fatalf("unexpected non-snapshot tool %q", tool.Name)
		}
	}
}

func TestVisibleToolSchemasHideVerbose(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range registry.Tools() {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("Unmarshal(%s) error = %v", tool.Name, err)
		}
		if _, exists := schema.Properties["verbose"]; exists {
			t.Fatalf("tool %s still exposes verbose", tool.Name)
		}
	}
}

func TestTaskScopedSchemasDeriveProject(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"get_latest_task_packet", "post_review_findings", "request_review", "request_campaign_review", "split_review_findings_to_follow_up",
		"await_github_checks", "watch_github_checks", "get_github_check_gate", "wait_for_github_checks", "mark_task_notifications_read",
	} {
		tool, err := registry.Resolve(name)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if _, exists := schema.Properties["project_id"]; exists {
			t.Fatalf("tool %s still exposes project_id", name)
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

func containsName(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

func containsListedTool(tools []ListedTool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
