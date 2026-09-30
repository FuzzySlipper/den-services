---
name: product-playtest
description: Test games and web applications through crew-services playtest using persistent sessions, native controller/keyboard/mouse input or headless browser operations, supervised JavaScript, and original capture evidence. Use for visible product evaluation, interaction testing, reproduction, and visual comparisons without editing the product.
---

# Product Playtest

Use the installed crew-services `playtest` CLI or its matching MCP tools.
The maintained capability and setup reference is
[the service guide](/home/agent/dev/crew-services/docs/playtest.md); read the relevant
sections when choosing controls or scripting operations. This skill uses
crew-services throughout. Do not fall back to the retired Den browser broker,
Python controller, or an independently launched browser when a session fails.

## Parent and observer roles

For independent product observation, the coding/reviewing parent spawns
`agent_type: "playtester"` with a neutral mission. The worker operates and
observes; the parent owns implementation changes and acceptance mapping.
A worker already in that role does not spawn another playtester.

Supply the product/repository identity, profile or owned session ID, service URL
when nondefault, ordinary controls, requested observations, and any useful
scenario guidance. State whether an existing session should be retained or
stopped. A URL alone is not a configured profile: the parent owns adding a
profile or arranging service setup when discovery finds none.

The worker may read this skill and the service guide, use the playtest CLI/MCP,
write temporary JS test programs, inspect returned JSON evidence, and open
original images with its image tool. It must not inspect/edit product source,
repair services, change profiles, deploy replacements, or construct another
harness. Missing MCP registration is not a blocker when the CLI works.

## Local workstation service

On `den-agents`, the default service runs browsers locally on the RX 9070 XT.
The API binds only to `127.0.0.1:48200`; no SSH, Wolf, Moonlight, den-srv or
Den Kubernetes connection is needed. Use `backend: "browser"` and
`environment: "service"` for this installation. Local product servers use
`http://127.0.0.1:PORT/`; do not copy historical `192.168.1.22` profile URLs.

Profiles are in `/home/agent/.config/crew-playtest/games.json`, pool size in
`/home/agent/.config/crew-playtest/pool.json`, and evidence under
`/home/agent/.local/state/crew-playtest-local`. `local-gpu-check` is an
infrastructure check, not a game acceptance test. The parent adds product
profiles or changes pool size, then runs `playtest reload`. Reload preserves
existing sessions; invalid files and shrinks that would remove occupied slots
are refused. Do not restart the service to register a product.
See [local setup](/home/agent/dev/crew-services/docs/playtest-local.md).

The configured Chromium launcher uses Vulkan; setup verified WebGL2 on the
RX 9070 XT and a non-fallback AMD RDNA 4 WebGPU adapter. GPU presence alone does not prove every product's renderer.
This backend supplies browser keyboard/pointer actions and virtual gamepad
input, including relative `move` while the main document holds pointer lock.
Acquire lock with an ordinary click first. Use integer `dx`/`dy` in
-32767..32767; unlocking rejects subsequent relative movement. These are
trusted Chromium events, not OS mouse injection.

## Discover the execution environment

```sh
playtest games
playtest game show PROFILE
playtest status
```

The CLI is `/home/agent/.local/bin/playtest`. The default API is
`http://127.0.0.1:48200`, owned by `crew-playtest.service`. Use `PLAYTEST_URL` or
`playtest --url URL ...` for a supplied alternative loopback service.
`playtest mcp` exposes the same service operations; discover their current
schemas rather than assuming a tool-name prefix or a fixed tool count.

- `backend: "browser"` runs headless Chromium on the service machine. Use its
  DOM inspection/actions and absolute pointer/keyboard capabilities. It does
  not provide native relative mouse movement. When `capabilities.gamepad` is
  true, gamepad steps inject a standard browser Gamepad API device; this is
  virtual controller input, not native hardware evidence.
- `backend: "wolf"` is an optional legacy remote backend, not configured on
  this local service. It requires an explicitly supplied separate installation.
  Its native input evidence is different from browser virtual gamepad input.
