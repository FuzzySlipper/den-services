# Tasks Service Route-Flip Readiness

Task #3693 implements the first `den-services/tasks` HTTP service and the
`den_tasks` schema boundary from
[`docs/tasks-lifeboat-contract.md`](./tasks-lifeboat-contract.md).

No production MCP routes are flipped by this task. The service is ready for
local and staged verification, then a later explicit cutover task can update
`mcp/routes.example.yaml` and deployed MCP routing.

## Implemented Native Routes

- `POST /v1/projects/{project_id}/tasks`
- `GET /v1/projects/{project_id}/tasks`
- `GET /v1/projects/{project_id}/tasks/changes`
- `GET /v1/projects/{project_id}/tasks/changes/stream`
- `GET /v1/projects/{project_id}/tasks/next`
- `GET /v1/projects/{project_id}/tasks/{task_id}`
- `PATCH /v1/projects/{project_id}/tasks/{task_id}`
- `POST /v1/projects/{project_id}/tasks/{task_id}/dependencies`
- `DELETE /v1/projects/{project_id}/tasks/{task_id}/dependencies/{depends_on}`
- `GET /v1/tasks/{task_id}`
- `PATCH /v1/tasks/{task_id}`
- `POST /v1/tasks/{task_id}/dependencies`
- `DELETE /v1/tasks/{task_id}/dependencies/{depends_on}`
- `GET /v1/tasks/{task_id}/history`

## MCP Route Staging

Ready after import and parity smoke:

- `create_task`
- `list_tasks`
- `update_task`
- `next_task`
- `add_dependency`
- `remove_dependency`

Still staged:

- `get_task`

`get_task_workflow_summary` is now MCP-composed from tasks, review, and message
packet-header successor reads. `get_task` still includes message and review
projections. The tasks service owns only task lifecycle, dependencies, subtasks,
availability, and history, so MCP should keep `get_task` Core-routed or compose
it from message/review successor APIs when those exist.

## Required Before Cutover

1. Apply `den_tasks` migrations and app-role bootstrap in a staging database.
2. Import/sync existing `den_core.tasks`, `den_core.task_dependencies`, and
   `den_core.task_history`.
3. Compare Core and tasks-service results for default and filtered task lists.
4. Run temporary create/update/block/dependency/subtask/next/history flows.
5. Verify projects-service `assert-writable` blocks archived project writes.
6. Update MCP route mapping only in an explicit cutover task.
