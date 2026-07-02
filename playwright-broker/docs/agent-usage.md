# Playwright Broker Agent Usage

Use `den-playwright` when an agent needs to run Playwright against a live dev server. Do not hand-pick a fixed port, reuse a random server that happens to answer, or kill an unknown process on a port. The broker owns the boring operational work: choose a safe port, start or safely reuse the right server, pass `BASE_URL`, run Playwright, and write evidence.

This broker is local-run infrastructure. It lives in `den-services` for discoverability, but it is not a den-srv systemd service and is not listed in `deployment/services.yaml`.

## Quick Start

From `/home/dev/den-services`:

```bash
export DEN_PLAYWRIGHT_BROKER_CONFIG_PATH=/home/dev/den-services/playwright-broker/config/config.example.yaml
go run ./playwright-broker/cmd/den-playwright run <project-id> -repo /path/to/repo -- --reporter=list
```

If `den-playwright` has already been built onto your `PATH`, the equivalent is:

```bash
export DEN_PLAYWRIGHT_BROKER_CONFIG_PATH=/home/dev/den-services/playwright-broker/config/config.example.yaml
den-playwright run <project-id> -repo /path/to/repo -- --reporter=list
```

Useful task-linked form:

```bash
den-playwright run rusty-view \
  -repo /home/dev/rusty-view \
  -den-project rusty-view \
  -den-task 1234 \
  --grep @live-agent
```

The command prints the evidence path, selected base URL, and final status when a run is created.

## Repo Manifest

Each repo opts in with one manifest at the repo root. Lookup order:

- `.den-playwright.json`
- `.playwright-service.json`
- `den-playwright.json`

You can also pass `-manifest /path/to/manifest.json`.

Minimal manifest:

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

Template variables available in commands and manifest env values:

- `{project}`: manifest project id.
- `{repo_root}`: repo path passed with `-repo`.
- `{host}`: selected bind host.
- `{port}`: selected port.
- `{base_url}`: selected base URL.
- `{artifact_root}`: run artifact directory.

## Common Flags

- `-repo /path/to/repo`: repo root containing the manifest. Defaults to current directory.
- `-manifest /path/to/file.json`: explicit manifest path.
- `-config /path/to/config.yaml`: broker config path. Usually use `DEN_PLAYWRIGHT_BROKER_CONFIG_PATH`.
- `--grep <expr>`: forwarded to Playwright as `--grep`.
- `--headed`: forwarded to Playwright as `--headed`.
- `--pw-project <name>`: forwarded to Playwright as `--project`.
- `--test <file-or-title>`: appended as a Playwright test argument.
- `-den-project <id>` and `-den-task <id>`: copied into the evidence packet for handoff.
- Arguments after `--` are passed through to the Playwright command.

## Safety Rules

The broker never kills arbitrary user processes.

If `preferredPort` is occupied:

- matching health + broker-owned lease: reuse is allowed;
- matching health + `"reusePolicy": "explicit"`: reuse is allowed;
- matching health + default `"broker_owned"` but no broker lease: broker chooses another port;
- wrong health, wrong text, wrong identity header, or dead lease: broker chooses another port.

Broker-owned dev servers are started in their own process group and stopped after the run. Stale broker lock files are recovered only when their recorded owner PID is no longer alive.

Use `"reusePolicy": "explicit"` only for servers that are intentionally reusable and have a reliable identity check.

## Health And Identity

A server is considered the right app only when the health response matches the manifest.

Supported checks:

- `healthUrl`: path to request on the selected port.
- `readyText`: text that must appear in the response body.
- `identityHeader`: response header whose value must equal the manifest `project`.

Use at least `readyText` or `identityHeader`. Prefer both for repos where multiple apps can answer on similar routes.

Example:

```json
{
  "serve": {
    "healthUrl": "/",
    "readyText": "Asha Studio",
    "identityHeader": "X-Den-Project"
  }
}
```

## Evidence

Every created run writes a machine-readable `run-index.json` under the configured artifact root, usually:

```text
~/.cache/den-playwright/runs/<project>/<run-id>/run-index.json
```

The packet includes:

- project id and repo path;
- selected host, port, and `BASE_URL`;
- dev-server command, owned PID, reuse source, and health result;
- Playwright command, args, exit code, duration, stdout/stderr log paths;
- artifact root and discovered artifact file paths;
- Den project/task metadata when supplied;
- `human_inspection_required` for live UI policies.

The broker also sets these environment variables for the Playwright command:

- `BASE_URL`
- `PLAYWRIGHT_BROKER_BASE_URL`
- `PLAYWRIGHT_BROKER_ARTIFACT_ROOT`
- `PLAYWRIGHT_BROKER_EVIDENCE_PATH`

Configure Playwright screenshots, traces, and videos to land under `PLAYWRIGHT_BROKER_ARTIFACT_ROOT` when possible.

## Human Inspection

`artifactPolicy: "live-ui"` sets `human_inspection_required: true`. A passing Playwright result does not replace visual inspection when the acceptance criteria are subjective, layout-heavy, animation-heavy, or otherwise visually ambiguous.

For live UI work, attach or mention:

- the `run-index.json` path;
- screenshot, trace, or video paths from the artifact root;
- whether you inspected the evidence yourself;
- any visual uncertainty that remains.

## Troubleshooting

If the broker picks a different port than expected, that is usually correct. It means the preferred port was busy, unsafe to reuse, or failed identity checks. Use the evidence packet's `server.health` and `server.reuse_source` fields before touching processes.

If a run fails before printing evidence, setup failed before a run directory existed. Check the config path, manifest path, and manifest `project` value.

If Playwright cannot reach the app, confirm the test reads `BASE_URL` or `PLAYWRIGHT_BROKER_BASE_URL` instead of a hardcoded localhost port.

If a broker lock times out, another live broker run may be active. Do not delete lock files unless you have verified the recorded PID is gone; the broker already recovers stale locks for dead PIDs.

If artifacts are missing, check whether the repo's Playwright config writes traces/screenshots/videos into `PLAYWRIGHT_BROKER_ARTIFACT_ROOT`. The broker indexes files that exist under the artifact root; it does not rewrite Playwright's artifact policy by itself.
