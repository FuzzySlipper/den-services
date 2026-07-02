# Playwright Broker

`playwright-broker` provides the `den-playwright` local CLI for agent browser-test runs. It is a den-services module for discoverability and shared conventions, but it is intentionally not listed in `deployment/services.yaml`: the broker owns local ports and child processes where the tests run, currently den-k8.

## Run

Create a broker config from `config/config.example.yaml`, then add a manifest to a repo:

```json
{
  "project": "rusty-view",
  "serve": {
    "command": "pnpm exec nx run rusty-view:serve -- --host {host} --port {port}",
    "preferredPort": 4200,
    "healthUrl": "/",
    "readyText": "rusty-view",
    "reusePolicy": "broker_owned"
  },
  "tests": {
    "command": "pnpm exec playwright test",
    "config": "apps/rusty-view-e2e/playwright.config.mts",
    "artifactPolicy": "live-ui"
  }
}
```

```bash
den-playwright run rusty-view -config playwright-broker/config/config.example.yaml -repo /path/to/repo -- --reporter=list
```

The broker selects a safe port, starts or reuses only a matching server, sets `BASE_URL`, runs Playwright, stops only broker-owned processes, and writes `run-index.json` under the configured artifact root.

`artifactPolicy: "live-ui"` marks the evidence packet with `human_inspection_required: true`; Playwright pass/fail is not a substitute for inspecting screenshots/traces when the UI is subjective.

See `docs/agent-usage.md` for the agent-facing usage guide, including manifest fields, safety rules, evidence handoff, and troubleshooting.
