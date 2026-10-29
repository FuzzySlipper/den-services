---
name: den-tool-cli
description: Discover and invoke Den operations through the globally installed `den-tool` CLI. Use when an agent needs Den tasks, reviews, messages, documents, Knowledge, Board, backup status (Ops), handoffs, guidance, or long-tail MCP operations from a shell; when a Den operation is missing from ordinary MCP discovery; or when diagnosing whether Den tooling is installed and reachable. Prefer an already available native Den MCP tool for the same operation, and use this CLI as the discoverable shell and fallback surface.
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
   den-tool search handoff --json
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
den-tool den send_message --args-json \
  '{"project_id":"den-services","task_id":10442,"sender":"codex","content":"CI duration alert"}'
```

Do not mix `--args-json` with field flags. The CLI validates unknown fields,
missing required fields, and top-level types before sending the request.

Board also has a convenience surface:

```sh
den-tool board search --project den-services --query threading
den-tool board get-post --post-id 42 --json
```

Backup and maintenance status lives in Ops (`docs/ops-api-contract.md`). It
talks HTTP to `DEN_OPS_URL` (default web-edge `http://192.168.1.5:18080/api`):

```sh
den-tool ops status            # every job, worst first; --json is the API body
den-tool ops job den-agents-git
den-tool ops errors
den-tool ops run --job JOB --engine borg -- COMMAND ARGS   # wrap and report a run
den-tool ops replay            # deliver reports spooled during an Ops outage
den-tool ops collect --dry-run # show what the den-agents collector would report
```

On den-agents, `den-ops-collect.timer` runs `den-tool ops collect` every five
minutes. It reads existing evidence (Borg runner files, thin config reports,
the SnapRAID journal, Proxmox vzdump logs) and never runs a job itself.

Ops only records what machines report. Never start, rerun or "fix" a backup
because a status says it is overdue or failed; tell the owner instead.

Workshop machine activity, shared installations and work check-ins live in
the Resources service (`docs/resources-api-contract.md`; page at
`http://192.168.1.5:18080/resources`). `asset-resources` is a wrapper for the
same commands:

```sh
den-tool resources status      # machines, native queues, current check-ins
den-tool resources resources m5-comfy   # one shared install and its notes
den-tool resources run --host den-m5 --resource m5-comfy --purpose 'Generate' -- COMMAND
den-tool resources check-in --host den-nimo --purpose 'Async job' --ttl 600
```

Wrap only commands that wait for their real work to finish. Claims are
advisory; "quiet" or "stale" never means a machine or install is free.

## Respect authority and risk

- Treat `list`, `search`, and `describe` as local read-only discovery.
- Read the described operation's `risk` before invocation. Read operations are
  diagnostic; write and destructive operations still require authority from
  the user's request and the normal Den workflow.
- Prefer bounded reads and follow returned detail references instead of dumping
  broad datasets into context.
- Preserve exact task, review-round, and revision semantics. A CLI call does
  not weaken the owning Den service's lifecycle rules.
- Use the configured `DEN_MCP_URL` (no token is needed; Den MCP does not
  authenticate callers) without printing environment contents. Direct owning-service URLs are operator diagnostics,
  not the normal agent path.
- Preserve JSON output as evidence when diagnosing transport or backend errors;
  distinguish an outer CLI/HTTP failure from a Den tool result marked as an
  error.

## Understand the transport boundary

`den-tool` talks to the Den MCP gateway, normally through the LAN endpoint or
an explicit `DEN_MCP_URL`. The gateway routes typed operations to their owning
services. Discovery class controls catalog projection, not backend authority;
supported long-tail operations remain callable by exact name.
