# visual-contract

`visual-contract` turns typed visual evidence into `layered-visual-contract/v0.1`
contracts, compares candidate contracts against authored/reference contracts,
and persists report/overlay artifacts for agent repair loops.

## Browser Evidence Collector

The local proof collector lives at:

```bash
visual-contract/tools/browser-evidence-collector.mjs
```

It expects Playwright Chromium in the calling environment and emits a
`WebEvidenceRequest` payload compatible with:

```text
POST /visual-contracts/from-web-evidence
```

Install the Node test/collector dependency from the repo root:

```bash
pnpm install
pnpm exec playwright install chromium
```

Example:

```bash
pnpm exec node visual-contract/tools/browser-evidence-collector.mjs \
  --url "file:///home/dev/den-services/visual-contract/testdata/web/sample-page.html" \
  --scene-id sample_page \
  --capture-mode viewport-clipped \
  --screenshot /tmp/sample-page.png \
  --out /tmp/sample-page.web-evidence.json
```

The default viewport is `1920x1080`. Override it with `--width` and `--height`
only when the target app surface needs a different desktop baseline.

For app shells, editors, and ASHA-like workspaces, use:

```bash
pnpm exec node visual-contract/tools/browser-evidence-collector.mjs \
  --url "file:///path/to/app.html" \
  --scene-id app_shell \
  --capture-mode viewport-clipped \
  --root-selector "[data-visual-id='app_shell']" \
  --screenshot /tmp/app-shell.png \
  --out /tmp/app-shell.web-evidence.json
```

Capture modes:

- `viewport-clipped`: default. Includes only nodes intersecting the current
  viewport and clips partially visible bounds to the viewport. This is the mode
  to send to `/visual-contracts/from-web-evidence`.
- `viewport`: includes only nodes intersecting the viewport but preserves raw
  viewport-relative bounds. Partially visible nodes may exceed the viewport and
  the service will reject them with the offending node ID.
- `page`: emits page/document-space evidence and full-page screenshots. The
  current `/visual-contracts/from-web-evidence` route rejects this mode because
  page coordinates are not viewport-normalized.

Run the local collector smoke:

```bash
pnpm run test:visual-contract-collector
```

Agents should prefer stable markers:

- `data-visual-id` for object identity;
- `data-testid` as a fallback stable ID;
- `data-visual-role` for project vocabulary;
- accessible names/roles for human-facing semantics.

When the collector includes an intermediate selected DOM node without a stable
marker, it assigns it a `node-N` ID and parents descendants to the nearest
selected ancestor. The service normalizes those raw node IDs when converting
web evidence to a visual contract, for example `node-1` becomes `node_1`.

The collector intentionally does not dump full HTML, secrets, auth tokens, or
large uncontrolled text. It captures viewport, selected DOM/accessibility hints,
box geometry, parent relationships, and a small computed-style summary.

## Drafting Target Contracts

Use a two-step workflow for project-specific target contracts:

1. collect/decompose a generic page shape with the browser collector;
2. promote the generated IDs into project vocabulary, object IDs, roles, and
   authored constraints.

The promotion endpoint is:

```text
POST /visual-contracts/promote-contract
```

It accepts either a generic `contract` or raw web `evidence`, plus explicit
promotion rules:

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
      "domain_role": "central_3d_viewport"
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

The response contains a validated `layered-visual-contract/v0.1` contract and
draft diagnostics for unmapped important nodes or vocabulary mismatches. The
fixture `visual-contract/testdata/authored/asha-promotion.json` shows the compact
mapping shape used to draft an ASHA-like target from generic nodes.

## Service Usage

The deployed API requires the `DEN_VISUAL_CONTRACT_SERVICE_TOKEN` bearer token
for all `/visual-contracts/*` routes.

Convert browser evidence into a contract:

```bash
curl -fsS \
  -H "Authorization: Bearer ${DEN_VISUAL_CONTRACT_SERVICE_TOKEN}" \
  -H "Content-Type: application/json" \
  --data @/tmp/sample-page.web-evidence.json \
  http://127.0.0.1:8086/visual-contracts/from-web-evidence
```

Promote a generic contract into a project-specific target contract:

```bash
curl -fsS \
  -H "Authorization: Bearer ${DEN_VISUAL_CONTRACT_SERVICE_TOKEN}" \
  -H "Content-Type: application/json" \
  --data @/tmp/visual-contract-promotion.json \
  http://127.0.0.1:8086/visual-contracts/promote-contract
```

Compare a reference and candidate contract:

```bash
curl -fsS \
  -H "Authorization: Bearer ${DEN_VISUAL_CONTRACT_SERVICE_TOKEN}" \
  -H "Content-Type: application/json" \
  --data @/tmp/visual-contract-compare.json \
  http://127.0.0.1:8086/visual-contracts/compare
```

Compare responses include a `run_id` and artifact refs. Fetch artifacts through
the service with the same bearer token:

```bash
curl -fsS \
  -H "Authorization: Bearer ${DEN_VISUAL_CONTRACT_SERVICE_TOKEN}" \
  http://127.0.0.1:8086/visual-contracts/{run_id}/artifacts/report.json
```
