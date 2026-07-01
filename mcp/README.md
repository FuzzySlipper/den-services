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

To add an opt-in smoke against the current den-core backend, pass `--mode both`
and set the live backend URL explicitly. The current reachable MCP-compatible
Den endpoint is the den-srv adapter on `192.168.1.10:5199`; the harness starts
`den-services/mcp` locally and uses that URL as its backend:

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://192.168.1.10:5199 \
DEN_MCP_SMOKE_READ_TASK_ID=3446 \
python3 mcp/scripts/hermes_smoke.py --mode both
```

Or use the live-only Make target:

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://192.168.1.10:5199 \
DEN_MCP_SMOKE_READ_TASK_ID=3446 \
make mcp-smoke-live
```

Live write smoke is disabled unless a pre-existing disposable document target
is provided. The current disposable fixture is `den-services/mcp-smoke-disposable`.
The harness reads the document first, writes smoke content through
`store_document`, verifies the write through `get_document`, and restores the
original document before exiting:

```sh
DEN_MCP_SMOKE_DEN_CORE_URL=http://192.168.1.10:5199 \
DEN_MCP_SMOKE_WRITE_PROJECT=den-services \
DEN_MCP_SMOKE_WRITE_SLUG=mcp-smoke-disposable \
python3 mcp/scripts/hermes_smoke.py --mode both
```

The live mode passes `DEN_CORE_SERVICE_TOKEN` through to the MCP process when
that environment variable is set. The token value is never printed by the
harness.
