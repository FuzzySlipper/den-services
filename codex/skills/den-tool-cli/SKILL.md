---
name: den-tool-cli
description: Discover and invoke Den operations through the globally installed `den-tool` CLI. Use when an agent needs Den tasks, reviews, messages, documents, Knowledge, Board, handoffs, guidance, notifications, or long-tail MCP operations from a shell; when a Den operation is missing from ordinary MCP discovery; or when diagnosing whether Den tooling is installed and reachable. Prefer an already available native Den MCP tool for the same operation, and use this CLI as the discoverable shell and fallback surface.
---

# Den Tool CLI

Use `den-tool` as the typed shell facade over Den MCP. It carries the complete
supported operation catalog, including long-tail operations omitted from an
agent's ordinary MCP projection. It does not access service databases directly.

## Discover before invoking

1. Confirm installation without changing anything:

   ```sh
   den-tool --version
   ```

   If it is absent and `/home/dev/den-services` is available, report that the
   repository installer is `scripts/install-den-tool.sh`. Do not silently
   replace an unrelated executable.

2. Search the embedded catalog with task vocabulary:

   ```sh
   den-tool search review context
   den-tool search notification --json
   ```

3. Describe the exact catalog ID before first use. Check its risk, required
   fields, input schema, availability, and examples:

   ```sh
   den-tool describe den.get_review_context
   ```

Do not conclude that an operation is unavailable merely because it is absent
from native MCP discovery. Search the complete CLI catalog first.

## Invoke a Den operation

Prefer an equivalent native Den MCP tool when it is already callable in the
current agent. Otherwise invoke the typed CLI operation:

```sh
den-tool den get_task --task-id 7011
den-tool run den.get_task -- --task-id 7011
```

Flags accept schema spelling or kebab case. Arrays and objects are JSON values.
For nested, generated, or otherwise complex input, pass one complete object:

```sh
den-tool den send_user_notification --args-json \
  '{"project_id":"den-services","sender":"codex","content":"CI duration alert","urgency":"high"}'
```

Do not mix `--args-json` with field flags. The CLI validates unknown fields,
missing required fields, and top-level types before sending the request.

Board also has a convenience surface:

```sh
den-tool board search --project den-services --query threading
den-tool board get-post --post-id 42 --json
```

## Respect authority and risk

- Treat `list`, `search`, and `describe` as local read-only discovery.
- Read the described operation's `risk` before invocation. Read operations are
  diagnostic; write and destructive operations still require authority from
  the user's request and the normal Den workflow.
- Prefer bounded reads and follow returned detail references instead of dumping
  broad datasets into context.
- Preserve exact task, review-round, and revision semantics. A CLI call does
  not weaken the owning Den service's lifecycle rules.
- Use the configured `DEN_MCP_URL` and `DEN_MCP_TOKEN` without printing tokens
  or environment contents. Direct owning-service URLs are operator diagnostics,
  not the normal agent path.
- Preserve JSON output as evidence when diagnosing transport or backend errors;
  distinguish an outer CLI/HTTP failure from a Den tool result marked as an
  error.

## Understand the transport boundary

`den-tool` talks to the Den MCP gateway, normally through the LAN endpoint or
an explicit `DEN_MCP_URL`. The gateway routes typed operations to their owning
services. Discovery class controls catalog projection, not backend authority;
supported long-tail operations remain callable by exact name.
