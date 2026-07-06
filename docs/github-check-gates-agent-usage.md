# GitHub Check Gates Agent Usage

Task #4245 adds a Review-owned GitHub check gate for the low-ceremony agent flow:

```text
commit -> push -> register gate -> resume only on failure or completion evidence
```

Den does not run CI. GitHub Actions remains the CI runner. The Review service records a durable gate for an exact commit SHA, polls GitHub check runs for that SHA, and appends task-thread evidence when the gate passes, fails, times out, or is superseded.

## MCP Tool

Use `await_github_checks` after pushing:

```json
{
  "project_id": "den-services",
  "task_id": 4245,
  "repository": "OWNER/REPO",
  "commit_sha": "0123456789abcdef0123456789abcdef01234567",
  "ref": "main",
  "required_checks": ["go test", "lint"],
  "requested_by": "codex",
  "timeout_seconds": 1800,
  "poll_interval_seconds": 120,
  "agent_profile": "codex",
  "agent_instance_id": "optional-runtime-instance",
  "session_key": "optional-session"
}
```

`required_checks` may be a JSON array or comma-separated list through the MCP facade. `commit_sha` must be the full 40-character SHA. Den tracks that exact SHA, not the current branch head.

The response is the current gate record:

- `status`: `pending`, `passed`, `failed`, `timed_out`, or `superseded`.
- `status_url`: Review readback URL when configured.
- `check_runs`: required check run names, states, and URLs seen so far.
- `failure_summary`: compact failed-check summary when available.
- `evidence_message_status`: `not_required`, `pending`, `posted`, or `error`.

## Review HTTP API

Register a gate:

```http
POST /v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates
```

Read current status:

```http
GET /v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates/{commit_sha}
```

Both endpoints require the Review service token.

For the current high-trust local deployment, Review may be configured with
`allow_unauthenticated_local_dev: true`. In that mode, direct local HTTP
fallbacks do not need `Authorization`; the Review service still keeps its token
configured for MCP/backend callers.

## Evidence Behavior

Terminal gates append task-thread messages with one of these intents:

- `github_checks_passed`
- `github_checks_failed`
- `github_checks_timeout`
- `github_checks_superseded`

Failure messages include the failed check names and check run URLs. If the messages service is unavailable, Review keeps the terminal gate state durable and marks `evidence_message_status=error`; the watcher retries pending/error evidence before polling GitHub again.

Registering a newer pending SHA for the same project/task supersedes older pending gates. Terminal gates remain historical evidence.
