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
			Name:         tool.Name,
			Description:  modernizeDescription(tool.Name, tool.Description),
			Backend:      denCoreBackend,
			Operation:    tool.Name,
			InputSchema:  modernizeInputSchema(tool.Name, tool.InputSchema),
			Execution:    tool.Execution,
			WorkflowTier: workflowTierForTool(tool.Name),
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
		if policy, ok := hiddenCompatibilityToolPolicies[tool.Name]; ok {
			definition.Hidden = true
			definition.Deprecated = true
			definition.DeprecationMessage = policy.message
		}
		tools = append(tools, definition)
	}
	tools = append(tools, githubCheckGateTools()...)
	tools = append(tools, reviewFinalizationTools()...)
	tools = append(tools, campaignReviewTools()...)
	tools = append(tools, taskContextTools()...)
	tools = append(tools, reviewContextTools()...)
	tools = append(tools, reviewPipelineTools()...)
	tools = append(tools, contractErgonomicsTools()...)
	tools = append(tools, handoffTools()...)
	tools = append(tools, knowledgeTools()...)
	tools = append(tools, boardTools()...)
	return tools, nil
}

func boardTools() []ToolDefinition {
	projectID := StringSchema("Project whose Board should be accessed.")
	postID := IntegerSchema("Board post ID.")
	commentID := IntegerSchema("Board comment ID.")
	afterID := NullableIntegerSchema("Optional exclusive cursor ID for the next bounded page.")
	limit := NullableIntegerSchema("Optional page size from 1 to 100.")
	return []ToolDefinition{
		{
			Name: "create_board_post", Description: "Create a titled Board post. Board is durable passive conversation, not an executable task, wake, or instruction lane.", Backend: "board", Operation: "create_board_post",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id": projectID, "title": StringSchema("Post title."), "body_markdown": StringSchema("Post Markdown body."), "author_identity": StringSchema("Human or agent identity shown as the author."),
			}, "project_id", "title", "body_markdown", "author_identity"),
		},
		{
			Name: "list_board_posts", Description: "List one bounded page of visible Board post summaries. This never expands comment trees.", Backend: "board", Operation: "list_board_posts",
			InputSchema: ObjectSchema(map[string]Schema{"project_id": projectID, "after_id": afterID, "limit": limit}, "project_id"),
		},
		{
			Name: "get_board_post", Description: "Get one visible Board post without dumping its comments. Traverse replies separately with list_board_comments.", Backend: "board", Operation: "get_board_post",
			InputSchema: ObjectSchema(map[string]Schema{"post_id": postID}, "post_id"),
		},
		{
			Name: "create_board_comment", Description: "Reply to a Board post or directly to one comment. parent_comment_id identifies the immediate parent; omit it to comment on the original post.", Backend: "board", Operation: "create_board_comment",
			InputSchema: ObjectSchema(map[string]Schema{
				"post_id": postID, "parent_comment_id": NullableIntegerSchema("Immediate parent comment ID; omit for a direct reply to the post."), "body_markdown": StringSchema("Comment Markdown body."), "author_identity": StringSchema("Human or agent identity shown as the author."),
			}, "post_id", "body_markdown", "author_identity"),
		},
		{
			Name: "list_board_comments", Description: "List one bounded page of direct visible children under a post or parent comment. Omit parent_comment_id for comments directly on the original post; repeat per child to walk the tree.", Backend: "board", Operation: "list_board_comments",
			InputSchema: ObjectSchema(map[string]Schema{"post_id": postID, "parent_comment_id": NullableIntegerSchema("Immediate parent comment ID; omit for the post root."), "after_id": afterID, "limit": limit}, "post_id"),
		},
		{
			Name: "get_board_comment", Description: "Get one visible Board comment without expanding descendants.", Backend: "board", Operation: "get_board_comment",
			InputSchema: ObjectSchema(map[string]Schema{"comment_id": commentID}, "comment_id"),
		},
		{
			Name: "get_board_comment_path", Description: "Get a bounded breadcrumb path from the original post to one visible comment. Content-purged intermediate ancestors may appear only as structural tombstones.", Backend: "board", Operation: "get_board_comment_path",
			InputSchema: ObjectSchema(map[string]Schema{"comment_id": commentID, "limit": limit}, "comment_id"),
		},
		{
			Name: "purge_board_post", Description: "Purge a Board post and its entire reply subtree from every normal read, list, search, CLI, UI, and MCP surface. This destructive moderation action scrubs authored content and identity metadata.", Backend: "board", Operation: "purge_board_post",
			InputSchema: ObjectSchema(map[string]Schema{"post_id": postID, "actor_identity": StringSchema("Identity performing the purge."), "reason": StringSchema("Moderation reason retained without copying purged content.")}, "post_id", "actor_identity", "reason"),
		},
		{
			Name: "purge_board_comment", Description: "Purge one Board comment's authored content and identity from every normal surface. Retained descendants remain reachable through a content-free structural tombstone.", Backend: "board", Operation: "purge_board_comment",
			InputSchema: ObjectSchema(map[string]Schema{"comment_id": commentID, "actor_identity": StringSchema("Identity performing the purge."), "reason": StringSchema("Moderation reason retained without copying purged content.")}, "comment_id", "actor_identity", "reason"),
		},
	}
}

