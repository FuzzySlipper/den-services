# Product playtest green path

Use this path for an evidence-backed judgement of a visible product outcome.
It is a completion, review, nightly, or release activity, not an ordinary
per-commit CI gate.

The default worker is the repository-owned Codex `playtester` agent running
`gpt-5.6-luna` with `max` reasoning. That default is provisional: use a second
worker or a stronger verifier when the result is consequential, surprising,
or uncertain. GoblinBench calibration is useful future work but does not block
adoption of this path.

## Adopt in a repository

Copy and tailor the two files in
[`templates/product-playtest`](../templates/product-playtest):

- add the `playtest` block from `manifest.example.json` to the product's
  `.den-playwright.json` or compatible serve manifest;
- copy `scenario.example.json` to `product-playtest.scenario.json` and replace
  its placeholder mission, ordinary controls, and evidence preferences.

If the compatible manifest is not named `.den-playwright.json`, the parent
mission must supply its absolute path. Keep the scenario as a concise mission
packet, not a second test framework or product specification.

Check the adoption packet deterministically:

```bash
node /home/dev/den-services/scripts/check-product-playtest-adoption.mjs \
  /absolute/path/to/repository \
  /absolute/path/to/manifest.json \
  /absolute/path/to/product-playtest.scenario.json
```

This check validates only the repository-facing packet. The installer and MCP
catalog have their own deterministic preflight:

```bash
/home/dev/den-services/scripts/install-codex-playtester.sh --check
```

Start a fresh Codex task after installation because agent, skill, and MCP
catalogs are loaded at task start.

## Run the playtest

Give a fresh parent task one command-sized request:

```text
Spawn the custom `playtester` agent at its configured Luna/max settings and run
the mission in /absolute/path/to/product-playtest.scenario.json against exact
revision <40-character SHA> of /absolute/path/to/repository. Use manifest
/absolute/path/to/manifest.json. Keep one browser session, judge the visible
outcome from repeated screenshots or frame bursts, label any diagnostic
influence, make at most one bounded reproduction attempt, always clean up, and
return the complete product-playtest evidence report.
```

The parent should verify the repository revision and dirty state before the
run. The worker then owns one persistent browser session through
`start -> observe / act / inspect -> finish|cancel`. It uses ordinary controls
from the mission packet and does not edit the product or harness.

Valid outcomes are `pass`, `fail`, `uncertain`, and `infrastructure_error`.
A failure is useful evidence, not permission for the worker to repair code.

## Judgement and diagnostics

Visible observations are the authority for user-visible claims. Use a before
and after screenshot, repeated screenshots, or a frame burst whenever motion,
camera direction, collision, targeting, or a state transition is part of the
claim. A single still is insufficient when another observation is practical.

DOM, accessibility, eval, application state, CDP, network traffic, browser
storage, and test hooks are available because the local broker is deliberately
permissive. They may help reproduce or explain behavior, but they do not turn
hidden state into visible product proof. The report must identify each
diagnostic that influenced its conclusion.

## Completion and review checklist

- repository path, exact 40-character revision, origin, and dirty state;
- configured worker model/effort and recorded runtime identity when available;
- concrete mission, ordinary controls, and final outcome;
- visible before/after observations and at most one bounded reproduction;
- diagnostic influence, or an explicit statement that there was none;
- absolute `playtest-index.json` path plus useful timeline offsets and
  screenshot, frame-burst, trace, or video paths;
- discrepancies, console/page errors, and relevant infrastructure warnings;
- cleanup state for browser, driver, dev server, and lease;
- why the run belongs outside ordinary CI;
- any adoption friction or repository-specific exception.

The evidence packet is durable supporting evidence. Model judgement remains
labelled and never becomes product authority, deterministic test authority, or
an automatic merge decision.

## Den completion block

```text
Product playtest: <project>/<scenario>
Repository: <absolute path>
Revision: <40-character SHA> (<clean|dirty; list dirty paths>)
Worker: <configured model/effort; recorded runtime identity if available>
Mission: <visible user outcome>
Outcome: <pass|fail|uncertain|infrastructure_error>
Visible observations: <before/after or repeated-frame judgement>
Reproduction: <none|one bounded attempt and result>
Diagnostics: <none|what influenced the conclusion>
Discrepancies/warnings: <count and relevant codes>
Evidence: <absolute playtest-index.json path>
Key offsets/artifacts: <timeline offsets, screenshots, frame bursts, trace/video>
Cleanup: <browser, driver, server, lease>
CI posture: on-demand evidence; deterministic checks only in ordinary CI
Adoption friction: <none|bounded repository-specific notes>
```

For the full manual CLI/MCP lifecycle, see [agent usage](agent-usage.md). For
installation, model identity, and parent spawning details, see
[Codex Luna playtester](codex-playtester.md).
