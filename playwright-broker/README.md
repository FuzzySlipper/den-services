# Playwright Broker

`playwright-broker` provides two local browser-testing modes:

- `den-playwright run` owns a dev server for one conventional Playwright test run.
- `den-playwright playtest` keeps a browser page alive across repeated agent calls and writes a durable evidence packet.

The persistent playtest mode is a trusted local testing/debugging tool. It is deliberately permissive: callers can use selectors, DOM and accessibility inspection, JavaScript evaluation, script/event injection, application globals and test hooks, request/response/WebSocket data, browser storage, and raw CDP. Owner labels, sequence numbers, focus, pointer lock, and lifecycle expectations are diagnostic context. Mismatches are recorded and the driver continues whenever it can still do useful work.

The broker lives in `den-services` for shared conventions but is not a production Den service and is not listed in `deployment/services.yaml`.

## Persistent quick start

Build the CLI and use the included fixture:

```bash
go build -o /tmp/den-playwright ./playwright-broker/cmd/den-playwright

/tmp/den-playwright playtest start permissive-playtest-fixture \
  -config /home/dev/den-services/playwright-broker/config/config.example.yaml \
  -repo /home/dev/den-services/playwright-broker/examples/permissive-playtest \
  -scenario pointer-lock-smoke
```

The result contains a `session_id`. Reuse it across calls:

```bash
/tmp/den-playwright playtest act "$session_id" -config "$config" -request '{
  "sequence": 40,
  "actions": [
    {"type":"click","selector":"#world"},
    {"type":"keyboard_press","key":"w"},
    {"type":"evaluate","expression":"() => window.testHooks.snapshot()"},
    {"type":"cdp","method":"Runtime.evaluate","params":{"expression":"window.gameState","returnByValue":true}}
  ]
}'

/tmp/den-playwright playtest observe "$session_id" -config "$config" -request '{
  "dom":true,
  "accessibility":true,
  "storage":true,
  "frameBurst":{"count":3,"intervalMs":50},
  "expressions":["() => window.gameState"]
}'

/tmp/den-playwright playtest finish "$session_id" -config "$config" -request '{
  "outcome":"pass",
  "annotation":"pointer lock, injected state, and network payload checked"
}'
```

Sequence and owner mismatches in this example do not stop execution. They appear in `discrepancies` in the response and `playtest-index.json`.

## Repo manifest

Extend the existing `.den-playwright.json` convention with an optional `playtest` block:

```json
{
  "project": "my-app",
  "serve": {
    "command": "pnpm run dev -- --host {host} --port {port}",
    "healthUrl": "/",
    "readyText": "My App"
  },
  "playtest": {
    "startPath": "/game",
    "viewport": { "width": 1280, "height": 720 },
    "headed": false,
    "recordVideo": true
  },
  "tests": {
    "command": "pnpm exec playwright test",
    "artifactPolicy": "live-ui"
  }
}
```

The broker chooses separate ports and artifact roots for concurrent sessions. Dev-server identity/reuse metadata is used to keep runs intelligible and cleanup precise, not to restrict browser inspection.

## Agent-facing surfaces

The JSON CLI accepts `-request '{...}'`, `-request @file.json`, or `-request -` for stdin. It exposes:

- `playtest start`
- `playtest observe`
- `playtest act`
- `playtest inspect`
- `playtest finish`
- `playtest cancel`
- `playtest get`
- `playtest list`

`den-playwright mcp -config <path>` exposes the same lifecycle as an MCP stdio server with `playtest_start`, `playtest_observe`, `playtest_act`, `playtest_inspect`, `playtest_finish`, `playtest_cancel`, `playtest_get`, and `playtest_list`. Tool schemas keep `additionalProperties: true`, so experimental caller fields are retained instead of rejected.

See [agent usage](docs/agent-usage.md) for the operation catalog and evidence
packet. For the repository-owned Luna/max custom agent, installer, parent spawn
prompt, and troubleshooting, see [Codex Luna playtester](docs/codex-playtester.md).
For the canonical repository adoption packet, judgement boundary, completion
checklist, and Den evidence block, see the
[product playtest green path](docs/product-playtest-green-path.md).

## One-shot Playwright runs

The existing path remains available:

```bash
den-playwright run my-app \
  -config playwright-broker/config/config.example.yaml \
  -repo /path/to/repo \
  -- --reporter=list
```

It starts/reuses the configured dev server, sets `BASE_URL`, runs the repo's Playwright command, and writes `run-index.json` under the configured artifact root.
