# Persistent Playtest Agent Usage

Use `den-playwright playtest` when testing benefits from a real page that survives across multiple model turns. The surface is designed for trusted local experimentation and broad debugging access, including application/browser internals and caller-defined mutations.

## Lifecycle

```text
start -> observe / act / inspect (repeat in any order) -> finish or cancel
```

The order is a helpful convention, not an enforcement mechanism. Every request is copied verbatim to `requests.jsonl`. Missing/stale sequence values, owner-label mismatches, unexpected order, inactive focus/pointer lock, unknown fields, and action errors become structured discrepancies. The driver continues with later actions and calls whenever the browser remains usable.

Callers may declare advisory expectations with `expected_previous_kind`,
`expected_focus`, and `expected_pointer_lock` (camel-case aliases are also
accepted). A mismatch records `unexpected_operation_order`, `focus_inactive`
or `focus_unexpected`, and `pointer_lock_inactive` or
`pointer_lock_unexpected`. These hints never reject or suppress an operation;
omit them when focus, pointer lock, or call order is irrelevant to the test.

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

`mouse_move` continues to use viewport coordinates. On Linux playtest sessions,
the broker runs hidden Chromium on a session-owned Xvfb display and sends mouse
movement and buttons through XTest. Before pointer lock, coordinates are
absolute. After pointer lock, the same coordinates are compared with the last
requested position and emitted as real relative movement. This avoids the large
synthetic recenter events produced by Chromium's DevTools mouse path and lets
FPS-style games receive ordinary trusted `movementX`/`movementY` input. Agents
should keep a simple virtual cursor position, for example center `(640, 360)`,
then request `(590, 360)` for 50 pixels left and `(640, 360)` for 50 pixels
right. Pointer-locked clicks use genuine XTest button events and do not move the
virtual cursor.

The configured `playtest.input_helper` must be the repository's
`playtest-x11-input` binary. The Codex installer builds and validates it. Manual
Linux installations also require `Xvfb`, X11, and XTest runtime libraries.

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

## Optional decision trace

Set `"verbose_trace": true` on `playtest_start` only when a playtest is hard to
understand or a human expects to debug the tester's control strategy. Normal
sessions do not create a decision-trace artifact and need no extra fields.
Verbose tracing stores concise, explicitly supplied user-facing summaries—not
private chain-of-thought—in `decision-trace.jsonl` and the index.

Use one stable `cycle_id` for an act and its following observation:

```json
{
  "sequence": 12,
  "actions": [{ "type": "keyboard_down", "key": "w" }, { "type": "wait", "ms": 500 }, { "type": "keyboard_up", "key": "w" }],
  "trace": {
    "cycle_id": "approach-gate",
    "observe": "A closed gate is centered and appears several steps away.",
    "hypothesis": "W moves toward the current view direction.",
    "intent": "Walk forward briefly to approach the gate.",
    "expected_effect": "The gate should grow larger while remaining centered."
  }
}
```

Then attach the visible verification to the ordinary observation:

```json
{
  "sequence": 13,
  "screenshot": true,
  "frameBurst": { "count": 5, "intervalMs": 100 },
  "trace": {
    "cycle_id": "approach-gate",
    "observed_effect": "The gate grew larger and remained centered.",
    "matched_expectation": true,
    "confidence": "high",
    "plan_update": "Probe the documented interaction key next."
  }
}
```

Trace entries retain request sequence, operation phase, timeline offset, the
actual action/result or observation artifact paths, and the supplied summary.
No extra screenshot is taken for tracing: it links whatever the normal observe
call already captured. Five or more cycles can be useful when diagnosing why a
tester repeatedly got lost; routine smoke playtests should leave the mode off.

## Finish and cancel

```json
{
  "outcome": "pass",
  "annotation": "free-form summary",
  "assertions": [
    { "name": "movement persisted", "pass": true, "artifact": "timeline offset 4" }
  ],
  "exit_interview": {
    "difficulties": ["The interaction key was not visible in the game."],
    "confusing_controls": ["Mouse capture was clear; the gate action was not."],
    "failed_approaches": ["Tried walking into the gate before finding E in the mission."],
    "blockers": [],
    "confidence": "medium",
    "suggestions": ["Show the interaction key when the gate is targeted."]
  }
}
```

`exit_interview` is an optional free-form suggestion box. It is retained in the
index, returned by finish, and copied into the persisted session returned by
`playtest_get`. Missing, partial, fuzzy, or low-confidence feedback is valid and
never changes outcome or cleanup. The camel-case `exitInterview` and
`tester_feedback` aliases are also accepted for existing experimental callers.

Finish/cancel stops tracing, closes the browser, terminates the local driver, attempts precise dev-server cleanup, releases the lease record, and updates cleanup fields. Repeating cleanup is harmless. Cleanup errors are evidence rather than reasons to discard the packet.

If the driver cannot receive a request, the manager appends the exact request,
`driver_call_error`, and a chronological fallback event directly to the same
packet before returning. Host cleanup also writes `host-cleanup.jsonl` before
patching the index; lease, session, process, sidecar, and index failures receive
distinct diagnostic codes. The sidecar remains a recovery record if the main
index itself cannot be rewritten.

## Evidence packet

Each session writes:

```text
<artifact-root>/<project>/<session-id>/
  playtest-index.json
  requests.jsonl
  timeline.jsonl
  decision-trace.jsonl   # only when verbose_trace=true and trace summaries exist
  driver.stdout.log
  driver.stderr.log
  server.stdout.log
  server.stderr.log
  host-cleanup.jsonl
  screenshots/*.png
  video/*                 # when requested
  trace.zip
```

`playtest-index.json` includes:

- repo path, exact Git SHA/origin when available, and dirty-state details;
- Den/scenario/caller metadata;
- browser/version/viewport and timestamps;
- original request and interpreted timeline offsets;
- optional sequence-aligned decision summaries and exit interview;
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

The server exposes the eight `playtest_*` tools described above. All tool input schemas accept additional properties. Every MCP result includes text and structured content. A successful `playtest_observe` additionally returns the screenshot and current frame-burst files as MCP `image` content blocks, in capture order, so vision clients receive the same pixels retained by the evidence packet. Image attachment warnings are appended as text without discarding the structured observation.

For a repository-owned Codex skill and `gpt-5.6-luna` custom playtester that
uses this stdio surface, see [Codex Luna playtester](codex-playtester.md).

## Den completion note

Use the full repository-facing checklist and completion block in
[product playtest green path](product-playtest-green-path.md) for task or review
evidence. The compact form below remains useful for manual diagnostics.

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
