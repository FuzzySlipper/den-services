# Projects Service Route-Flip Readiness

Task #3692 implements the first `den-services/projects` HTTP service and the
`den_projects.projects` schema boundary from
[`docs/projects-scope-lifeboat-contract.md`](./projects-scope-lifeboat-contract.md).

No production MCP routes are flipped by this task. The service is ready for
local and staged verification, then a later explicit cutover task can update
`mcp/routes.example.yaml` and deployed MCP routing.

## Implemented Native Routes

- `POST /v1/projects`
- `GET /v1/projects`
- `GET /v1/projects/{id}`
- `PATCH /v1/projects/{id}`
- `POST /v1/spaces`
- `GET /v1/spaces`
- `GET /v1/spaces/{id}`
- `PATCH /v1/spaces/{id}/visibility`
- `POST /v1/spaces/{id}/archive`
- `POST /v1/admin/spaces/{id}/delete`
- `GET /v1/scopes/{id}`
- `POST /v1/scopes/{id}/assert-writable`

The delete route is an admin-only escape hatch. It is not a green-path lifecycle
route; ordinary callers should use `archive_space` or
`update_space_visibility`.

## MCP Route Staging

Ready after import and parity smoke:

- `create_project`
- `list_projects`
- `update_project`
- `create_space`
- `list_spaces`
- `update_space_visibility`
- `archive_space`

Still staged:

- `get_project`
- `get_space`

Those two Core tools currently return task counts and agent unread-message
counts. The projects service only owns scope metadata, so MCP should keep those
tools Core-routed or compose them from task/message successor APIs when those
exist.

Do not green-path:

- `delete_space`

Deletion remains operator/admin-only and hidden from default MCP discovery.
Stale direct MCP calls route to the projects admin endpoint rather than Core.
Archive or visibility updates are the normal lifecycle path.

## Required Before Cutover

1. Apply `den_projects` migrations and app-role bootstrap in a staging database.
2. Import/sync existing `den_core.projects` rows into `den_projects.projects`.
3. Compare Core and projects-service results for default and filtered
   project/space lists.
4. Run temporary create/update/archive/unarchive flows through the projects
   service.
5. Verify archived-scope `assert-writable` conflicts and restore behavior.
6. Update MCP route mapping only in an explicit cutover task.