func reviewPipelineTools() []ToolDefinition {
	return []ToolDefinition{{
		Name:         "list_review_pipeline",
		Description:  "List a bounded read-only project review pipeline projection, including reviewable tasks with no round or gate. This does not create reviews, register gates, wake reviewers, finalize reviews, or change task state.",
		Backend:      "review",
		Operation:    "list_review_pipeline",
		WorkflowTier: WorkflowTierPrimitive,
		InputSchema: ObjectSchema(map[string]Schema{
			"project_id": StringSchema("Project ID whose reviewable tasks should be listed."),
			"limit":      NullableIntegerSchema("Optional page size from 1 to 100. Defaults to 50."),
			"offset":     NullableIntegerSchema("Optional non-negative page offset. Defaults to 0."),
		}, "project_id"),
	}}
}

func knowledgeTools() []ToolDefinition {
	return []ToolDefinition{{
		Name:        "den_knowledge_delete",
		Description: "Permanently delete one Knowledge entry by exact slug, including its revision history, tags, and links. This is irreversible and should be used only for deliberate curation of obsolete, duplicate, or low-value records; do not archive entries selected for removal.",
		Backend:     "knowledge",
		Operation:   "den_knowledge_delete",
		InputSchema: ObjectSchema(map[string]Schema{
			"slug": StringSchema("Exact Knowledge entry slug to hard delete."),
		}, "slug"),
	}}
}

func handoffTools() []ToolDefinition {
	label := StringSchema("Exact case-sensitive handoff label, such as den-services, task/6651, or campaign:gateway-cutover.")
	return []ToolDefinition{
		{
			Name:        "set_handoff",
			Description: "Create or silently replace the one current non-executable Markdown handoff for an exact arbitrary label. The service records UTC timestamps and returns the resulting revision; caller Markdown is never rewritten.",
			Backend:     "handoff",
			Operation:   "set_handoff",
			InputSchema: ObjectSchema(map[string]Schema{
				"label":         label,
				"body_markdown": StringSchema("Complete Markdown resume note. Maximum 64 KiB; frontmatter, if present, remains caller-owned and unchanged."),
			}, "label", "body_markdown"),
		},
		{
			Name:        "get_handoff",
			Description: "Retrieve the complete current Markdown handoff and server-owned revision/timestamp metadata for an exact label. Reading a handoff never wakes an agent or executes work.",
			Backend:     "handoff",
			Operation:   "get_handoff",
			InputSchema: ObjectSchema(map[string]Schema{"label": label}, "label"),
		},
	}
}

