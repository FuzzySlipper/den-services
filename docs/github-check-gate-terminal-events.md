# GitHub check gate terminal events

Review publishes an immutable, versioned terminal event for each exact-commit GitHub check gate. The event is the machine wake authority for orchestrators. Task-thread messages remain a human-readable projection and may be delivered later or retried independently.

Review never schedules, suspends, or resumes an agent. A runtime such as Rusty Crew consumes these facts and owns those decisions.

## Read and bounded wait

```http
GET /v1/projects/{project_id}/review/github-check-gate-events?after_id=41&task_id=5496&limit=50&wait_ms=30000
```

- `after_id` is the last event ID durably handled by the consumer. Omit it or use `0` for the beginning.
- `task_id` is optional. Omit it to consume all terminal gates in a project.
- `limit` defaults to 50 and is capped at 100.
- `wait_ms` is optional. The server clamps it to `github.event_wait_max` (55 seconds by default). An empty bounded wait returns `timed_out: true` and leaves `next_cursor` unchanged.
- The server returns events ordered by ascending global event ID.

```json
{
  "events": [
    {
      "id": 42,
      "schema": "den_review.github_check_gate_terminal_event",
      "schema_version": 1,
      "gate_id": 123,
      "project_id": "den-services",
      "task_id": 5496,
      "repository": "FuzzySlipper/den-services",
      "commit_sha": "0123456789abcdef0123456789abcdef01234567",
      "ref": "main",
      "status": "passed",
      "terminal_reason": "checks_passed",
      "required_checks": ["Verify"],
      "check_runs": [{"name":"Verify","status":"completed","conclusion":"success","details_url":"https://github.com/example/check/1"}],
      "observed_check_runs": [{"name":"Verify","status":"completed","conclusion":"success","details_url":"https://github.com/example/check/1"}],
      "missing_required_checks": [],
      "summary": "All required GitHub checks passed.",
      "requested_by": "codex",
      "agent_profile": "codex-cli",
      "agent_instance_id": "agent-7",
      "session_key": "thread-9",
      "gate_created_at": "2026-07-09T20:00:00Z",
      "completed_at": "2026-07-09T20:02:00Z",
      "created_at": "2026-07-09T20:02:00Z"
    }
  ],
  "next_cursor": 42,
  "timed_out": false
}
```

## Delivery and recovery semantics

Events are append-only and written atomically with the gate's terminal transition. A unique `gate_id` constraint guarantees that retries cannot create a second terminal event for the same gate.

Consumption is at least once:

1. Read after the consumer's durable cursor.
2. Handle events in ascending ID order.
3. Persist the event ID only after the corresponding runtime action is durable.
4. Reconnect with that ID as `after_id` after restart or transport failure.

Consumers must treat `id` as the idempotency key. Re-reading an event is expected and must not resume a session twice. No acknowledgement is stored by Review because each runtime owns its own progress.

## Terminal and supersession semantics

Version 1 emits compatibility statuses `passed`, `failed`, `timed_out`, and `superseded`. The stable `terminal_reason` distinguishes:

- `checks_passed`
- `checks_failed`
- `required_checks_missing`
- `timeout`
- `superseded`

Registering a newer SHA for the same project/task atomically supersedes every older pending SHA before registering the new gate. Those supersession events receive lower cursor IDs than any later terminal event for the new gate. Already-terminal gates are immutable and are not superseded retroactively.

Exact requested check runs appear in `check_runs`; every latest-by-name run GitHub exposed for the SHA appears in `observed_check_runs`. `missing_required_checks` and the observed check URLs make invalid configuration actionable without another GitHub query.

Schema changes require a new `schema_version`. Additive fields may be introduced within a version only when old consumers can safely ignore them.
