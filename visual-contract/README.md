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
  --screenshot /tmp/sample-page.png \
  --out /tmp/sample-page.web-evidence.json
```

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