func campaignReviewTools() []ToolDefinition {
	return []ToolDefinition{{
		Name:         "request_campaign_review",
		Description:  "Request a campaign reconciliation review from approved child review rounds and the current named repositories. Children must be direct subtasks or share the campaign:<parent_project_id>:<parent_task_id> tag.",
		Backend:      "review",
		Operation:    "request_campaign_review",
		WorkflowTier: WorkflowTierPrimitive,
		InputSchema: ObjectSchema(map[string]Schema{
			"task_id":      IntegerSchema("Parent campaign task ID."),
			"requested_by": StringSchema("Agent or user requesting campaign reconciliation."),
			"children":     AnySchema("JSON array of {project_id, task_id, review_round_id} child snapshots."),
			"repositories": AnySchema("JSON array of {repository} entries naming the repositories in the campaign."),
			"tests_run":    AnySchema("Optional JSON array or comma-separated list of campaign verification commands."),
			"notes":        NullableStringSchema("Optional reconciliation notes."),
			"thread_id":    NullableIntegerSchema("Optional task thread receiving the review packet."),
			"run_id":       NullableStringSchema("Optional agent run correlation ID."),
		}, "task_id", "requested_by", "children", "repositories"),
	}}
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
	case "create_review_round", "request_review":
		for _, field := range []string{
			"base_commit", "head_commit", "last_reviewed_head_commit", "commits_since_last_review",
			"preferred_diff_base_commit", "preferred_diff_head_commit", "alternate_diff_base_commit",
			"alternate_diff_head_commit", "delta_base_commit", "inherited_commit_count", "task_local_commit_count",
			"preferred_diff_base_ref", "preferred_diff_head_ref", "alternate_diff_base_ref", "alternate_diff_head_ref",
		} {
			if _, exists := properties[field]; exists {
				delete(properties, field)
				changed = true
			}
		}
	case "post_worker_completion_packet":
		for _, field := range []string{"base_commit", "head_commit", "audited_head_commit"} {
			if _, exists := properties[field]; exists {
				delete(properties, field)
				changed = true
			}
		}
	}
	if name == "create_review_round" || name == "request_review" {
		required, _ := object["required"].([]any)
		filtered := make([]any, 0, len(required))
		for _, value := range required {
			field, _ := value.(string)
			switch field {
			case "base_commit", "head_commit":
				changed = true
				continue
			}
			filtered = append(filtered, value)
		}
		object["required"] = filtered
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
	case "create_review_round", "request_review":
		return "Create or idempotently reuse a review request for the current checkout and task context. The reviewer reads the current repository state when the request is handled."
	case "mark_notifications_read":
		return "Mark explicit user notification IDs as read for an agent identity. For scoped operations, use mark_project_notifications_read or mark_task_notifications_read."
	case "get_document_discussion":
		return "Read discussion threads and comments for a document without creating state. Use ensure_document_discussion only when a default thread must exist."
	case "store_document":
		return "Create or update a document. The full markdown content is persisted; the MCP result returns bounded metadata, byte count, SHA-256, and a preview so large writes are not mistaken for clipped storage."
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

func reviewContextTools() []ToolDefinition {
	return []ToolDefinition{{
		Name:        "get_review_context",
		Description: "Compose a bounded, read-only current review startup context from the canonical task, current review round, gate, packet headers, and guidance handles. It fails closed when no current review round exists and never falls back to the broad task context.",
		Backend:     "tasks", Operation: "get_review_context",
		WorkflowTier: WorkflowTierGreenPath,
		InputSchema: ObjectSchema(map[string]Schema{
			"task_id": IntegerSchema("Canonical task ID whose current review context should be composed."),
		}, "task_id"),
	}}
}

func githubCheckGateTools() []ToolDefinition {
	discoverySchema := ObjectSchema(map[string]Schema{
		"repository":      StringSchema("GitHub repository as owner/name."),
		"commit_sha":      StringSchema("Full 40-character commit SHA to inspect. Discovery is exact-SHA and read-only."),
		"required_checks": AnySchema("Optional JSON array or comma-separated list of exact GitHub check-run names to validate against observed runs."),
	}, "repository", "commit_sha")
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
		Name:        "discover_github_checks",
		Description: "Read GitHub check runs for an exact commit and optionally validate exact required names without creating a gate, changing a task, or posting evidence.",
		Backend:     "review",
		Operation:   "discover_github_checks", InputSchema: discoverySchema,
		WorkflowTier: WorkflowTierPrimitive,
	}, {
		Name:        "watch_github_checks",
		Description: "Register or read the durable exact-SHA GitHub check gate and return its deferral handle/current status immediately.",
		Backend:     "review",
		Operation:   "watch_github_checks", InputSchema: watchSchema,
		WorkflowTier: WorkflowTierPrimitive,
	}, {
		Name: "get_github_check_gate", Description: "Read an existing exact-SHA GitHub check gate without changing its timeout or polling state.",
		Backend: "review", Operation: "get_github_check_gate", InputSchema: readSchema, WorkflowTier: WorkflowTierPrimitive,
	}, {
		Name: "wait_for_github_checks", Description: "Wait briefly for an existing exact-SHA gate terminal event. Returns terminal state or a typed progress/timeout receipt without re-registering the gate.",
		Backend: "review", Operation: "wait_for_github_checks", InputSchema: waitSchema, WorkflowTier: WorkflowTierPrimitive,
	}, {
		Name: "await_github_checks", Description: "Compatibility alias for watch_github_checks. This operation returns immediately and does not await terminal checks.",
		Backend: "review", Operation: "await_github_checks", InputSchema: watchSchema, WorkflowTier: WorkflowTierPrimitive, Deprecated: true,
		DeprecationMessage: "Use watch_github_checks, then get_github_check_gate or bounded wait_for_github_checks. await_github_checks historically returned immediately.",
	}}
}

