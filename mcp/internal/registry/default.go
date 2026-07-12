package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const denCoreBackend = "den-core"

type retiredToolPolicy struct {
	message string
}

type hiddenToolPolicy struct {
	message string
}

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
		definition := ToolDefinition{
			Name:        tool.Name,
			Description: modernizeDescription(tool.Name, tool.Description),
			Backend:     denCoreBackend,
			Operation:   tool.Name,
			InputSchema: modernizeInputSchema(tool.Name, tool.InputSchema),
			Execution:   tool.Execution,
		}
		if policy, ok := retiredToolPolicies[tool.Name]; ok {
			definition.Hidden = true
			definition.TombstoneMessage = policy.message
			definition.Deprecated = true
			definition.DeprecationMessage = policy.message
		}
		if policy, ok := hiddenAdminToolPolicies[tool.Name]; ok {
			definition.Hidden = true
			definition.Deprecated = true
			definition.DeprecationMessage = policy.message
		}
		tools = append(tools, definition)
	}
	tools = append(tools, githubCheckGateTools()...)
	tools = append(tools, taskContextTools()...)
	tools = append(tools, contractErgonomicsTools()...)
	return tools, nil
}

func modernizeInputSchema(name string, schema Schema) Schema {
	var object map[string]any
	if err := json.Unmarshal(schema, &object); err != nil {
		return schema
	}
	properties, ok := object["properties"].(map[string]any)
	if !ok {
		return schema
	}
	changed := false
	if _, exists := properties["verbose"]; exists {
		delete(properties, "verbose")
		changed = true
	}
	if taskDerivesProject(name) {
		if _, exists := properties["project_id"]; exists {
			delete(properties, "project_id")
			changed = true
		}
	}
	switch name {
	case "mark_notifications_read":
		delete(properties, "mark_all")
		delete(properties, "scope_project_id")
		delete(properties, "scope_task_id")
		properties["notification_ids"] = map[string]any{
			"type":        "string",
			"description": "Comma-separated notification IDs to mark as read.",
		}
		changed = true
	case "get_document_discussion":
		delete(properties, "create_if_missing")
		changed = true
	}
	if !changed {
		return schema
	}
	required, _ := object["required"].([]any)
	filtered := make([]any, 0, len(required)+1)
	for _, value := range required {
		field, _ := value.(string)
		if field == "verbose" || (field == "project_id" && taskDerivesProject(name)) {
			continue
		}
		filtered = append(filtered, value)
	}
	if name == "mark_notifications_read" && !containsRequired(filtered, "notification_ids") {
		filtered = append(filtered, "notification_ids")
	}
	if len(filtered) == 0 {
		delete(object, "required")
	} else {
		object["required"] = filtered
	}
	return mustSchema(object)
}

func modernizeDescription(name, description string) string {
	verboseSentence := regexp.MustCompile(`(?i)(;\s*)?use verbose=true[^.]*\.`)
	description = strings.TrimSpace(verboseSentence.ReplaceAllString(description, "."))
	description = strings.ReplaceAll(description, "..", ".")
	switch name {
	case "mark_notifications_read":
		return "Mark explicit user notification IDs as read for an agent identity. For scoped operations, use mark_project_notifications_read or mark_task_notifications_read."
	case "get_document_discussion":
		return "Read discussion threads and comments for a document without creating state. Use ensure_document_discussion only when a default thread must exist."
	default:
		return description
	}
}

func containsRequired(required []any, field string) bool {
	for _, value := range required {
		if value == field {
			return true
		}
	}
	return false
}

func taskContextTools() []ToolDefinition {
	return []ToolDefinition{{
		Name:        "get_task_context",
		Description: "Compose a bounded, read-only Den task briefing from canonical task, workflow, guidance, librarian, and task-thread authorities. The canonical task supplies project scope. A missing canonical task is an error; degraded optional sources are labelled in source_status.",
		Backend:     "tasks", Operation: "get_task_context",
		InputSchema: ObjectSchema(map[string]Schema{
			"task_id": IntegerSchema("Canonical task ID to brief."),
		}, "task_id"),
	}}
}

