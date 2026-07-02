# Task Workflow Summary Route Readiness

Task: 3889

`get_task_workflow_summary` is composed in `den-services/mcp` from successor
read APIs:

- tasks: `GET /v1/tasks/{task_id}` for task metadata, dependencies, subtasks,
  and history;
- review: `GET /v1/projects/{project_id}/tasks/{task_id}/review/workflow-summary`
  for review counts, current round/verdict, findings, and timeline;
- messages: `GET /v1/projects/{project_id}/tasks/{task_id}/packets/latest`
  for bounded packet headers only.

The route must not point at `den-core` in normal operation. MCP owns the
compatibility response shape; tasks does not read review or message SQL.

## Compact Response

The default response includes task status, dependency/subtask projections,
review counts/current state/open findings, latest packet headers without
message `content`, and source links. `verbose=true` adds task history, resolved
findings, review timeline, and raw source records for debugging.

Missing packet headers are optional and are returned as `null` entries. Missing
review rounds are represented by the review service's zero-count workflow
summary.

## Smoke

Local:

```sh
make mcp-smoke
```

Live on `den-srv`:

```sh
make mcp-smoke-live-den-srv
```

The live smoke uses loopback successor URLs for tasks, messages, review,
documents, guidance, and librarian, and asserts that
`get_task_workflow_summary` returns a composed successor summary.

## Rollback

If a production incident requires rollback, restore only this route in
`mcp/routes.example.yaml` and the deployed MCP route table:

```yaml
- operation: "get_task_workflow_summary"
  backend: "den-core"
  method: "POST"
  path: "/mcp"
  request_adapter: "mcp_tools_call"
  response_adapter: "mcp_jsonrpc_result"
```

Restart `den-go@mcp.service` after the route rollback and rerun the live smoke.
