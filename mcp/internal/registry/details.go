package registry

// DetailArgumentAllowed reports whether a concise tool can issue an intentional
// read-only detail reference and whether an argument is safe to encode in it.
func DetailArgumentAllowed(toolName, argument string) bool {
	switch toolName {
	case "get_task", "get_task_workflow_summary":
		return argument == "task_id"
	case "get_document":
		return argument == "project_id" || argument == "slug"
	case "get_thread":
		return argument == "thread_id"
	case "get_discussion_thread":
		return argument == "thread_id" || argument == "include_comments"
	case "get_document_discussion":
		return argument == "project_id" || argument == "slug" || argument == "anchor" || argument == "include_resolved"
	case "get_latest_task_packet":
		return argument == "task_id" || argument == "packet_type" || argument == "role"
	case "list_review_rounds":
		return argument == "task_id"
	case "list_review_findings":
		return argument == "task_id" || argument == "review_round_id" || argument == "status" || argument == "resolved"
	default:
		return false
	}
}

func SupportsDetails(toolName string) bool {
	return DetailArgumentAllowed(toolName, firstDetailArgument(toolName))
}

func firstDetailArgument(toolName string) string {
	switch toolName {
	case "get_task", "get_task_workflow_summary", "get_latest_task_packet", "list_review_rounds", "list_review_findings":
		return "task_id"
	case "get_document", "get_document_discussion":
		return "project_id"
	case "get_thread", "get_discussion_thread":
		return "thread_id"
	default:
		return ""
	}
}

func taskDerivesProject(name string) bool {
	switch name {
	case "get_latest_task_packet", "post_review_findings", "request_review", "split_review_findings_to_follow_up",
		"await_github_checks", "watch_github_checks", "get_github_check_gate", "wait_for_github_checks",
		"mark_task_notifications_read":
		return true
	default:
		return false
	}
}

// TaskDerivesProject reports whether the facade must resolve canonical task
// identity before dispatching the tool to its owning backend.
func TaskDerivesProject(name string) bool {
	return taskDerivesProject(name)
}
