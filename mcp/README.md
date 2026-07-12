# den-services/mcp

`den-services/mcp` is the successor MCP facade for the legacy `den-mcp`
surface. It exposes the same static tool discovery shape while proxying tool
execution to the configured Den backend.

## Task-context briefing

`get_task_context(task_id)` is the bounded, read-only startup
composition for an agent beginning, resuming, investigating, or reviewing a
Den task. The canonical task read supplies the project scope for every composed
source; callers do not provide or assert a project. It is additive: use
`get_task`, `get_task_workflow_summary`, `get_agent_guidance`,
`query_librarian`, and `get_messages` when a source handle needs deeper
follow-up.

The result is versioned (`schema_version`) and contains the canonical task,
bounded dependencies/subtasks and recent task-thread messages, review state,
guidance document handles, librarian references/recommendations, deterministic
`search_hints`, `limits`/`truncated` markers, and per-source `source_status`.
It does not copy guidance document bodies or introduce a cache/projection.

The canonical task is fail-closed: a missing task or malformed task response is
a tool error. Workflow, task-thread, guidance, and
librarian reads are optional context sources. Their failure returns a partial
packet only with a `source_status` entry whose state is `partial` or
`unavailable`, plus an error code and retryability. An empty source with `ok`
status is not an unavailable source.

The facade adds no caller impersonation, credentials, or visibility bypass:
every source read uses the existing configured backend route and its normal
access behavior. Do not treat this ergonomic composition as a security
boundary. Follow document/message handles on demand and stop under the
project's Den-connectivity policy when the packet cannot be read.

## Concise results and intentional details

Normal tool discovery does not repeat a `verbose` parameter across resource
tools. Concise read tools that have a meaningful deeper representation return
an opaque `detail_ref`; pass that reference to `get_details` when the full
record is intentionally needed. Detail references expire, contain only an
allowlisted set of non-secret identity/filter arguments, and can dispatch only
to read-only tools.

Legacy callers may continue sending `verbose` directly during migration, but
it is hidden from discovery and new callers should use `get_details`.

## Tool usage reports

The facade emits one privacy-safe `mcp_tool_call` JSON log event per call with
tool name, backend, outcome, retryability, and duration. Arguments, response
content, tokens, and backend error bodies are never logged.

On a host with journal access, summarize usage over a bounded window with:

```sh
mcp/scripts/tool_usage_report.sh --since "7 days ago"
```

Use `--until` and `--unit` to override the end time or systemd unit.

## Hermes Stability Smoke

Run the local smoke harness before MCP cutover work:

```sh
make mcp-smoke
```

The default smoke is fully loopback and disposable. It starts:

- one temporary fake den-core backend;
- one `den-services/mcp` process on an alternate loopback port;
- a second fake-backend outage/recovery phase against the same MCP process.

Expected output contains these checkpoints:

```text
ok: local initialize
ok: local tools/list returned 69 tools
ok: local read tool proxied through backend
ok: local non-representative tool proxied through backend
ok: local get_agent_guidance returned MCP-compatible successor shape
ok: local list_agent_guidance_entries returned MCP-compatible array shape
ok: local query_librarian proxied to librarian successor
ok: local write tool proxied through backend and restored disposable state
ok: mcp /health stayed healthy during backend outage
ok: tools/list remained identical while backend was unavailable
ok: backend outage returned retryable den_backend_unavailable
ok: backend recovered in the same MCP process
ok: hermes stability smoke complete
```

To add an opt-in live smoke, pass `--mode both` and set the live backend URLs
explicitly. The route table now contains both legacy MCP-routed Core tools and
REST-routed successor services, so do not point every backend at the old MCP
facade. The harness starts a temporary `den-services/mcp` locally, uses
`DEN_MCP_SMOKE_DEN_CORE_URL` for remaining Core-routed tools, and uses the
successor URLs for the REST-routed smoke calls.

Live smoke requires those successor services to already be deployed and
reachable from the machine running the harness. Successor services are
loopback-bound on den-srv, so run the live smoke on den-srv or use the SSH helper
below from a development host. Do not use `192.168.1.10:8092` style LAN URLs for
the successor backends.

From a development host with SSH access to den-srv:

```sh
make mcp-smoke-live-den-srv
```

Directly on den-srv:

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://127.0.0.1:5299 \
DEN_MCP_SMOKE_TASKS_URL=http://127.0.0.1:8092 \
DEN_MCP_SMOKE_DOCUMENTS_URL=http://127.0.0.1:8094 \
DEN_MCP_SMOKE_GUIDANCE_URL=http://127.0.0.1:8097 \
DEN_MCP_SMOKE_LIBRARIAN_URL=http://127.0.0.1:8098 \
DEN_MCP_SMOKE_READ_TASK_ID=3446 \
python3 mcp/scripts/hermes_smoke.py --mode both
```

Or use the live-only Make target:

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://127.0.0.1:5299 \
DEN_MCP_SMOKE_TASKS_URL=http://127.0.0.1:8092 \
DEN_MCP_SMOKE_DOCUMENTS_URL=http://127.0.0.1:8094 \
DEN_MCP_SMOKE_GUIDANCE_URL=http://127.0.0.1:8097 \
DEN_MCP_SMOKE_LIBRARIAN_URL=http://127.0.0.1:8098 \
DEN_MCP_SMOKE_READ_TASK_ID=3446 \
make mcp-smoke-live
```

Expected live output includes:

```text
ok: live initialize
ok: live tools/list returned 69 tools
ok: live read tool proxied to tasks successor
ok: live non-representative tool proxied to documents successor
ok: live get_agent_guidance returned MCP-compatible successor shape
ok: live list_agent_guidance_entries returned MCP-compatible array shape
ok: live query_librarian proxied to librarian successor
```

Live write smoke is disabled unless a pre-existing disposable document target
is provided. The current disposable fixture is `den-services/mcp-smoke-disposable`.
The harness reads the document first, writes smoke content through
`store_document`, verifies the write through `get_document`, and restores the
original document before exiting:

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://127.0.0.1:5299 \
DEN_MCP_SMOKE_TASKS_URL=http://127.0.0.1:8092 \
DEN_MCP_SMOKE_DOCUMENTS_URL=http://127.0.0.1:8094 \
DEN_MCP_SMOKE_GUIDANCE_URL=http://127.0.0.1:8097 \
DEN_MCP_SMOKE_LIBRARIAN_URL=http://127.0.0.1:8098 \
DEN_MCP_SMOKE_WRITE_PROJECT=den-services \
DEN_MCP_SMOKE_WRITE_SLUG=mcp-smoke-disposable \
python3 mcp/scripts/hermes_smoke.py --mode both
```

The live mode passes backend service tokens through to the MCP process when
their normal service-token variables are set, such as `DEN_CORE_SERVICE_TOKEN`,
`DEN_TASKS_SERVICE_TOKEN`, `DEN_DOCUMENTS_SERVICE_TOKEN`, and
`DEN_GUIDANCE_SERVICE_TOKEN`, and `DEN_LIBRARIAN_SERVICE_TOKEN`. Token values
are never printed by the harness.