func githubCheckGateTools() []ToolDefinition {
	watchSchema := ObjectSchema(map[string]Schema{
		"task_id":               IntegerSchema("Task ID to gate."),
		"repository":            StringSchema("GitHub repository as owner/name."),
		"commit_sha":            StringSchema("Full 40-character commit SHA to watch. Den tracks this exact SHA, not latest branch head."),
		"ref":                   StringSchema("Branch or ref the agent pushed, e.g. main."),
		"required_checks":       AnySchema("JSON array or comma-separated list of required GitHub check run names."),
		"timeout_seconds":       NullableIntegerSchema("Optional timeout in seconds. Defaults to review service config."),
		"poll_interval_seconds": NullableIntegerSchema("Optional poll interval in seconds for this gate. Defaults to review service config."),
		"requested_by":          StringSchema("Agent or user registering the gate."),
		"agent_profile":         NullableStringSchema("Optional logical agent profile for correlation."),
		"agent_instance_id":     NullableStringSchema("Optional runtime instance ID for correlation."),
		"session_key":           NullableStringSchema("Optional session key for correlation."),
	}, "task_id", "repository", "commit_sha", "ref", "required_checks", "requested_by")
	readSchema := ObjectSchema(map[string]Schema{
		"task_id":    IntegerSchema("Task ID that owns the existing gate."),
		"commit_sha": StringSchema("Exact 40-character commit SHA of the existing gate."),
	}, "task_id", "commit_sha")
	waitSchema := ObjectSchema(map[string]Schema{
		"task_id":    IntegerSchema("Task ID that owns the existing gate."),
		"commit_sha": StringSchema("Exact 40-character commit SHA of the existing gate."),
		"after_id":   NullableIntegerSchema("Last terminal-event cursor already handled. Defaults to 0."),
		"wait_ms":    NullableIntegerSchema("Bounded server wait in milliseconds. Defaults to no wait and is capped at 50000."),
	}, "task_id", "commit_sha")
	return []ToolDefinition{{
		Name:        "watch_github_checks",
		Description: "Register or read the durable exact-SHA GitHub check gate and return its deferral handle/current status immediately.",
		Backend:     "review",
		Operation:   "watch_github_checks", InputSchema: watchSchema,
	}, {
		Name: "get_github_check_gate", Description: "Read an existing exact-SHA GitHub check gate without changing its timeout or polling state.",
		Backend: "review", Operation: "get_github_check_gate", InputSchema: readSchema,
	}, {
		Name: "wait_for_github_checks", Description: "Wait briefly for an existing exact-SHA gate terminal event. Returns terminal state or a typed progress/timeout receipt without re-registering the gate.",
		Backend: "review", Operation: "wait_for_github_checks", InputSchema: waitSchema,
	}, {
		Name: "await_github_checks", Description: "Compatibility alias for watch_github_checks. This operation returns immediately and does not await terminal checks.",
		Backend: "review", Operation: "await_github_checks", InputSchema: watchSchema, Deprecated: true,
		DeprecationMessage: "Use watch_github_checks, then get_github_check_gate or bounded wait_for_github_checks. await_github_checks historically returned immediately.",
	}}
}

func contractErgonomicsTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_details",
			Description: "Intentionally expand one concise read result using its opaque detail_ref. Detail reads are allowlisted, read-only, and preserve the original backend authorization path.",
			Backend:     "mcp-facade",
			Operation:   "get_details",
			InputSchema: ObjectSchema(map[string]Schema{
				"detail_ref": StringSchema("Opaque detail reference returned by a concise read tool."),
			}, "detail_ref"),
		},
		{
			Name:        "mark_project_notifications_read",
			Description: "Mark all notifications in one project as read for an agent identity.",
			Backend:     "messages",
			Operation:   "mark_project_notifications_read",
			InputSchema: ObjectSchema(map[string]Schema{
				"agent":      StringSchema("Agent identity to mark read for."),
				"project_id": StringSchema("Project whose notifications should be marked read."),
			}, "agent", "project_id"),
		},
		{
			Name:        "mark_task_notifications_read",
			Description: "Mark all notifications on one canonical task as read for an agent identity. Project scope is derived from the task.",
			Backend:     "messages",
			Operation:   "mark_task_notifications_read",
			InputSchema: ObjectSchema(map[string]Schema{
				"agent":   StringSchema("Agent identity to mark read for."),
				"task_id": IntegerSchema("Canonical task whose notifications should be marked read."),
			}, "agent", "task_id"),
		},
		{
			Name:        "ensure_document_discussion",
			Description: "Ensure a document has a default discussion thread and return it. Use get_document_discussion for read-only lookup.",
			Backend:     "documents",
			Operation:   "ensure_document_discussion",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id": StringSchema("Project or space ID."),
				"slug":       StringSchema("Document slug."),
			}, "project_id", "slug"),
		},
	}
}