func reviewFinalizationTools() []ToolDefinition {
	return []ToolDefinition{{
		Name:         "finalize_review",
		Description:  "Finalize a normal review exactly once. This durable saga posts the canonical findings packet and transitions the task; retries resume incomplete delivery checkpoints. Supports looks_good and changes_requested only.",
		Backend:      "review",
		Operation:    "finalize_review",
		WorkflowTier: WorkflowTierGreenPath,
		InputSchema: ObjectSchema(map[string]Schema{
			"review_round_id":           IntegerSchema("Existing review round to finalize."),
			"verdict":                   StringSchema("Green-path verdict: looks_good or changes_requested."),
			"decided_by":                StringSchema("Agent or user making the review decision and task transition."),
			"notes":                     NullableStringSchema("Optional final review notes included in the canonical packet."),
			"thread_id":                 NullableIntegerSchema("Optional existing task-thread root for the canonical packet."),
			"run_id":                    NullableStringSchema("Optional run correlation identifier."),
			"subagent_role":             NullableStringSchema("Optional subagent role for correlation."),
			"prior_finding_resolutions": AnySchema("Optional JSON array of {finding_id, status, verification_note} terminal prior-finding resolutions."),
			"new_findings":              AnySchema("Optional JSON array of structured current-round findings; each has category, summary, notes, file_references, and test_commands."),
		}, "review_round_id", "verdict", "decided_by"),
	}}
}

func workflowTierForTool(name string) WorkflowTier {
	switch name {
	case "create_review_round", "create_review_finding", "post_review_findings",
		"respond_to_review_finding", "set_review_finding_status", "split_review_findings_to_follow_up",
		"request_review", "set_review_verdict":
		return WorkflowTierPrimitive
	default:
		return WorkflowTierOperator
	}
}

func contractErgonomicsTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "record_human_acceptance_review",
			Description: "Record a trusted human's affirmative task acceptance without creating or satisfying an agent review round or GitHub gate. The Tasks authority stores an immutable supplied-fact note, applies only the explicit lifecycle effect, and returns authoritative task/parent readback.",
			Backend:     "tasks",
			Operation:   "record_human_acceptance_review",
			InputSchema: ObjectSchema(map[string]Schema{
				"task_id":                  IntegerSchema("Canonical task receiving the human acceptance record."),
				"reviewer_identity":        StringSchema("Authenticated or trusted-caller-reconciled human reviewer identity."),
				"verdict":                  NullableStringSchema("Affirmative verdict. Omit for looks_good; no other verdict is accepted."),
				"rationale":                NullableStringSchema("Short hands-on observation or rationale. Omit for the plain Looks good easy path."),
				"reviewed_revision":        NullableStringSchema("Optional exact revision or version actually reviewed."),
				"reviewed_build":           NullableStringSchema("Optional build identity actually reviewed."),
				"reviewed_environment":     NullableStringSchema("Optional environment actually reviewed."),
				"evidence_links":           AnySchema("Optional JSON array or comma-separated evidence links supplied by the human."),
				"lifecycle_effect":         NullableStringSchema("Explicit effect: record_only, complete_task, or complete_task_and_parent. Defaults to record_only; parent completion is never implicit."),
				"idempotency_key":          StringSchema("Caller-generated retry key scoped to this task and acceptance facts."),
				"expected_task_updated_at": NullableStringSchema("Optional RFC3339 task updated_at readback used to reject stale confirmation."),
			}, "task_id", "reviewer_identity", "idempotency_key"),
		},
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

var hiddenCompatibilityToolPolicies = map[string]hiddenToolPolicy{
	"set_review_verdict": {
		message: "set_review_verdict is hidden compatibility behavior for exceptional follow_up_needed or blocked_by_dependency decisions. Use finalize_review for normal looks_good or changes_requested closeout.",
	},
}

var retiredToolPolicies = map[string]retiredToolPolicy{
	"send_agent_stream_message": {message: "send_agent_stream_message is retired from the MCP facade during the Core-off purge. Use task-thread messages or successor delivery/notification paths for supported wakes."},
	"get_agent_stream_entry":    {message: "agent-stream Core readback is retired from the default MCP facade pending a successor observation surface."},
	"list_agent_stream":         {message: "agent-stream Core readback is retired from the default MCP facade pending a successor observation surface."},

	"store_blackboard_entry":     {message: "blackboard tools are retired from the MCP facade; use set_handoff for mutable latest-value resume context, or project documents, task messages, and knowledge entries for durable history."},
	"get_blackboard_entry":       {message: "blackboard tools are retired from the MCP facade; use get_handoff for mutable latest-value resume context, or project documents, task messages, and knowledge entries for durable history."},
	"list_blackboard_entries":    {message: "blackboard tools are retired from the MCP facade; handoffs intentionally have no list surface. Use exact-label get_handoff or a durable owned surface."},
	"delete_blackboard_entry":    {message: "blackboard tools are retired from the MCP facade; handoffs intentionally have no delete surface. Replace the exact label or use a durable owned surface."},
	"cleanup_blackboard_entries": {message: "blackboard tools are retired from the MCP facade; handoffs intentionally have no cleanup lifecycle."},

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
