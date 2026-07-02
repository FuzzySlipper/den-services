# Agent Usage

Use `den-playwright` instead of hand-picking fixed ports for Playwright runs.

```bash
export DEN_PLAYWRIGHT_BROKER_CONFIG_PATH=/home/dev/den-services/playwright-broker/config/config.example.yaml
den-playwright run rusty-view -repo /home/dev/rusty-view --grep @live-agent
```

Manifest lookup checks `.den-playwright.json`, `.playwright-service.json`, then `den-playwright.json` in the repo root unless `-manifest` is supplied.

Evidence is machine-readable JSON:

- project id and repo path;
- selected host, port, and `BASE_URL`;
- dev-server command, owned PID, reuse source, and health result;
- Playwright command, args, exit code, duration, stdout/stderr log paths;
- artifact root and discovered artifact file paths;
- Den project/task metadata when supplied with `-den-project` and `-den-task`;
- `human_inspection_required` for live UI policies.

The broker never kills arbitrary processes. If the preferred port is occupied by the wrong app, it records the mismatch and chooses a fallback port. It reuses an existing process only when the health check matches the manifest and either the process is broker-owned or the manifest sets `"reusePolicy": "explicit"`.
