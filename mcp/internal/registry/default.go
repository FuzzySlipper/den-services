package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
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
			Description: tool.Description,
			Backend:     denCoreBackend,
			Operation:   tool.Name,
			InputSchema: tool.InputSchema,
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
	return tools, nil
}

func githubCheckGateTools() []ToolDefinition {
	watchSchema := ObjectSchema(map[string]Schema{
		"project_id":            StringSchema("Project ID that owns the task."),
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
	}, "project_id", "task_id", "repository", "commit_sha", "ref", "required_checks", "requested_by")
	readSchema := ObjectSchema(map[string]Schema{
		"project_id": StringSchema("Project ID that owns the task."),
		"task_id":    IntegerSchema("Task ID that owns the existing gate."),
		"commit_sha": StringSchema("Exact 40-character commit SHA of the existing gate."),
	}, "project_id", "task_id", "commit_sha")
	waitSchema := ObjectSchema(map[string]Schema{
		"project_id": StringSchema("Project ID that owns the task."),
		"task_id":    IntegerSchema("Task ID that owns the existing gate."),
		"commit_sha": StringSchema("Exact 40-character commit SHA of the existing gate."),
		"after_id":   NullableIntegerSchema("Last terminal-event cursor already handled. Defaults to 0."),
		"wait_ms":    NullableIntegerSchema("Bounded server wait in milliseconds. Defaults to no wait and is capped at 50000."),
	}, "project_id", "task_id", "commit_sha")
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
