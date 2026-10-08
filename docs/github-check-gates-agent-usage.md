# GitHub Check Gates Agent Usage

Use Den document `den-services/review-pointer-first-contract` for end-to-end
review routing. Ordinary agents in every harness (Codex, DSH, Claude Code)
call `submit_task_for_review` once per task; the crew-review service registers
and watches that task's gate and admits the reviewer when it passes. Do not
separately call `watch_github_checks` for a submitted task. The operations
below are the deliberate direct/unmanaged Den path and the typed operator
readback/recovery surface.

crew-review has no gate bypass. If GitHub Actions cannot make progress (an
outage or exhausted quota), tell the owner instead of removing required checks.

The Review-owned gate serves the low-ceremony agent flow:

```text
commit -> push -> register gate -> resume only on failure or completion evidence
```

Den does not run CI. GitHub Actions remains the CI runner. The Review service records a durable gate for an exact commit SHA, polls GitHub check runs for that SHA, and appends task-thread evidence when the gate passes, fails, times out, or is superseded. A fine-grained GitHub token with repository `Checks: read` is the preferred credential. If that token can read `Actions` but GitHub denies the check-runs endpoint, Review falls back to the exact-SHA workflow-run jobs endpoint; required names still match job names exactly.

## Checks from later commits and re-runs

A gate is for a task's commit on its `ref`, but its required checks may pass on
code that contains that commit rather than on the commit itself. When a
required check is missing, cancelled, or failed on the gated commit, Review
reads later commits of `ref` that contain it and accepts a passing run there.
It reads the later commits that have a passing or still-running Actions
workflow run, nearest first, plus the ref head, at most
`github.later_commit_limit` of them; without Actions access it reads the
nearest commits plus the head. Workflow runs are paged newest first until the
gated commit's own runs are reached (at most five pages of 100). These bounds
keep GitHub API use per poll small; the ref head is always read, so a check
that eventually passes on the head always satisfies the gate.
Several tasks can therefore land as separate commits in one push and share the
head's slow or serialized checks.

- Each entry in `check_runs` carries `head_sha`, the commit whose run
  satisfied or is deciding that check. Evidence messages name a later commit
  when one was used.
- A cancelled run counts as missing, not failed.
- A failure on a later commit keeps the check pending, because a later change
  may have caused it. The gate fails early only when the check failed on the
  gated commit and no later run passed or is still running; otherwise it fails
  at its timeout.
- Registering a failed or timed-out gate again re-reads its checks. If the
  result is no longer a failure (for example after a GitHub re-run or a later
  containing commit passed), the gate reopens as a new `attempt` and emits a
  new terminal event when it finishes. A still-failing result leaves the
  recorded terminal gate unchanged. A terminal gate keeps its stored `ref` and
  `required_checks` unless it reopens.

## MCP tools

When the exact GitHub job/check-run names are not already known, inspect the
exact pushed commit first:

```json
{
  "repository": "OWNER/REPO",
  "commit_sha": "0123456789abcdef0123456789abcdef01234567",
  "required_checks": ["optional exact name to validate"]
}
```

`discover_github_checks` is read-only. It does not require a Den task, change
task status, create or supersede a gate, post evidence, or alter any polling
deadline. It returns every latest-by-name check run currently visible for the
exact SHA, including status, conclusion, and URLs. When `required_checks` is
supplied, `configuration_status` is `valid` or
`missing_required_checks`, and the response includes exact observed candidates.
An empty observation means GitHub has not exposed check runs for that SHA yet;
it is not a successful gate.

Copy the intended exact names into `watch_github_checks` after discovery:

```json
{
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

`watch_github_checks` is intentionally non-blocking. It registers the durable gate for the commit and returns the deferral handle/current state. The older `await_github_checks` alias is deprecated; use `watch_github_checks`.

Repository check profiles are intentionally not part of this contract.
Discovery keeps configuration explicit and auditably exact without introducing
a second name-mapping system. Add profiles only if measured repeated usage
shows a problem that exact-SHA discovery does not solve.

Registering or retrying a gate preserves the referenced task's status. A gate
records CI evidence; it cannot reopen a completed or cancelled task, undo human
acceptance, or clear a blocker. The explicit review request owns the transition
into `review` (managed submissions perform that request before registering a gate).

Read the existing gate without changing its timeout, grace window, polling interval, or `next_poll_at`:

```json
{"task_id":4245,"commit_sha":"0123456789abcdef0123456789abcdef01234567"}
```

Use that payload with `get_github_check_gate`, or add `after_id` and `wait_ms` for `wait_for_github_checks`. The bounded wait is capped at 50 seconds and returns either terminal gate/event state or a typed progress receipt with `timed_out: true` and a reusable `next_cursor`. A direct Codex CLI session may issue another bounded wait using that cursor; it must not hot-loop or call the watch operation again. Managed runtimes should consume the project terminal-event cursor directly.

`required_checks` may be a JSON array or comma-separated list through the MCP facade. These values are exact GitHub **check-run/job names**, not workflow display names. For example, a workflow named `ASHA Studio CI` may expose the required check run as `Verify ASHA Studio`. `commit_sha` must be the full 40-character SHA. Den tracks that exact commit as the gate's identity, not the current branch head; checks may still be satisfied by later commits that contain it (see above).

Review records every check-run name observed for the exact SHA. When requested names are missing, the response includes `missing_required_checks`, `observed_check_runs`, and candidate names in `summary`. If GitHub has exposed terminal runs but the requested names still do not appear after the configured grace period, the gate fails with `terminal_reason=required_checks_missing` instead of consuming the full gate timeout. It does not fail this way while any Actions workflow run on the ref for the gated commit or a consulted later commit is still queued or running, because a run waiting in a concurrency group has no check runs yet. Matching remains exact; Review does not guess or fuzzy-match workflow labels.

The response is the current gate record:

- `status`: `pending`, `passed`, `failed`, `timed_out`, or `superseded`.
- `status_url`: Review readback URL when configured.
- `check_runs`: required check run names, states, URLs, and the `head_sha` that supplied each.
- `attempt`: evaluation attempt; increases when a failed or timed-out gate reopens.
- `observed_check_runs`: all latest-by-name check runs GitHub exposed for the SHA, including unrequested runs.
- `missing_required_checks`: requested names that GitHub has not exposed.
- `terminal_reason`: stable machine reason for terminal status, including `required_checks_missing`.
- `failure_summary`: compact failed-check summary when available.
- `evidence_message_status`: `not_required`, `pending`, `posted`, or `error`.

## Review HTTP API

Discover check runs without creating workflow state:

```http
POST /v1/review/github-checks/discover
```

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

These endpoints require the Review service token.

For the current high-trust local deployment, Review may be configured with
`allow_unauthenticated_local_dev: true`. In that mode, direct local HTTP
fallbacks do not need `Authorization`; the Review service still keeps its token
configured for MCP/backend callers.

## Evidence Behavior

Review scans its database for due gates on `github.scan_interval` (5 seconds by default). Each gate retains its own `next_poll_at` and `poll_interval_seconds` (at least 30 seconds), so faster local scans reduce timer-alignment delay without increasing GitHub API frequency. Due gates are drained across batches; a transport failure is recorded on that gate with a future retry and does not stop unrelated gates.

GitHub evaluation completes before task-message evidence retries run. A Messages outage can delay the human projection but cannot block polling or terminal event creation for other gates. Structured logs report scan backlog/duration, API results, throttling and retry reasons, check queue/run time, Review detection lag, and evidence lag.

Review classifies GitHub HTTP failures before selecting a bounded retry. A 403 with exhausted primary quota follows the primary reset header; a primary/secondary throttle follows `Retry-After` when supplied; and a 403 with quota remaining is a permission denial, not a rate limit. Permission failures use a separate bounded backoff and are reported as `permission_denied`, while a readable Actions fallback continues terminal reconciliation without waiting for credential changes.

Terminal gates append task-thread messages with one of these intents:

- `github_checks_passed`
- `github_checks_failed`
- `github_checks_timeout`
- `github_checks_superseded`

Failure messages include the failed check names and check run URLs. They are authored by `den-review`; the requester and agent/session correlation remain typed metadata so the projection is not self-authored. If the messages service is unavailable, Review keeps the terminal gate state durable and marks `evidence_message_status=error`; the watcher evaluates all due GitHub gates before running the isolated evidence-retry phase.

Registering a newer pending SHA for the same project/task supersedes older pending gates. Terminal gates remain historical evidence unless the same SHA is registered again and reopens as described above.

`get_review_context` does not fetch gates. In the managed path the crew-review submission enforces the gate before admitting a reviewer; direct callers read gate state with the operations above.
