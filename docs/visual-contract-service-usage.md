# Visual Contract Service Usage

This is the agent-facing usage reference for the deployed `visual-contract`
service on `den-srv`.

## Deployment

- Service: `visual-contract`
- systemd unit: `den-go@visual-contract.service`
- Deployed health: `http://127.0.0.1:8086/health` on `den-srv`
- API base: `http://127.0.0.1:8086/visual-contracts` on `den-srv`
- Current schema: `layered-visual-contract/v0.1`

The service is loopback-bound on `den-srv`. From another host, either run the
request over SSH on `den-srv` or create an SSH tunnel. Do not assume
`192.168.1.10:8086` is reachable.

Auth is required for every `/visual-contracts/*` route:

```sh
set -a
. /etc/den-services/visual-contract.env
set +a
```

Use `Authorization: Bearer $DEN_VISUAL_CONTRACT_SERVICE_TOKEN`. Do not print the
token in logs, task comments, docs, or command output.

## What It Does

The service compares structured visual contracts, not raw screenshots. Typical
agent loop:

1. Collect browser evidence with Playwright.
2. Convert evidence to a generic visual contract.
3. Optionally promote generated object IDs into project vocabulary.
4. Compare a reference contract and a candidate contract.
5. Fetch report/overlay artifacts and use failures as repair hints.

Use this when an agent needs a repeatable visual check for web UI layout,
presence, relative position, containment, alignment, or major bounding-box
drift.

Do not use it as OCR, image generation, pixel-perfect screenshot diffing, or a
substitute for manual design judgment.

## Collect Browser Evidence

Run from a checkout that has Node dependencies installed:

```sh
pnpm install
pnpm exec playwright install chromium
```

Collect viewport-clipped evidence:

```sh
pnpm exec node visual-contract/tools/browser-evidence-collector.mjs \
  --url "http://127.0.0.1:3000" \
  --scene-id app_shell \
  --capture-mode viewport-clipped \
  --root-selector "[data-visual-id='app_shell']" \
  --screenshot /tmp/app-shell.png \
  --out /tmp/app-shell.web-evidence.json
```

Prefer stable markers in the app:

- `data-visual-id` for stable object identity.
- `data-testid` as a fallback stable ID.
- `data-visual-role` for project vocabulary.
- Accessible role/name for human-facing semantics.

Use `viewport-clipped` for service input. `page` mode is not accepted by
`from-web-evidence`.

## Convert Evidence To A Contract

Payload shape:

```json
{
  "evidence": {
    "scene_id": "app_shell",
    "coordinate_space": "viewport",
    "capture_mode": "viewport-clipped",
    "viewport": {"width_px": 1920, "height_px": 1080},
    "screenshot_ref": "app-shell.png",
    "nodes": []
  }
}
```

Request:

```sh
ssh den-srv '
  set -euo pipefail
  set -a; . /etc/den-services/visual-contract.env; set +a
  curl -fsS \
    -H "Authorization: Bearer ${DEN_VISUAL_CONTRACT_SERVICE_TOKEN}" \
    -H "Content-Type: application/json" \
    --data @/tmp/app-shell.web-evidence.json \
    http://127.0.0.1:8086/visual-contracts/from-web-evidence
'
```

The response is a validated `layered-visual-contract/v0.1` contract.

## Promote A Generic Contract

Promotion turns generated IDs like `node_2` into durable project vocabulary and
can add authored constraints.

Endpoint:

```text
POST /visual-contracts/promote-contract
```

Payload shape:

```json
{
  "contract": {},
  "project": {
    "id": "asha",
    "vocabulary": "asha_studio_v0",
    "roles": ["central_3d_viewport", "scene_hierarchy"]
  },
  "objects": [
    {
      "source_id": "node_2",
      "target_id": "central_3d_viewport",
      "domain_role": "central_3d_viewport",
      "importance": "critical"
    }
  ],
  "ignore_objects": ["node_noise"],
  "constraints": [
    {
      "id": "central_viewport_exists",
      "type": "object_exists",
      "object": "central_3d_viewport",
      "importance": "critical"
    }
  ]
}
```

The response includes a validated contract and diagnostics for unmapped
important nodes or unknown vocabulary roles.

## Validate A Contract

```sh
ssh den-srv '
  set -euo pipefail
  set -a; . /etc/den-services/visual-contract.env; set +a
  curl -fsS \
    -H "Authorization: Bearer ${DEN_VISUAL_CONTRACT_SERVICE_TOKEN}" \
    -H "Content-Type: application/json" \
    --data @/tmp/validate.json \
    http://127.0.0.1:8086/visual-contracts/validate
'
```

Payload:

```json
{"contract": {}}
```

Response includes `valid`, `scene_id`, and object/relation/constraint/evidence
counts.

## Compare Contracts

Endpoint:

```text
POST /visual-contracts/compare
```

Payload:

```json
{
  "reference": {},
  "candidate": {}
}
```

Request:

```sh
ssh den-srv '
  set -euo pipefail
  set -a; . /etc/den-services/visual-contract.env; set +a
  curl -fsS \
    -H "Authorization: Bearer ${DEN_VISUAL_CONTRACT_SERVICE_TOKEN}" \
    -H "Content-Type: application/json" \
    --data @/tmp/compare.json \
    http://127.0.0.1:8086/visual-contracts/compare
'
```

Response includes:

- `verdict`: `pass`, `needs_revision`, or `fail`
- `score`
- `run_id`
- `passes`, `failures`, `warnings`
- `artifacts` with report and SVG overlay URLs

Fetch artifacts with the same bearer token:

```sh
curl -fsS \
  -H "Authorization: Bearer ${DEN_VISUAL_CONTRACT_SERVICE_TOKEN}" \
  http://127.0.0.1:8086/visual-contracts/{run_id}/artifacts/report.json
```

Available generated artifact names include:

- `report.json`
- `reference.overlay.svg`
- `candidate.overlay.svg`
- `diff.overlay.svg`
- `reference.contract.json`
- `candidate.contract.json`

## Constraint Types

Supported constraint types:

- `object_exists`
- `layout_relation`
- `relative_position`
- `alignment`
- `area_ratio`
- `bounds_tolerance`
- `containment`

Supported relation values:

- `left_of`, `right_of`, `above`, `below`
- `inside`, `contains`, `overlaps`
- `aligned_left`, `aligned_right`, `aligned_top`, `aligned_bottom`
- `dominant_over`

Importance values:

- `critical`
- `major`
- `minor`
- `advisory`

## Practical Agent Guidance

- Save the reference contract as a durable artifact in the project repo or Den
  document when it represents an intended UI state.
- Compare against candidate contracts generated from the same viewport size and
  root selector.
- Use failures and `repair_hint` fields to decide concrete UI fixes.
- Prefer authored/promoted contracts over raw generated node IDs for long-lived
  checks.
- Keep screenshots as supporting evidence; the service compares contracts.

## Known Limitations

- The service does not ingest raw screenshots directly.
- Browser evidence must be viewport-normalized; page-space evidence is rejected.
- It does not run Playwright itself. Use the collector script or another tool to
  create evidence.
- Artifact URLs in responses use the service's configured base URL, often
  `127.0.0.1`; fetch them from `den-srv` or through a tunnel.
- The service is currently a local successor utility, not a public browser API.