var hiddenAdminToolPolicies = map[string]hiddenToolPolicy{
	"delete_space": {message: "delete_space is admin-only and hidden from default MCP tool discovery. Prefer archive_space or update_space_visibility for normal lifecycle removal."},
}

var retiredToolPolicies = map[string]retiredToolPolicy{
	"send_agent_stream_message": {message: "send_agent_stream_message is retired from the MCP facade during the Core-off purge. Use task-thread messages or successor delivery/notification paths for supported wakes."},
	"get_agent_stream_entry":    {message: "agent-stream Core readback is retired from the default MCP facade pending a successor observation surface."},
	"list_agent_stream":         {message: "agent-stream Core readback is retired from the default MCP facade pending a successor observation surface."},

	"store_blackboard_entry":     {message: "blackboard tools are retired from the MCP facade; use project documents, task messages, or knowledge entries with explicit ownership instead."},
	"get_blackboard_entry":       {message: "blackboard tools are retired from the MCP facade; use project documents, task messages, or knowledge entries with explicit ownership instead."},
	"list_blackboard_entries":    {message: "blackboard tools are retired from the MCP facade; use project documents, task messages, or knowledge entries with explicit ownership instead."},
	"delete_blackboard_entry":    {message: "blackboard tools are retired from the MCP facade; use project documents, task messages, or knowledge entries with explicit ownership instead."},
	"cleanup_blackboard_entries": {message: "blackboard tools are retired from the MCP facade; use project documents, task messages, or knowledge entries with explicit ownership instead."},

	"legacy_get_dispatch":                {message: "legacy dispatch tools are retired from the default MCP facade; dispatch is archive-only historical state."},
	"legacy_approve_dispatch":            {message: "legacy dispatch mutation is retired; use review/task/message successor workflow instead."},
	"legacy_reject_dispatch":             {message: "legacy dispatch mutation is retired; use review/task/message successor workflow instead."},
	"legacy_complete_dispatch":           {message: "legacy dispatch mutation is retired; use review/task/message successor workflow instead."},
	"legacy_list_dispatches":             {message: "legacy dispatch tools are retired from the default MCP facade; dispatch is archive-only historical state."},
	"legacy_request_den_publish_dry_run": {message: "legacy publish/dry-run tools are retired from the default MCP facade; use current review and promotion workflow evidence instead."},
	"legacy_publish_reviewed_branch":     {message: "legacy publish tools are retired from the default MCP facade; use current reviewed branch promotion workflow instead."},
	"legacy_publish_worker_branch":       {message: "legacy worker publish tools are retired from the default MCP facade; worker pool compatibility is not preserved during rusty-crew migration."},

	"update_topic":                   {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"create_topic":                   {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"delete_topic":                   {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"get_topic":                      {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"list_topics":                    {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"validate_topic_tags":            {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"append_topic_clip":              {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"list_topic_clips":               {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"discard_topic_clips":            {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"escalate_topic_clips":           {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"claim_topic_clip_batch":         {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"complete_topic_clips":           {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"list_curation_decisions":        {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},
	"cleanup_topic_clip_raw_content": {message: "topic and curation tools are retired from the default MCP facade until a successor curation owner exists."},

	"get_capability":               {message: "Core capability tools are hidden pending a successor capability owner; do not invoke capabilities through Core during Core-off purge."},
	"list_capabilities":            {message: "Core capability tools are hidden pending a successor capability owner; do not invoke capabilities through Core during Core-off purge."},
	"invoke_capability":            {message: "Core capability tools are hidden pending a successor capability owner; do not invoke capabilities through Core during Core-off purge."},
	"upsert_capability_definition": {message: "Core capability tools are hidden pending a successor capability owner; do not mutate capabilities through Core during Core-off purge."},
	"analyze_image":                {message: "analyze_image is a Core capability wrapper and is hidden until a successor capability owner is available."},
	"retry_cap_report":             {message: "retry_cap_report is a Core capability/diagnostic helper and is hidden until a successor capability owner is available."},

	"prepare_coder_context_packet":          {message: "worker context packet builders are hidden while worker/run ownership moves to rusty-crew; use task/message/review successor APIs directly."},
	"prepare_reviewer_context_packet":       {message: "worker context packet builders are hidden while worker/run ownership moves to rusty-crew; use task/message/review successor APIs directly."},
	"prepare_validator_context_packet":      {message: "worker context packet builders are hidden while worker/run ownership moves to rusty-crew; use task/message/review successor APIs directly."},
	"prepare_drift_checker_context_packet":  {message: "worker context packet builders are hidden while worker/run ownership moves to rusty-crew; use task/message/review successor APIs directly."},
	"prepare_packet_auditor_context_packet": {message: "worker context packet builders are hidden while worker/run ownership moves to rusty-crew; use task/message/review successor APIs directly."},
	"prepare_scope_auditor_context_packet":  {message: "worker context packet builders are hidden while worker/run ownership moves to rusty-crew; use task/message/review successor APIs directly."},

	"get_latest_worker_completion":        {message: "worker completion/run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"post_worker_completion_packet":       {message: "worker completion/run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"list_worker_runs":                    {message: "worker run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"get_worker_run":                      {message: "worker run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"get_worker_run_status":               {message: "worker run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"register_worker_run":                 {message: "worker run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"rerun_worker_run":                    {message: "worker run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"cleanup_worker_run":                  {message: "worker run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"abort_worker_run":                    {message: "worker run tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"detect_orphaned_worker_runs":         {message: "worker runtime cleanup is retired from Core; use the future rusty-crew/runtime owner for process/session cleanup."},
	"force_terminate_orphan_run":          {message: "worker runtime cleanup is retired from Core; use the future rusty-crew/runtime owner for process/session cleanup."},
	"lease_worker":                        {message: "worker leasing is hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"list_pool_members":                   {message: "worker pool tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"upsert_pool_member":                  {message: "worker pool tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"quarantine_pool_member":              {message: "worker pool tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"get_worker_pool_summary":             {message: "worker pool tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"list_assignments":                    {message: "worker assignment tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"get_assignment":                      {message: "worker assignment tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"release_assignment":                  {message: "worker assignment tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"record_cleanup_evidence":             {message: "worker assignment cleanup tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"append_checkpoint":                   {message: "worker checkpoint tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"respond_to_checkpoint":               {message: "worker checkpoint tools are hidden while worker/run ownership moves to rusty-crew; no legacy pi-crew or Hermes shim is preserved."},
	"list_no_capacity_requests":           {message: "worker no-capacity diagnostics are hidden while worker/run ownership moves to rusty-crew."},
	"get_no_capacity_request":             {message: "worker no-capacity diagnostics are hidden while worker/run ownership moves to rusty-crew."},
	"get_pool_residency_projection":       {message: "worker/orchestrator residency projections are hidden while worker/run ownership moves to rusty-crew."},
	"create_orchestrator_lease":           {message: "orchestrator lease tools are hidden while worker/orchestrator ownership moves to rusty-crew."},
	"list_orchestrator_leases":            {message: "orchestrator lease tools are hidden while worker/orchestrator ownership moves to rusty-crew."},
	"get_orchestrator_lease":              {message: "orchestrator lease tools are hidden while worker/orchestrator ownership moves to rusty-crew."},
	"transition_orchestrator_lease":       {message: "orchestrator lease tools are hidden while worker/orchestrator ownership moves to rusty-crew."},
	"reconcile_stale_orchestrator_leases": {message: "orchestrator lease tools are hidden while worker/orchestrator ownership moves to rusty-crew."},
	"determine_orchestrator_next_action":  {message: "orchestrator action tools are hidden while worker/orchestrator ownership moves to rusty-crew."},
	"list_active_agents":                  {message: "Core active-agent projection is hidden pending successor runtime/observation ownership."},
	"list_agent_instance_bindings":        {message: "Core agent instance bindings are hidden pending successor runtime/observation ownership."},
}
