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
- a neutral player goal; do not require a desired verdict or a statement that
  the expected fix already succeeded;
- ordinary user controls;
- desired screenshots, frame bursts, trace, or video;
- project/scenario labels and an optional Den task;
- an optional complete current field-guide snapshot and optional source handles.

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

## Use guidance without surrendering observation

Keep two knowledge layers distinct:

- Generic gameplay concepts are reviewed Den Knowledge entries. Retrieve a
  supplied handle on demand with the optional `den_reference` tools; do not
  preload a large corpus and do not send retrieval through the Playwright
  broker.
- A game/scenario field guide is a volatile, complete Markdown-friendly
  snapshot supplied by the parent. It may contain controls, landmarks, spatial
  relationships, strategies, confusing affordances, failed approaches, and
  open questions. It is not Den Knowledge and is not an acceptance rubric.

If practical, capture the initial neutral scene before reading game-specific
guidance. Treat all notes as fallible hints. When current visible evidence
contradicts a note, retain both the contradiction and what was visibly
observed; the observation wins.

At the end of a run, optionally emit one complete replacement candidate. The
parent owns publication to a latest-value Den document, authored repository
document, or other explicit owner. `replacement_mode` must be
`replace-complete`: rewrite everything still useful and omit displaced claims.
Never append or patch the old guide, never publish it from the worker, and
never concatenate historical revisions into a later worker's context. The
evidence index retains the exact input snapshot, handles, usage, and proposed
replacement for audit.

## Observe before judging

The first account is evidence, not a verdict. Before applying acceptance
criteria or answering a targeted visual question:

- describe the visible scene in concrete spatial terms;
- identify conspicuous or unexpected details, including details unrelated to
  the apparent desired result;
- distinguish what is visible now from what changed after an action;
- preserve ambiguity instead of completing the parent's implied story.

For primarily visual classification, the parent/orchestrator normally owns the
acceptance mapping. It may keep its criteria private until the neutral account
exists. The worker still owns ordinary operational outcomes: whether it
completed the player goal, encountered a visible failure, remained uncertain,
or could not run the harness.

A targeted follow-up is appropriate after the neutral account when it asks for
missing evidence, disambiguates a concrete spatial or temporal detail, or
continues the intended player affordance. Do not use a follow-up merely to ask
the worker to agree with a desired verdict.

## Stay in the playtester role

- Operate and observe the supplied application. Do not edit product or
  configuration code, repair services, deploy replacements, or create a
  substitute harness.
- Use only the eight `playtest_*` tools plus the optional read-only
  `den_reference` tools (`den_knowledge_get`, `den_knowledge_guide`,
  `den_knowledge_search`, and `get_document`) during the worker turn. Do not diagnose
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

1. Confirm that all eight playtest tools are available:
   `playtest_start`, `playtest_observe`, `playtest_act`, `playtest_inspect`,
   `playtest_finish`, `playtest_cancel`, `playtest_get`, and `playtest_list`.
   If the configured MCP is absent or cannot start, return
   `infrastructure_error`; do not replace it inside this turn.
2. Call `playtest_start` with `project`, `repo_root`, `scenario`, artifact
   preferences, and useful correlation fields. Include the supplied exact
   revision, mission, controls, model identity, and Den references as additional
   evidence metadata. Preserve any dirty-state declaration from the parent.
   Include `field_guide` and `source_handles` exactly as received so the
   evidence index records the run's guidance input.
3. Capture an initial screenshot or frame burst. Before mapping it to acceptance,
   record a neutral visible account of startup state, spatial relationships,
   focus, pointer lock, conspicuous details, and discrepancies.
4. Alternate `playtest_act` with `playtest_observe`. Use genuine keyboard and
   mouse actions. For held movement, prefer key-down, a bounded wait, and
   key-up. Capture before/after observations around important interactions.
   When testing an affordance, continue beyond its first local reaction:
   activate it, observe the effect, attempt the intended downstream use, and
   verify the resulting player state.
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

When the parent explicitly requests a verbose action trace, set
`verbose_trace: true` on start. For each cycle, put a concise `trace` object on
the act with `cycle_id`, `observe`, `hypothesis`, `intent`, and
`expected_effect`; put the same `cycle_id` plus `observed_effect`,
`matched_expectation`, `confidence`, and `plan_update` on the following
observe. These are short user-facing decision summaries, not private
chain-of-thought. They link to the existing action and screenshot/frame burst,
so do not take extra screenshots solely for tracing. Leave tracing off for
normal playtests.

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

Use `mouse_click` with numeric `x`/`y` for a viewport-coordinate click such as
canvas capture. `click` with a `selector` is a locator action. The broker also
accepts `click` with numeric coordinates as a recovery shorthand, but do not
combine selector and coordinate forms.

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
  "annotation": "operational mission result and bounded diagnostics",
  "neutral_observation": {
    "initial": "concrete spatial account before acceptance mapping",
    "trajectory": ["visible change after an important action"],
    "unexpected": ["conspicuous detail or contradiction"]
  },
  "operational_outcome": {
    "status": "completed",
    "summary": "furthest player-level state reached"
  },
  "acceptance_mapping": {
    "owner": "orchestrator",
    "status": "pending"
  },
  "field_guide_usage": {
    "handles_read": ["den-knowledge:gameplay-interaction-completion"],
    "useful_claims": ["verify the downstream player state after activation"],
    "contradictions": ["a supplied traversal claim conflicted with visible collision"]
  },
  "field_guide_replacement": {
    "schema_version": 1,
    "guide_id": "project/scenario",
    "revision": "next-candidate",
    "replaces_revision": "7",
    "replaces_sha256": "<exact input bundle hash>",
    "replacement_mode": "replace-complete",
    "provenance": ["playtest session <session>"],
    "observed_build_revision": "<40-character product SHA>",
    "freshness": "observed this run",
    "confidence": "medium",
    "notes_markdown": "Complete next-run controls, landmarks, strategies, and caveats.",
    "unresolved_questions": ["question retained for a future run"]
  },
  "assertions": [
    { "name": "mission result", "pass": true, "artifact": "timeline offset 3" }
  ],
  "exit_interview": {
    "difficulties": ["free-form operating difficulty"],
    "failed_approaches": ["failed probe and any workaround"],
    "confidence": "high, medium, low, or free-form",
    "suggestions": ["game, controls, mission, or harness improvement"]
  }
}
```

The exit interview is optional. Include it when the parent asks for tester
feedback or when a concrete control, prompt, navigation, or harness difficulty
would help the next run. Partial and uncertain feedback is useful; do not
invent suggestions to fill fields.

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

These are operational mission outcomes. For a visual-description mission,
`pass` can mean the requested neutral account was captured; it does not mean a
withheld product criterion passed. Preserve the separate acceptance owner and
status in `acceptance_mapping`.

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
Neutral observation: <initial concrete account, trajectory, unexpected details>
Operational result: <furthest player state reached or blocker>
Acceptance mapping: <orchestrator owner/status, or why worker judgement applies>
Guidance: <input revision/hash, handles read, contradictions, replacement candidate or none>
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
