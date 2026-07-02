# Delete Space Admin Route Readiness

Task: 3881

`delete_space` is no longer a visible Core-routed MCP dependency. The supported
successor path is an admin-only projects route:

```text
POST /v1/admin/spaces/{id}/delete
```

The route is intentionally named under `/v1/admin`. It is not a normal lifecycle
operation. Prefer `archive_space` or `update_space_visibility` for ordinary
space removal.

## MCP Behavior

Default MCP `tools/list` hides `delete_space`. The tool remains resolvable for
stale or explicit admin direct calls and routes to the projects successor:

```yaml
- operation: "delete_space"
  backend: "projects"
  method: "POST"
  path: "/v1/admin/spaces/{space_id}/delete"
  request_adapter: "mcp_projects_rest"
  response_adapter: "mcp_tool_result_json"
```

Protected scopes (`_global`, `den-core`, `core`, `kind=system`, and
`kind=personal`) require `force=true`. The response includes the deleted space
record plus an empty `dependency_counts` object with
`dependency_counts_complete=false`; projects owns scope metadata and must not
query dependent service tables directly. Use archive preflight or
domain-specific checks before deleting real scopes.

The admin route requires the projects app role to hold `DELETE` on only
`den_projects.projects`. That privilege is granted by
`migration/postgres/den_projects/002_admin_delete_privilege.sql`; it is not a
general default table privilege.

## Smoke

Run:

```sh
make test
make mcp-smoke
```

For deployment, restart only `projects` and `mcp`, then run:

```sh
make mcp-smoke-live-den-srv
```

## Rollback

If the admin route must be rolled back, restore only this MCP route:

```yaml
- operation: "delete_space"
  backend: "den-core"
  method: "POST"
  path: "/mcp"
  request_adapter: "mcp_tools_call"
  response_adapter: "mcp_jsonrpc_result"
```

Then restart `den-go@mcp.service`. The projects admin route can remain dormant;
it is unreachable from MCP after the route rollback.
