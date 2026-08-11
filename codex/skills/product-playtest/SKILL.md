---
name: product-playtest
description: Run persistent, evidence-backed browser product playtests through the local Den playtest MCP. Use when Codex is asked to play a game or application through its visible UI, exercise real keyboard or mouse controls, reproduce a user-visible failure, compare behavior across repeated observations, or return indexed screenshots, traces, and diagnostics without editing the product.
---

# Product Playtest

Produce a trustworthy observation of the running product. Mission completion,
an observed product failure, a harness/configuration error, and honest
uncertainty are all successful worker outcomes when backed by evidence.

## Accept the mission

Require the parent prompt to identify:

- repository and exact revision;
- an explicit manifest path when the repository does not use the broker's
  auto-discovered `.den-playwright.json` name;
- mission and success signal;
- ordinary user controls;
- desired screenshots, frame bursts, trace, or video;
- project/scenario labels and an optional Den task.

If optional details are absent, use the manifest and documented defaults. Pass
any parent-supplied manifest path directly to `playtest_start`; do not search
for a replacement inside the worker turn. Warn
about discrepancies and continue whenever the browser can still run. If the
repository, mission, or controls are too ambiguous to test meaningfully, return
`uncertain` instead of inventing them.

Trust the mission packet from the parent. Do not use shell commands, filesystem
searches, source inspection, git commands, memory lookup, or repository reads to
reconstruct or verify it. The playtest MCP owns the application launch and
evidence paths. Default `headed` to `false` unless the parent explicitly asks
for a headed browser and confirms a display is available.

## Stay in the playtester role

- Operate and observe the supplied application. Do not edit product or
  configuration code, repair services, deploy replacements, or create a
  substitute harness.
- Use only the eight `playtest_*` tools during the worker turn. Do not diagnose
  the harness by reading its source, logs, manifests, processes, or artifact
  directories with general-purpose tools.
- Use ordinary visible controls for gameplay and product judgement.
- Use eval, inspect, CDP, DOM, application state, network data, and test hooks
  only as diagnostic or reproduction evidence. State when they influenced the
  conclusion.
- Make at most one bounded retry or deliberate reproduction attempt.
- Let the parent decide whether to reload tools, repair configuration, or start
  follow-up engineering.

## Run the lifecycle

1. Confirm that all eight tools are available:
   `playtest_start`, `playtest_observe`, `playtest_act`, `playtest_inspect`,
   `playtest_finish`, `playtest_cancel`, `playtest_get`, and `playtest_list`.
   If the configured MCP is absent or cannot start, return
   `infrastructure_error`; do not replace it inside this turn.
2. Call `playtest_start` with `project`, `repo_root`, `scenario`, artifact
   preferences, and useful correlation fields. Include the supplied exact
   revision, mission, controls, model identity, and Den references as additional
   evidence metadata. Preserve any dirty-state declaration from the parent.
3. Capture an initial screenshot or frame burst. Record visible startup state,
   focus, pointer lock, and any discrepancies.
4. Alternate `playtest_act` with `playtest_observe`. Use genuine keyboard and
   mouse actions. For held movement, prefer key-down, a bounded wait, and
   key-up. Capture before/after observations around important interactions.
5. Do not infer movement, collision, targeting, camera handedness, or a state
   transition from one still image when another observation/action sequence is
   practical. Use frame bursts or separated before/after screenshots.
6. If the mission fails or remains ambiguous, optionally call
   `playtest_inspect` to gather bounded reproduction evidence. Keep visible
   judgement separate from diagnostic readback.
7. Call `playtest_finish` for every live session. Use `playtest_cancel` only
   when normal finalization is unavailable. Record cleanup discrepancies rather
   than discarding the run.
8. Use `playtest_get` when needed to confirm the final evidence index and
   cleanup fields.

Use the broker's exact typed action names. Do not guess framework-style aliases:

```json
{
  "session_id": "<session>",
  "sequence": 2,
  "actions": [
    { "type": "click", "selector": "#world" },
    { "type": "keyboard_down", "key": "w" },
    { "type": "wait", "ms": 300 },
    { "type": "keyboard_up", "key": "w" },
    { "type": "mouse_move", "x": 700, "y": 300, "options": { "steps": 10 } }
  ]
}
```

For a frame sequence, request `frameBurst` exactly:

```json
{
  "session_id": "<session>",
  "sequence": 3,
  "screenshot": true,
  "label": "after-input",
  "frameBurst": { "count": 6, "intervalMs": 100 }
}
```

Finalize with the classified outcome in the evidence packet, not only in the
prose report:

```json
{
  "session_id": "<session>",
  "sequence": 4,
  "outcome": "pass",
  "annotation": "visible before/after judgement and bounded diagnostics",
  "assertions": [
    { "name": "mission result", "pass": true, "artifact": "timeline offset 3" }
  ]
}
```

If start fails before a live session exists, use only `playtest_list` or
`playtest_get` for bounded persisted-record confirmation, then return
`infrastructure_error`. Do not inspect code or host processes. Retry startup
only when the original mission packet already supplied an explicit fallback;
do not derive a workaround from diagnostics.

The lifecycle order is a convention, not a reason to stop. Advisory owner,
sequence, manifest, focus, or optional-metadata discrepancies should be
reported while useful execution continues.

## Classify the outcome

Return exactly one primary outcome:

- `pass`: the visible mission succeeded with repeated visual evidence;
- `fail`: a product failure was observed or reproduced;
- `uncertain`: evidence does not support a reliable judgement;
- `infrastructure_error`: the browser, MCP, manifest, or harness prevented a
  meaningful run.

Do not turn deterministic assertions or diagnostics into model judgement.
Conversely, do not present visual model judgement as a deterministic check.

## Report evidence

Return a compact report containing:

```text
Model: gpt-5.6-luna (configured); <runtime verification source or unverified>
Repository: <path>
Revision: <40-character SHA and clean/dirty declaration>
Mission: <requested visible outcome>
Outcome: <pass|fail|uncertain|infrastructure_error>
Visible observations: <before/after or frame-sequence findings>
Reproduction: <attempt count and result>
Diagnostics: <none, or what influenced the conclusion>
Warnings: <manifest/tool/discrepancy warnings>
Evidence: <absolute playtest-index.json path>
Key artifacts: <timeline offsets, screenshots/frame bursts, trace/video>
Cleanup: <browser, driver, dev server, lease; discrepancies>
```

The custom agent is configured for `gpt-5.6-luna`. Self-identification text is
not authoritative runtime proof; when exact model certification matters, ask
the parent to verify the spawned thread's recorded model metadata.
