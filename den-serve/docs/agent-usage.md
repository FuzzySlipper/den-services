# den-serve Agent Usage

Use `den-serve` when an agent needs to leave a dev or demo app running for human inspection over the LAN. Do not hand-pick a fixed port, bind only to localhost, reuse a random server that happens to answer, or kill an unknown process on a port.

`den-serve` is local-run infrastructure. It lives in `den-services` for discoverability, but it is not a den-srv systemd service and is not listed in `deployment/services.yaml`.

## Quick Start

From `/home/dev/den-services`:

```bash
go run ./den-serve/cmd/den-serve up asha-demo -repo /home/dev/asha-demo
```

The command prints a packet like:

```text
asha-demo running
local: http://127.0.0.1:5173/
lan:   http://192.168.1.22:5173/
state: /home/agent/.cache/den-serve/sessions/asha-demo-abc123/current.json
logs:  /home/agent/.cache/den-serve/sessions/asha-demo-abc123/asha-demo-...
pid:   12345
```

The LAN URL is the one to give the human.

## Commands

```bash
den-serve up <project-id> -repo /path/to/repo
den-serve status <project-id> [-repo /path/to/repo]
den-serve list
den-serve logs <project-id> [-repo /path/to/repo]
den-serve stop <project-id> [-repo /path/to/repo]
```

Use `--public-host <ip-or-host>` only when automatic LAN IP detection chooses the wrong address. There is intentionally no prominent `--host` flag: started dev servers bind LAN-facing by default.

`den-serve` has built-in defaults matching `den-serve/config/config.example.yaml`. Use `-config` or `DEN_SERVE_CONFIG_PATH` only when overriding those defaults.

## Repo Manifest

`den-serve` reads the `project` and `serve` block from one repo-root manifest. Lookup order:

- `.den-serve.json`
- `den-serve.json`
- `.den-playwright.json`
- `.playwright-service.json`
- `den-playwright.json`

Minimal manifest:

```json
{
  "project": "asha-demo",
  "serve": {
    "command": "npm run dev -- --host {host} --port {port}",
    "preferredPort": 5173,
    "healthUrl": "/health",
    "readyText": "\"project\": \"asha-demo\"",
    "identityHeader": "X-Den-Project",
    "reusePolicy": "broker_owned",
    "startupTimeout": "45s"
  }
}
```

`identityHeader` must return the project ID by default. Set
`identityHeaderValue` when a host contract uses a different stable value, such
as `"browser-host.v0"` for `X-ASHA-Browser-Host`; pair that host identity with
`readyText` when it must independently identify the project.

`den-serve` does not require a Playwright `tests` block when reading `.den-playwright.json`.

Template variables available in serve commands and manifest env values:

- `{project}`
- `{repo_root}`
- `{host}` / `{bind_host}`
- `{probe_host}`
- `{port}`
- `{local_url}`
- `{public_url}`
- `{session_dir}`

## Safety Rules

If `preferredPort` is occupied:

- matching health + matching broker-owned lease: reuse is allowed;
- matching health + `"reusePolicy": "explicit"`: reuse is allowed, but `den-serve stop` will not kill it;
- wrong health, wrong text, wrong identity header, or unowned broker-owned policy: `den-serve` chooses a fallback port;
- no safe port: the command fails clearly.

Broker-owned dev servers are started in their own process group. `stop` only stops broker-owned process groups with recorded live sessions. It never kills arbitrary user processes.

## Session State

Session state lives under `~/.cache/den-serve/sessions` by default and is keyed by project plus repo path. Two worktrees with the same project id get separate session records. `status`, `list`, `logs`, and `stop` work from this persisted state.
