# visual-contract

`visual-contract` turns typed visual evidence into `layered-visual-contract/v0.1`
contracts, compares candidate contracts against authored/reference contracts,
and persists report/overlay artifacts for agent repair loops.

## Browser Evidence Collector

The local proof collector lives at:

```bash
visual-contract/tools/browser-evidence-collector.mjs
```

It expects Playwright in the calling environment and emits a
`WebEvidenceRequest` payload compatible with:

```text
POST /visual-contracts/from-web-evidence
```

Example:

```bash
npx playwright install chromium
node visual-contract/tools/browser-evidence-collector.mjs \
  --url "file:///home/dev/den-services/visual-contract/testdata/web/sample-page.html" \
  --scene-id sample_page \
  --screenshot /tmp/sample-page.png \
  --out /tmp/sample-page.web-evidence.json
```

Agents should prefer stable markers:

- `data-visual-id` for object identity;
- `data-testid` as a fallback stable ID;
- `data-visual-role` for project vocabulary;
- accessible names/roles for human-facing semantics.

The collector intentionally does not dump full HTML, secrets, auth tokens, or
large uncontrolled text. It captures viewport, selected DOM/accessibility hints,
box geometry, parent relationships, and a small computed-style summary.
