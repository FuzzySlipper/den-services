# Task Change Stream Route Readiness

Task #4197 adds a canonical successor task-change stream for den-web task-list freshness.

## Routes

The tasks service owns both routes:

- `GET /v1/projects/{project_id}/tasks/changes?after=<cursor>&limit=<n>`
- `GET /v1/projects/{project_id}/tasks/changes/stream?after=<cursor>&limit=<n>`

The Gateway should route browser-facing reads/SSE through the successor tasks service, not legacy den-core or den-channels. The matching Gateway route is a `GET` successor route over `/v1/projects/{project_id}/tasks` with the tasks read caller token and tasks upstream token.

Do not use a broad `/v1/projects/` Gateway route for this cutover. Gateway routes match by prefix, so the task-stream route must stay scoped to the task subtree to avoid stealing unrelated project reads such as documents, messages, librarian queries, and project detail. The route-table regression `TestRouteTableProjectTaskTemplateDoesNotCatchOtherProjectReads` covers this behavior.

## Event Shape

Backfill responses return:

```json
{
  "events": [
    {
      "event_id": 42,
      "cursor": "42",
      "kind": "updated",
      "changed_at": "2026-07-05T05:00:00Z",
      "task_id": 4177,
      "project_id": "den-services",
      "task": {
        "id": 4177,
        "project_id": "den-services",
        "parent_id": null,
        "title": "Example",
        "status": "review",
        "priority": 2,
        "updated_at": "2026-07-05T05:00:00Z",
        "dependency_count": 1,
        "unfinished_dependency_count": 0,
        "subtask_count": 3,
        "availability": "review"
      }
    }
  ],
  "next_cursor": "42"
}
```

SSE uses the same payload for `task_change` events and sets the SSE `id:` field to the cursor:

```text
id: 42
event: task_change
data: {"event_id":42,"cursor":"42",...}
```

The stream also emits:

- `stream_open`: stream metadata, supported event names, heartbeat interval, and backfill URL.
- `heartbeat`: current server time and cursor while the connection is idle.

## Reconnect And Backfill

Clients should store the latest `cursor` or SSE `Last-Event-ID`.

Recommended den-web behavior:

1. Open `GET /v1/projects/{project_id}/tasks/changes/stream`.
2. Apply each `task_change.task` summary to the task-list row and selected task header/status.
3. Store the newest cursor from either the SSE `id:` line or payload `cursor`.
4. On reconnect, call `/v1/projects/{project_id}/tasks/changes?after=<cursor>` before reopening the stream, or reopen `/stream?after=<cursor>`.
5. If the backfill call fails, the cursor is missing, or the client detects a gap it cannot reconcile, fall back to the existing full task-list refresh and then reopen the stream from the newest known cursor.

`after` is exclusive: `after=42` returns events with event IDs greater than 42.

## Semantics

Events are summary invalidation rows, not executable work. Reading or replaying the stream must never create, claim, wake, retry, or complete work.

The tasks service emits changes for:

- task creation;
- direct task updates;
- dependency add/remove for the blocked task;
- dependent task summaries when a dependency status changes;
- parent task summaries when subtask counts can change.

This is intentionally a task-list freshness stream, not an audit log. Historical audit remains `task_history`.
