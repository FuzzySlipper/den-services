# GitHub Check Gates Agent Usage

Task #4245 adds a Review-owned GitHub check gate for the low-ceremony agent flow:

```text
commit -> push -> register gate -> resume only on failure or completion evidence
```

Den does not run CI. GitHub Actions remains the CI runner. The Review service records a durable gate for an exact commit SHA, polls GitHub check runs for that SHA, and appends task-thread evidence when the gate passes, fails, times out, or is superseded.

## MCP tools

Use `watch_github_checks` after pushing:

```json
{
  "project_id": "den-services",
  "task_id": 4245,
  "repository": "OWNER/REPO",
  "commit_sha": "0123456789abcdef0123456789abcdef01234567",
  "ref": "main",
  "required_checks": ["go test", "lint"],
  "requested_by": "codex",
  "timeout_seconds": 7200,
  "poll_interval_seconds": 120,
  "agent_profile": "codex",
  "agent_instance_id": "optional-runtime-instance",
  "session_key": "optional-session"
}
```

`watch_github_checks` is intentionally non-blocking. It registers the durable exact-SHA gate and returns the deferral handle/current state. `await_github_checks` remains available for compatibility through the migration window, but is deprecated because it historically returned immediately despite its name.

Read the existing gate without changing its timeout, grace window, polling interval, or `next_poll_at`:

```json
{"project_id":"den-services","task_id":4245,"commit_sha":"0123456789abcdef0123456789abcdef01234567"}
```

Use that payload with `get_github_check_gate`, or add `after_id` and `wait_ms` for `wait_for_github_checks`. The bounded wait is capped at 50 seconds and returns either terminal gate/event state or a typed progress receipt with `timed_out: true` and a reusable `next_cursor`. A direct Codex CLI session may issue another bounded wait using that cursor; it must not hot-loop or call the watch operation again. Managed runtimes should consume the project terminal-event cursor directly.

`required_checks` may be a JSON array or comma-separated list through the MCP facade. These values are exact GitHub **check-run/job names**, not workflow display names. For example, a workflow named `ASHA Studio CI` may expose the required check run as `Verify ASHA Studio`. `commit_sha` must be the full 40-character SHA. Den tracks that exact SHA, not the current branch head.

Review records every check-run name observed for the exact SHA. When requested names are missing, the response includes `missing_required_checks`, `observed_check_runs`, and candidate names in `summary`. If GitHub has exposed terminal runs but the requested names still do not appear after the configured grace period, the gate fails with `terminal_reason=required_checks_missing` instead of consuming the full gate timeout. Matching remains exact; Review does not guess or fuzzy-match workflow labels.

The response is the current gate record:

- `status`: `pending`, `passed`, `failed`, `timed_out`, or `superseded`.
- `status_url`: Review readback URL when configured.
- `check_runs`: required check run names, states, and URLs seen so far.
- `observed_check_runs`: all latest-by-name check runs GitHub exposed for the SHA, including unrequested runs.
- `missing_required_checks`: requested names that GitHub has not exposed.
- `terminal_reason`: stable machine reason for terminal status, including `required_checks_missing`.
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

Bounded wait on an existing task/commit gate:

```http
GET /v1/projects/{project_id}/tasks/{task_id}/review/github-check-gates/{commit_sha}/wait?after_id=41&wait_ms=45000
```

Both endpoints require the Review service token.

For the current high-trust local deployment, Review may be configured with
`allow_unauthenticated_local_dev: true`. In that mode, direct local HTTP
fallbacks do not need `Authorization`; the Review service still keeps its token
configured for MCP/backend callers.

## Evidence Behavior

Review scans its database for due gates on `github.scan_interval` (5 seconds by default). Each gate retains its own `next_poll_at` and `poll_interval_seconds` (at least 30 seconds), so faster local scans reduce timer-alignment delay without increasing GitHub API frequency. Due gates are drained across batches; a transport failure is recorded on that gate with a future retry and does not stop unrelated gates.

GitHub evaluation completes before task-message evidence retries run. A Messages outage can delay the human projection but cannot block polling or terminal event creation for other gates. Structured logs report scan backlog/duration, API results, throttling and retry reasons, check queue/run time, Review detection lag, and evidence lag.

Terminal gates append task-thread messages with one of these intents:

- `github_checks_passed`
- `github_checks_failed`
- `github_checks_timeout`
- `github_checks_superseded`

Failure messages include the failed check names and check run URLs. They are authored by `den-review`; the requester and agent/session correlation remain typed metadata so the projection is not self-authored. If the messages service is unavailable, Review keeps the terminal gate state durable and marks `evidence_message_status=error`; the watcher evaluates all due GitHub gates before running the isolated evidence-retry phase.

Registering a newer pending SHA for the same project/task supersedes older pending gates. Terminal gates remain historical evidence.