- Read returned capabilities and report unsupported operations explicitly.
  Do not substitute one input/backend type and call the evidence equivalent.

`den-serve` owns building and serving a repository demo at a URL reachable from
the execution machine. crew-services owns test sessions, interaction, scripts,
observations and cleanup. The worker does not start or stop the demo server.
The service allocates an independent slot from its configured pool. `playtest
status` lists `pool` occupancy and per-slot `slots`; `status SESSION` inspects
your session. Retain both returned session ID and slot ID. Never stop or recover
another agent's session. If all slots are occupied, start queues briefly and may
return `pool_busy`; report that limitation or retry later without taking over a
slot. Other demos may keep rendering while their agents are idle; capture
`pool_activity` records occupancy, not a guarantee of isolated performance.

## Run the session

1. Use the supplied owned session, or `playtest start PROFILE`. Retain the
   returned session ID. If start fails, inspect the returned error and
   `playtest status`; return the observed limitation instead of provisioning a
   workaround. A degraded/interrupted owned session can be stopped or recovered.
2. Capture with `playtest observe SESSION` or `playtest capture SESSION` and
   open the returned original image. Record the visible initial scene before
   deciding whether it satisfies the mission. `connected` establishes launch,
   not asset readiness, focus, game input consumption, or visual acceptance.
   Wolf startup can deliver a native focus click, so the initial state is not
   guaranteed pristine.
3. Alternate bounded actions and observations. Use JS for a useful sequence,
   loop or conditional; direct commands remain appropriate for short probes.
   Verify the downstream effect of an interaction, not only its first reaction.
   Use before/after images for movement, camera changes and state transitions.
4. On uncertainty, inspect bounded diagnostics and distinguish them from what
   was visible. Treat scenario hints as fallible: retain contradictions rather
   than adjusting the observation to fit an expected answer.
5. Stop owned sessions with `playtest stop SESSION`, including after a failed
   mission, unless explicitly asked to retain them. Check the cleanup receipt;
   use `status` for discrepancies. Client disconnection does not stop a session.

## Inputs and browser operations

Use profile controls. Native batches have explicit kinds and millisecond holds:

```sh
playtest input SESSION --json '[{"kind":"gamepad","lx":0.3,"rt":0.5,"ms":400}]'
playtest input SESSION --json '[{"kind":"hold","keys":[87],"ms":200}]'
playtest input SESSION --json '[{"kind":"move","dx":35,"dy":-15}]'
playtest input SESSION --json '[{"kind":"point","x":640,"y":460,"width":1280,"height":720},{"kind":"click","button":1,"ms":100}]'
```

Raw keyboard holds use Windows virtual-key integers; the JS helper accepts
named keys. Controller sticks range -1..1, positive X right and positive Y up;
triggers range 0..1. Buttons use `buttons: ["a"]`, not `a: true`.
Holds release afterward. Native batches are bounded to 10 seconds. These are
real-time inputs, not admitted simulation-update counts or deterministic replay.
Wolf lock readback is unavailable: a delivered movement does not establish
pointer lock or game consumption. Use ordinary click/Escape/refocus controls
and observations when testing acquisition/loss; do not fabricate a lock state.

For a browser-capable session:

```sh
playtest browser SESSION --json '{"op":"inspect"}'
playtest browser SESSION --json '{"op":"fill","selector":".new-todo","value":"Example"}'
playtest browser SESSION --json '{"op":"press","selector":".new-todo","key":"Enter"}'
playtest browser SESSION --json '{"op":"click","selector":"button[type=submit]"}'
playtest browser SESSION --json '{"op":"near","x":300,"y":200,"max_distance":80}'
playtest browser SESSION --json '{"op":"select","token":"RETURNED_TOKEN","action":"click"}'
```

DOM assistance is bounded near-cursor selection, not game-world targeting.
`select` defaults to moving only; activation requires explicit `action: "click"`.
Tokens are short-lived and single-use. Respect stale, obstructed, disabled,
ambiguous and no-candidate outcomes. Use a specific locator after a strict-mode
ambiguity rather than assuming the first matching element is intended.
Record when DOM/semantic inspection or assistance influenced the result;
assisted interaction evidence does not prove unaided visual discovery or aiming.

