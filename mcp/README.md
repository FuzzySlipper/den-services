# den-services/mcp

`den-services/mcp` is the successor MCP facade for the legacy `den-mcp`
surface. It exposes the same static tool discovery shape while proxying tool
execution to the configured Den backend.

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
ok: local tools/list returned 61 tools
ok: local read tool proxied through backend
ok: local non-representative tool proxied through backend
ok: local get_agent_guidance returned MCP-compatible successor shape
ok: local list_agent_guidance_entries returned MCP-compatible array shape
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
facade. The harness starts `den-services/mcp` locally, uses
`DEN_MCP_SMOKE_DEN_CORE_URL` for remaining Core-routed tools, and uses the
successor URLs for the REST-routed smoke calls:

Live smoke requires those successor services to already be deployed and
reachable from the machine running the harness. This branch's local smoke proves
the MCP route table, outage behavior, and guidance compatibility shape; live
Core-dependency removal still needs a deployed successor-service smoke with the
URLs below. In the task plan, that live deployment/cutover proof is tracked by
#3890.

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://192.168.1.10:5199 \
DEN_MCP_SMOKE_TASKS_URL=http://192.168.1.10:8092 \
DEN_MCP_SMOKE_DOCUMENTS_URL=http://192.168.1.10:8094 \
DEN_MCP_SMOKE_GUIDANCE_URL=http://192.168.1.10:8097 \
DEN_MCP_SMOKE_READ_TASK_ID=3446 \
python3 mcp/scripts/hermes_smoke.py --mode both
```

Or use the live-only Make target:

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://192.168.1.10:5199 \
DEN_MCP_SMOKE_TASKS_URL=http://192.168.1.10:8092 \
DEN_MCP_SMOKE_DOCUMENTS_URL=http://192.168.1.10:8094 \
DEN_MCP_SMOKE_GUIDANCE_URL=http://192.168.1.10:8097 \
DEN_MCP_SMOKE_READ_TASK_ID=3446 \
make mcp-smoke-live
```

Expected live output includes:

```text
ok: live initialize
ok: live tools/list returned 61 tools
ok: live read tool proxied to tasks successor
ok: live non-representative tool proxied to documents successor
ok: live get_agent_guidance returned MCP-compatible successor shape
ok: live list_agent_guidance_entries returned MCP-compatible array shape
```

Live write smoke is disabled unless a pre-existing disposable document target
is provided. The current disposable fixture is `den-services/mcp-smoke-disposable`.
The harness reads the document first, writes smoke content through
`store_document`, verifies the write through `get_document`, and restores the
original document before exiting:

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://192.168.1.10:5199 \
DEN_MCP_SMOKE_TASKS_URL=http://192.168.1.10:8092 \
DEN_MCP_SMOKE_DOCUMENTS_URL=http://192.168.1.10:8094 \
DEN_MCP_SMOKE_GUIDANCE_URL=http://192.168.1.10:8097 \
DEN_MCP_SMOKE_WRITE_PROJECT=den-services \
DEN_MCP_SMOKE_WRITE_SLUG=mcp-smoke-disposable \
python3 mcp/scripts/hermes_smoke.py --mode both
```

The live mode passes backend service tokens through to the MCP process when
their normal service-token variables are set, such as `DEN_CORE_SERVICE_TOKEN`,
`DEN_TASKS_SERVICE_TOKEN`, `DEN_DOCUMENTS_SERVICE_TOKEN`, and
`DEN_GUIDANCE_SERVICE_TOKEN`. Token values are never printed by the harness.
