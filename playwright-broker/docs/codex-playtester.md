# Codex Luna playtester

The Codex playtester is an on-demand black-box product-testing lane over the
broker's eight local `playtest_*` MCP tools. It complements deterministic tests
and Rusty Crew playtesting; it does not require Rusty Crew or a deployed Den
service.

## Install and check

From the `den-services` repository root:

```bash
./scripts/install-codex-playtester.sh
./scripts/install-codex-playtester.sh --check
```

The command:

- builds `den-playwright` into the active Codex home;
- links the repository-owned `product-playtest` skill;
- renders the `playtester` custom agent with `gpt-5.6-luna` and `max` reasoning;
- renders a host-local broker configuration and artifact root;
- initializes the stdio MCP and verifies all eight tools.

Set `CODEX_HOME` or pass `--codex-home PATH` to target another Codex
configuration. The installer refuses to replace an unrelated skill directory.

Start a **fresh Codex task** after installation. Agent, skill, and MCP catalogs
are loaded when a task starts; an existing task does not gain them dynamically.

## Parent green path

Use one prompt with concrete mission inputs:

```text
Spawn the custom `playtester` agent for an evidence-backed product playtest.

Repository: /absolute/path/to/repository
Manifest: <optional absolute path when not named .den-playwright.json>
Exact revision: <40-character SHA> (<clean or dirty, with dirty paths if any>)
Mission: <visible user outcome to attempt>
Controls: <ordinary keyboard/mouse controls and any startup action>
Artifacts: <screenshots/frame bursts/trace/video preferences>
Project/scenario: <project id> / <scenario label>
Den reference: <optional project and task>

Keep one browser session across repeated observe/act turns. Judge visible behavior from repeated screenshots or frame bursts, use diagnostics only when useful and label their influence, make at most one bounded reproduction attempt, always clean up, and return the product-playtest evidence report.
```

The playtester defaults to headless Chromium because screenshots and frame
bursts are its visible evidence surface. Request headed execution only when the
host has a display. The worker trusts the mission packet and uses only the
eight `playtest_*` tools; it does not inspect repository or harness source.

The broker auto-discovers `.den-playwright.json`. Existing products that expose
the compatible serve contract under another filename, such as
`.den-serve.json`, must include that absolute path in the parent mission so the
worker can pass it as `manifest_path` without searching the repository.

The parent should accept `pass`, `fail`, `uncertain`, and
`infrastructure_error` as valid evidence-bearing worker outcomes. A surfaced
failure is not an invitation for the playtester to edit code or repair the
harness; follow-up engineering belongs to the parent.

## Model identity

The installed agent pins:

```toml
model = "gpt-5.6-luna"
model_reasoning_effort = "max"
```

The worker reports this configured identity. For acceptance or cost evidence,
verify the spawned agent thread's recorded runtime model metadata as well.
Generic prose such as “GPT-5 (Codex)” is not authoritative model proof.

## Evidence and judgement

Use ordinary visible controls and before/after observations for product
judgement. A lone still image does not prove movement, collision, targeting,
camera handedness, or a state transition when another observation is practical.

Eval, DOM, application state, CDP, network data, and test hooks remain useful
for reproduction and diagnosis. The report must say when those diagnostics
influenced its conclusion and must retain the absolute `playtest-index.json`
path, useful timeline/artifact offsets, warnings, and cleanup state.

Live model-driven playtests are completion, review, nightly, or release
evidence. They remain outside ordinary every-commit CI. The installer and MCP
catalog checks are deterministic and suitable for CI or local preflight.

## Troubleshooting

- **No `playtester` agent or skill:** start a fresh Codex task, then rerun the
  installer with `--check`.
- **No `playtest_*` tools:** inspect the check output and rendered paths under
  the active Codex home. Do not build a substitute harness inside a playtest.
- **Manifest or optional metadata warning:** continue with documented defaults
  if the browser can still run; include the warning in the report.
- **Browser/MCP startup failure:** return `infrastructure_error` with the
  captured error. The parent decides whether to repair configuration.
- **Manual fallback needed outside Codex:** use the JSON CLI lifecycle in
  [agent usage](agent-usage.md). Do not claim that a manual CLI run proves the
  custom-agent MCP integration.