## World objects: avoid repeated pixel hunting

For a world container, door or talk target, inspect the product's Engine debug
catalog before spending a long sequence guessing screen coordinates. Products
using `InteractionDebugModule` expose `interaction.help` and `interaction.inspect`.
The latter returns current labels, IDs/revisions, reach/visibility/availability,
rejection reasons and exact `useCommand` values. Use the matching runtime's
`rusty-live-debug --origin URL --command "interaction.inspect"`.

When target-ID assistance fits the requested test, execute the reported
`interaction.use <id> <revision>` through that same CLI. It is an explicit
mutating assisted action using the product's ordinary use handler; it removes
reticle precision only. Approach normally when out of reach, resolve occlusion,
and reinspect stale identities. Verify the actual resulting UI with the normal
playtest browser tools. Record this as assisted interaction, not proof of a
physical click. If the mission tests picking itself, use ordinary pointer input.

Missing commands are a product integration gap, not a reason to invent browser
gameplay hooks. The parent can adopt the shared Engine `WorldInteraction` and
`InteractionDebugModule`; see `/home/dev/rusty-engine/docs/controller-interaction.md`.
The existing `playtest interaction` query below remains read-only.

## Optional product interaction queries

For a profile with `interaction_queries: true`, use `playtest interaction SESSION`
or `await interaction()` in a script. The service checks the product's generated
Engine catalog; missing support reports `capability_unavailable`. Read returned
`facts` and retain `query_id`/`evidence_path`. Query facts are semantic assistance,
not screenshot freshness or permission to activate an old target observation.
Approach, cycle and use through ordinary controller/keyboard input, then query
again. Preserve product availability, visibility and unknown route outcomes.

Free-cursor queries use `interaction({mode:"cursor",x:0.5,y:0.5,aspect:width/height})`.
Supply known viewport-local normalized bottom-left coordinates and viewport aspect;
do not substitute mouse-look deltas. Queries never turn, navigate or activate.
Label captures and reports when these facts guided the test.

## Jev-assisted control intervals

For repeated navigation/combat decisions with useful text observations, consider
`playtest-assist` on the existing owned session. Read the
[assistant guide](/home/agent/dev/crew-services/docs/playtest-assistant.md) for setup,
spatial-map interpretation and the complete gamepad + concurrent-parent example.
The supervising agent supplies the goal, finite tactics and stop conditions;
Jev selects actions while an optional parent model periodically revises guidance.
Use this as a bounded debugging tool, with visual inspection before and afterward.

Combine compact product facts with a small `spatial.map ascii` read when supported.
Read its axes, legend, Y intervals and revisions: collision blanks do not prove
walkability, navigation may be unknown, and maps are omniscient assistance.
Use fresh target/weapon/interaction facts for immediate actions. Jev receives text only. The harness parent is text-only by default;
`parent_vision: true` attaches the current PNG pixels to its request. Use
`capture_every: 1` for a current image on every parent request and ensure capture
paths are readable where `playtest-assist` runs. Check transcript `input_image`
provenance; screenshot paths alone do not give either model vision.
New product commands require explicit harness allowlist support, not merely a
catalog entry. The guide explains the supported observation commands.

The parent arranges configuration, profile and model routes; an observer can run
an already supplied interval without editing the product or repairing services.
Do not drive the session manually or with a second script while it runs. Preserve
the JSONL transcript, inspect the actual handback reason and input cleanup, then
capture/inspect the result. A reached kill/objective threshold ends the interval
before its budget expires; continuing requires a revised goal/threshold. The
runner leaves the session open, so stop it when the mission is finished unless
asked to retain it. Record semantic/gamepad aim assistance in the final report.

## Compose supervised JavaScript

Write a temporary `.js` file and submit it with:

```sh
playtest run SESSION --file /absolute/path/trial.js --budget-ms 60000
playtest script SCRIPT_ID
playtest resume SESSION --json '{"keys":["W"]}'
```

