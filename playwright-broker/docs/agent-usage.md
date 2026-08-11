# Persistent Playtest Agent Usage

Use `den-playwright playtest` when testing benefits from a real page that survives across multiple model turns. The surface is designed for trusted local experimentation and broad debugging access, including application/browser internals and caller-defined mutations.

## Lifecycle

```text
start -> observe / act / inspect (repeat in any order) -> finish or cancel
```

The order is a helpful convention, not an enforcement mechanism. Every request is copied verbatim to `requests.jsonl`. Missing/stale sequence values, owner-label mismatches, unexpected order, inactive focus/pointer lock, unknown fields, and action errors become structured discrepancies. The driver continues with later actions and calls whenever the browser remains usable.

## Start

```bash
den-playwright playtest start <project-id> \
  -config /path/to/config.yaml \
  -repo /path/to/repo \
  -owner optional-correlation-label \
  -scenario scenario-name \
  -den-project den-project-id \
  -den-task 6783 \
  -metadata '{"caller_note":"anything useful"}'
```

Optional `-headed`, `-video`, and `-viewport WIDTHxHEIGHT` flags override manifest defaults. Invalid optional metadata/viewport text is retained as a discrepancy where possible.

The start result reports the session, base URL, driver/dev-server PIDs, artifact root, evidence index, and state file. The browser and page stay alive after the CLI exits.

## Act

One `act` request may contain any number of actions. Each action produces its own success/error result and a failure does not prevent later actions from running.

Typed action names:

- `keyboard_press`, `keyboard_down`, `keyboard_up`
- `mouse_move`, `mouse_click`, `mouse_down`, `mouse_up`, `mouse_wheel`
- `wait`, `viewport`, `goto`
- `click`, `dblclick`, `fill`, `type`, `focus`, `hover`, `select_option`, `set_input_files`
- `dispatch`, `evaluate`, `evaluate_handle`, `add_script`, `add_style`
- `request`, `cdp`, `screenshot`

Example:

```json
{
  "owner": "vision-worker-1",
  "sequence": 12,
  "any_new_field": { "retained": true },
  "actions": [
    { "type": "click", "selector": "canvas" },
    { "type": "keyboard_press", "key": "KeyW" },
    {
      "type": "evaluate",
      "expression": "() => { window.game.debug = true; return window.game.snapshot(); }"
    },
    {
      "type": "dispatch",
      "selector": "canvas",
      "event": "test-input",
      "eventInit": { "detail": { "strength": 1 } }
    },
    {
      "type": "cdp",
      "method": "Runtime.evaluate",
      "params": { "expression": "window.renderer", "returnByValue": true }
    }
  ]
}
```

An unknown action with an `expression` field is interpreted as JavaScript evaluation. An unknown action without one is logged as `action_error`; subsequent actions continue.

## Observe

Observation can combine any of these in one call:

```json
{
  "screenshot": true,
  "label": "after-move",
  "dom": true,
  "text": true,
  "accessibility": true,
  "storage": true,
  "expressions": [
    "() => window.gameState",
    "() => window.store.getState()"
  ],
  "frameBurst": { "count": 5, "intervalMs": 40 }
}
```

The response also contains URL/title, viewport, focus, active element, pointer-lock element, scroll position, visibility, and event counts.

## Inspect

`inspect` is the open-ended readback path:

```json
{
  "expression": "() => ({ store: window.store, renderer: window.renderer })",
  "selector": "#app canvas",
  "dom": true,
  "events": true,
  "cdp": {
    "method": "Performance.getMetrics",
    "params": {}
  }
}
```

Captured event data includes console messages, page errors/crashes, request headers and post bodies, response headers and bodies, and WebSocket frames.

## Finish and cancel

```json
{
  "outcome": "pass",
  "annotation": "free-form summary",
  "assertions": [
    { "name": "movement persisted", "pass": true, "artifact": "timeline offset 4" }
  ]
}
```

Finish/cancel stops tracing, closes the browser, terminates the local driver, attempts precise dev-server cleanup, releases the lease record, and updates cleanup fields. Repeating cleanup is harmless. Cleanup errors are evidence rather than reasons to discard the packet.

## Evidence packet

Each session writes:

```text
<artifact-root>/<project>/<session-id>/
  playtest-index.json
  requests.jsonl
  timeline.jsonl
  driver.stdout.log
  driver.stderr.log
  server.stdout.log
  server.stderr.log
  screenshots/*.png
  video/*                 # when requested
  trace.zip
```

`playtest-index.json` includes:

- repo path, exact Git SHA/origin when available, and dirty-state details;
- Den/scenario/caller metadata;
- browser/version/viewport and timestamps;
- original request and interpreted timeline offsets;
- discrepancies and per-action partial results;
- DOM/accessibility/application/storage observations;
- console/page/network/WebSocket capture;
- screenshots, frame bursts, video, trace, and cleanup results.

This is testing/review evidence. Model judgement and caller-supplied outcomes remain labelled rather than becoming automatic merge decisions.

## MCP stdio

Build the CLI, then configure an MCP client to launch:

```text
/absolute/path/to/den-playwright mcp -config /absolute/path/to/config.yaml
```

The server exposes the eight `playtest_*` tools described above. All tool input schemas accept additional properties. The MCP result includes both text and structured content.

## Den completion note

Compact evidence template:

```text
Playtest: <scenario> at exact SHA <sha> (<clean|dirty>)
Outcome: <pass|fail|uncertain|infrastructure_error|caller-defined>
Discrepancies: <count and relevant codes>
Evidence: <absolute playtest-index.json path>
Key offsets/artifacts: <timeline offsets, screenshots, trace>
Judgement: <what was model judgement vs deterministic assertion>
```

Persistent model-driven sessions are on-demand completion/review/nightly/release evidence. Ordinary CI should keep to deterministic lifecycle/schema/cleanup tests.
