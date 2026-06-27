package registry

const denCoreBackend = "den-core"

func DefaultRegistry() (*Registry, error) {
	return New(DefaultTools())
}

// DefaultTools is the static compatibility surface exposed by tools/list.
// Extend this list as tool families are ported, then intentionally update the
// schema snapshot hash in snapshot_test.go.
func DefaultTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_project",
			Description: "Get a project by ID with summary stats and optional unread count for an agent.",
			Backend:     denCoreBackend,
			Operation:   "get_project",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id": StringSchema("Project ID."),
				"agent":      NullableStringSchema("Agent identity used for unread message count."),
			}, "project_id"),
		},
		{
			Name:        "list_tasks",
			Description: "List tasks in a project with optional filters.",
			Backend:     denCoreBackend,
			Operation:   "list_tasks",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id":  StringSchema("Project ID."),
				"assigned_to": NullableStringSchema("Filter by assigned agent."),
				"parent_id":   NullableIntegerSchema("Filter by parent task ID."),
				"priority":    NullableIntegerSchema("Filter to tasks at this priority or higher; lower numbers are higher priority."),
				"status":      NullableStringSchema("Comma-separated task statuses."),
				"tags":        NullableStringSchema("Comma-separated tags; task must have all specified tags."),
				"verbose":     BooleanSchema("Return full task records when true."),
			}, "project_id"),
		},
		{
			Name:        "get_task",
			Description: "Get full task details including dependencies, subtasks, and recent messages.",
			Backend:     denCoreBackend,
			Operation:   "get_task",
			InputSchema: ObjectSchema(map[string]Schema{
				"task_id": IntegerSchema("Task ID."),
				"verbose": BooleanSchema("Return full details when true."),
			}, "task_id"),
		},
		{
			Name:        "create_task",
			Description: "Create a new task or subtask in a project.",
			Backend:     denCoreBackend,
			Operation:   "create_task",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id":  StringSchema("Project ID."),
				"title":       StringSchema("Task title."),
				"assigned_to": NullableStringSchema("Agent identity to assign this task to."),
				"depends_on":  NullableStringSchema("Comma-separated task IDs this task depends on."),
				"description": NullableStringSchema("Detailed description or acceptance criteria in Markdown."),
				"parent_id":   NullableIntegerSchema("Parent task ID to create this as a subtask."),
				"priority":    BoundedIntegerSchema("Priority from 1 critical to 5 backlog.", 1, 5),
				"tags":        AnySchema("Optional tag payload accepted by the Den backend."),
				"verbose":     BooleanSchema("Return the full task record when true."),
			}, "project_id", "title"),
		},
		{
			Name:        "update_task",
			Description: "Update mutable fields on an existing task.",
			Backend:     denCoreBackend,
			Operation:   "update_task",
			InputSchema: ObjectSchema(map[string]Schema{
				"task_id":                      IntegerSchema("Task ID to update."),
				"agent":                        StringSchema("Agent identity for audit trail."),
				"assigned_to":                  NullableStringSchema("New assigned agent."),
				"blocker_attempted_remedies":   NullableStringSchema("Evidence of attempted remedies when blocking a task."),
				"blocker_reason":               NullableStringSchema("Why the agent cannot proceed when status is blocked."),
				"blocker_requires_human_input": NullableBooleanSchema("Whether human input is required to unblock the task."),
				"blocker_suggested_next_step":  NullableStringSchema("Suggested next decision or unblock path."),
				"blocker_summary":              NullableStringSchema("Short blocker summary when status is blocked."),
				"description":                  NullableStringSchema("New task description."),
				"parent_id":                    NullableIntegerSchema("New parent task ID."),
				"priority":                     NullableIntegerSchema("New priority from 1 critical to 5 backlog."),
				"status":                       NullableStringSchema("New status: planned, in_progress, review, blocked, done, or cancelled."),
				"tags":                         AnySchema("Optional tag payload accepted by the Den backend."),
				"title":                        NullableStringSchema("New task title."),
				"verbose":                      BooleanSchema("Return the full task record when true."),
			}, "task_id", "agent"),
		},
		{
			Name:        "send_message",
			Description: "Send a project message, optionally attached to a task or existing thread.",
			Backend:     denCoreBackend,
			Operation:   "send_message",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id": StringSchema("Project ID."),
				"sender":     StringSchema("Agent identity sending the message."),
				"content":    StringSchema("Message body in Markdown."),
				"intent":     NullableStringSchema("Optional canonical intent such as review_feedback or handoff."),
				"metadata":   AnySchema("Optional structured metadata."),
				"task_id":    NullableIntegerSchema("Task ID to attach this message to."),
				"thread_id":  NullableIntegerSchema("Root message ID to reply to."),
				"verbose":    BooleanSchema("Return the full message record when true."),
			}, "project_id", "sender", "content"),
		},
		{
			Name:        "get_messages",
			Description: "Get messages in a project with optional filters.",
			Backend:     denCoreBackend,
			Operation:   "get_messages",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id": StringSchema("Project ID."),
				"intent":     NullableStringSchema("Optional canonical intent filter."),
				"limit":      BoundedIntegerSchema("Maximum messages to return.", 1, 100),
				"since":      NullableStringSchema("ISO datetime; only messages after this time."),
				"task_id":    NullableIntegerSchema("Filter to messages on a specific task."),
				"unread_for": NullableStringSchema("Agent identity; only unread messages for this agent."),
				"verbose":    BooleanSchema("Return full message bodies when true."),
			}, "project_id"),
		},
		{
			Name:        "get_document",
			Description: "Get a Den document by project or space ID and slug.",
			Backend:     denCoreBackend,
			Operation:   "get_document",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id": StringSchema("Project or space ID."),
				"slug":       StringSchema("Document slug."),
				"verbose":    BooleanSchema("Return full document content when true."),
			}, "project_id", "slug"),
		},
		{
			Name:        "store_document",
			Description: "Create or update a Den document.",
			Backend:     denCoreBackend,
			Operation:   "store_document",
			InputSchema: ObjectSchema(map[string]Schema{
				"project_id": StringSchema("Project or space ID."),
				"slug":       StringSchema("Unique document slug within the project."),
				"title":      StringSchema("Document title."),
				"content":    StringSchema("Document content in Markdown."),
				"doc_type":   StringSchema("Document type such as prd, spec, adr, convention, reference, note, or memory."),
				"summary":    NullableStringSchema("Optional short summary for indexing and listing."),
				"tags":       AnySchema("Optional tag payload accepted by the Den backend."),
				"verbose":    BooleanSchema("Return the full document record when true."),
			}, "project_id", "slug", "title", "content"),
		},
	}
}
