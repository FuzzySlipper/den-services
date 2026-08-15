# den-tool MCP parity inventory

## Status

As of MCP catalog revision `mcp-catalog-v3`, `den-tool` maps every operation in
the MCP direct discovery profile. The checked-in machine-readable inventory is
`cmd/den-tool/mcp_catalog.json`; it contains 86 operations and their exact input
schemas, owning routed backend, workflow tier, description, and risk class.

This inventory is generated from `mcp/internal/registry` plus
`mcp/routes.example.yaml`:

```sh
go run ./mcp/cmd/den-tool-catalog \
  -output ./cmd/den-tool/mcp_catalog.json
```

`mcp/internal/registry/den_tool_parity_test.go` fails when a direct-profile MCP
operation is added, removed, or changed without regenerating the CLI inventory.
There is no parity allowlist. Registry entries hidden from `tools/list` are
retired, administrative, or compatibility-only operations and are not current
supported MCP discovery operations, so they are intentionally outside this
inventory.

## Invocation contract

Every MCP operation has a stable catalog ID `den.<operation>` and two equivalent
invocation forms:

```sh
den-tool den get_task --task-id 7011
den-tool run den.get_task -- --task-id 7011
```

Field flags accept either schema spelling (`--task_id`) or a hyphenated spelling
(`--task-id`). Arrays and objects are JSON values. For generated or complex
payloads, callers may pass one complete object with `--args-json`; it cannot be
mixed with field flags. The CLI rejects unknown fields, missing required fields,
and top-level type mismatches before making a request.

The default transport is `http://192.168.1.10:5199/mcp`, overrideable with
`DEN_MCP_URL`; optional bearer authentication uses `DEN_MCP_TOKEN`. MCP remains
the stable authenticated LAN transport because owning service ports are
loopback-only. The facade routes each typed operation to its owning service; the
CLI does not write schemas or domain storage directly. This route remains usable
when an operation is later excluded by a discovery profile because profile
projection does not change backend call authority.

Results are JSON. A response larger than 1 MiB is rejected rather than streamed
unbounded into an agent context. Domain adapters retain their existing bounded
summaries and detail-reference behavior within that outer limit.

## Inventory summary

| Owning backend | Count |
| --- | ---: |
| board | 9 |
| documents | 15 |
| guidance | 4 |
| handoff | 2 |
| knowledge | 5 |
| librarian | 1 |
| mcp-facade | 1 |
| messages | 12 |
| projects | 9 |
| review | 17 |
| tasks | 11 |
| **Total** | **86** |

| Risk | Count | Meaning |
| --- | ---: | --- |
| read | 44 | Observation or bounded wait; no requested durable mutation. |
| write | 36 | Creates or updates durable/workflow state. |
| destructive | 6 | Purges, deletes, or archives accessible state. |

The destructive set is explicit: `archive_space`,
`delete_agent_guidance_entry`, `delete_document`, `den_knowledge_delete`,
`purge_board_comment`, and `purge_board_post`.

`den-tool` also retains non-MCP catalog entries, including Board search and
repository utilities. They do not count toward MCP parity.

## Input to MCP pruning

The later pruning task can remove uncommon operations from model-facing
`tools/list` without making them undiscoverable: `den-tool search`,
`den-tool describe den.<operation>`, and `den-tool den <operation>` remain the
agent path. Pruning must be a separate profile/registry change. This parity work
does not hide, retire, rename, or remove any MCP operation.