`run` returns immediately. Poll `script` for completion, failure or a yielded
checkpoint; submitting a program is not evidence that it finished. Examples
below use different backend-specific capabilities; select those the session
supports.

```js
// Native game session: combine simultaneous movement/look in one state.
await controller.hold({ly: 0.5, rx: 0.25}, 300);
checkpoint('After movement', await observe());
const choice = await yieldToAgent('Choose the next action', await observe());
await keyboard.hold(choice.keys, 200);
```

```js
// Browser session: compose UI actions and preserve original comparison images.
await browser({op: 'fill', selector: '.new-todo', value: 'Example'});
await browser({op: 'press', selector: '.new-todo', key: 'Enter'});
const before = await capture({label: 'created'});
await browser({op: 'click', selector: '.toggle'});
checkpoint('Compare originals', await capture({label: 'completed', compare_to: before.capture_id}));
```

Other APIs are `input(steps)`, `sleep(ms)` and journaled `console.log(...)`.
Calls serialize; parallel promises do not create simultaneous controller/key
holds. The default program budget is 60 seconds, configurable from 100 ms to
120 seconds, with at most 512 API calls. Yield pauses count against the budget.
Split longer evaluations into programs and inspect results between them.

`cancel SESSION` terminates a running script and requests input cleanup. An
idle browser stays alive; cancelling an active browser operation can terminate
that browser and require explicit recovery. `recover SESSION` stops the old
session and returns a new session ID for the same profile. Re-inspect it before
continuing: progress may be lost, and product state may persist elsewhere.
Never automatically replay an action with unknown delivery. Report
`cleanup_uncertain` as uncertainty, not verified neutralization.

## Evidence and judgment

`observe` and `capture` return original image paths and metadata; the MCP can
return images inline. `capture` accepts `label`, `compare_to` (a previous
capture ID), caller-supplied `viewpoint` and `assistance`, and overlay policy
`preserve`. Do not hide product UI or diagnostics. Comparison metadata describes
known geometry and supplied viewpoint agreement; it is not a visual verdict.

For Engine products, `capture({engine_presentation:true})` (or the same CLI/MCP
capture option) records separate submitted camera/viewport/publication facts.
Profiles may enable this with `presentation_observations:true`. Inspect
`engine_presentation.facts` and `comparison.engine_presentation`, including
pending state, observation age and runtime/surface identity. A pending snapshot
can still show an older submitted camera. Requested viewpoint metadata is not
an observed pose; product viewpoint visits are explicit assisted movement.
The readback does not identify the PNG frame or establish GPU completion,
whole-world readiness or acceleration. Missing support is recorded while the
original capture remains useful.

Inspect originals directly. The final visual judge must see the original
images, record neutral observations, then map acceptance and state uncertainty.
Keep screenshot evidence, runtime diagnostics and ordinary-control usability
separate. Supplied viewpoint/assistance is not independently observed state;
screenshot dimensions do not establish canvas backing resolution, GPU rendering
or frame freshness. Use actual available metadata and leave missing facts
unknown. Engine readiness and world-target integration must be advertised by
the current service before use; do not invent a debug endpoint or game state.

Retain returned session/script/capture IDs, absolute image/sidecar paths and
journal paths. Cleanup success and evidence completeness are separate: report
persistence errors while preserving original artifacts. Do not reconstruct a
canonical history after a partial journal write. Private Moonlight logs can
contain credentials and should not be copied into reports.

Return a compact report:

- Mission and profile/session, with the execution backend.
- Primary outcome: `pass` (visible mission succeeded), `fail` (observed product
  failure), `uncertain`, or `infrastructure_error` (environment prevented testing).
- Neutral initial scene, important changes and unexpected details.
- Actions/reproduction steps; scripts, assistance or diagnostics used.
- Direct original-image links and relevant capture/script/journal paths.
- Cleanup receipt and any evidence or input-release uncertainty.

Useful control/navigation difficulties or replacement scenario notes can be
included for the parent. The worker does not publish shared guidance or create
follow-up engineering work. A successful neutral-observation mission need not
imply that the product passed a separate visual acceptance criterion.
