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
- copy the version 2 `scenario.example.json` to
  `product-playtest.scenario.json` and replace its neutral player mission,
  ordinary controls, observation protocol, orchestrator acceptance mapping,
  optional current field guide/source handles, and evidence preferences. Version 1 packets remain readable but the checker
  reports that they lack the observation-first split.

If the compatible manifest is not named `.den-playwright.json`, the parent
mission must supply its absolute path. Keep the scenario as a concise mission
packet, not a second test framework or product specification.

Both packet files must live inside the repository they claim to adopt, after
resolving symlinks. The deterministic checker rejects cross-repository packet
attribution so evidence cannot accidentally name one checkout while reading
another checkout's manifest or mission.

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
the neutral player mission in /absolute/path/to/product-playtest.scenario.json against the
current checkout in /absolute/path/to/repository. Use manifest
/absolute/path/to/manifest.json. Keep one browser session, judge the visible
outcome from repeated screenshots or frame bursts, and capture the worker's
neutral account before revealing or applying the orchestrator acceptance
mapping. Supply the current field-guide snapshot and source handles without
implying an acceptance verdict. Label any diagnostic influence, make at most one bounded reproduction
attempt, always clean up, and return the complete product-playtest evidence
report.
```

The parent should provide the repository path. The worker records the run
timestamp and then owns one persistent browser session through
`start -> observe / act / inspect -> finish|cancel`. It uses ordinary controls
from the mission packet and does not edit the product or harness.

Valid operational outcomes are `pass`, `fail`, `uncertain`, and
`infrastructure_error`. For a visual-description mission, `pass` may mean the
requested observation was completed; it does not assert that a withheld
product criterion passed. A failure is useful evidence, not permission for the
worker to repair code.

## Guidance and field-guide ownership

Reviewed cross-game concepts belong in Den Knowledge. The installed agent can
optionally discover the existing root Den MCP and exposes only four read-only
reference tools under `den_reference`: Knowledge search/guide/get and Den
document get. Retrieval is on demand and goes directly to Den; the Playwright
broker remains an eight-tool browser boundary. Public URLs or authored guides
must likewise be resolved by their owning tool, or supplied as content by the
parent when that tool is unavailable.

The initial reviewed concept-card handles are:

- `gameplay-orient-act-reobserve`;
- `gameplay-interaction-completion`;
- `gameplay-pointer-capture-recovery`.

Game/scenario notes have a different lifecycle. The parent supplies one
complete current snapshot with an observation timestamp, provenance, freshness,
confidence, Markdown notes, and unresolved questions. The worker
may propose one complete next snapshot in `field_guide_replacement`; it cannot
publish it. The parent publishes through the explicitly named latest-value
owner—normally a Den document or repository document—and replaces the previous
content wholesale. Prior snapshots remain in evidence or document history, but
are not concatenated into a future prompt.

Capture an initial neutral scene before reading game-specific hints when
practical. Field guides can help with controls and navigation, but they never
carry the expected mission verdict. Current visible evidence wins and any
contradiction belongs in `field_guide_usage.contradictions`.

## Judgement and diagnostics

Avoid leading the worker with the desired visual verdict or implying that a
fix already succeeded. Give it the player goal, controls, and any neutral game
guide it needs. Its first account should describe concrete spatial and temporal
relationships plus conspicuous or unexpected details. Preserve that account
verbatim enough that a reviewer can compare it with the retained frames.

The orchestrator normally owns acceptance interpretation for primarily visual
classification. Apply the separate `orchestratorAcceptance` mapping only after
the neutral account exists. A targeted follow-up may then gather missing
evidence, disambiguate a concrete detail, or continue the intended affordance;
it should not merely seek agreement with the desired result.

Visible observations are the authority for user-visible claims. Use a before
and after screenshot, repeated screenshots, or a frame burst whenever motion,
camera direction, collision, targeting, or a state transition is part of the
claim. A single still is insufficient when another observation is practical.
For an interaction, do not stop at the first local response. Continue through
the intended completion chain: activate, observe the local effect, attempt the
downstream use, and verify the resulting player state. A moving door does not
establish that its opening is traversable.

DOM, accessibility, eval, application state, CDP, network traffic, browser
storage, and test hooks are available because the local broker is deliberately
permissive. They may help reproduce or explain behavior, but they do not turn
hidden state into visible product proof. The report must identify each
diagnostic that influenced its conclusion.

## Completion and review checklist

- repository path and run timestamp;
- configured worker model/effort and recorded runtime identity when available;
- neutral player mission, ordinary controls, and operational outcome;
- initial neutral account, visible trajectory, unexpected details, and at most
  one bounded reproduction;
- separate acceptance owner/status and orchestrator mapping;
- field-guide input timestamp, source handles read, contradictions,
  and complete replacement candidate or explicit none;
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
Started: <UTC timestamp>
Worker: <configured model/effort; recorded runtime identity if available>
Mission: <neutral player goal>
Operational outcome: <pass|fail|uncertain|infrastructure_error>
Neutral observation: <initial spatial account, trajectory, unexpected details>
Acceptance mapping: <orchestrator owner/status and criterion mapping>
Guidance: <input snapshot timestamp; handles read; contradictions; replacement candidate or none>
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
