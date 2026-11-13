# Operating Resources

Resources replaced the asset-pipeline Resource Board in task 10487. It shows
what is using the workshop machines:

- measured telemetry and native queues;
- the shared resource directory (installs, model trees, caches);
- the advisory work ledger.

The API is in [resources-api-contract.md](resources-api-contract.md).

## Where to look

| What | Where |
|---|---|
| Page | `http://192.168.1.5:18080/resources` (den-web, top-level **Resources**) |
| CLI | `den-tool resources status`, `… resources [ID]`, `… work`, `… run`. `asset-resources` is a wrapper for it |
| API from the LAN | `http://192.168.1.5:18080/api/v1/resources/state` |
| Service | `den-go@resources.service` on CT106, `127.0.0.1:8103` |
| Store | `/data/services/resources/data/resources.sqlite3` on CT106 |
| Config | `/data/services/resources/config/config.yaml` (template `resources/config/config.example.yaml`) |
| Collector | `den-resources-collect.service` on den-agents (config `~/.config/den-resources-collect/config.json`, from `deployment/resources-collect/den-agents.json`) |

## How it fits together

The service is passive. It never polls or reaches any host.
`den-tool resources collect` runs on den-agents, which has the SSH access, and
every `poll_seconds` (20s) it does four things:

1. **Registers each configured host.** Its name, role, access, note and page
   order come from the collector config.
2. **Probes each machine read-only:**
   - `local` and `ssh` hosts run the embedded `resources_probe/probe.py`
     through `python3 -` (over SSH stdin for `ssh` hosts), so a host needs
     nothing installed except python3.
   - `windows` hosts run a copied `probe-windows.ps1` (below).
   - `push` hosts are skipped; an external reporter sends their telemetry.
   - The probe checks the paths of that host's directory entries for
     presence.
3. **Probes apps.** For `comfy` apps it reads the ComfyUI `/queue`: running
   and queued jobs, with their node types. For `health` apps it checks HTTP
   readiness only, so their queue stays unknown.
4. **Mirrors Workbench request files.** Queued and running request files
   become `workbench` work rows, timestamped with the file's modification
   time. A finished file closes only a row the ledger already shows as
   active.

A failed probe records its error and keeps the last good values. After
`stale_after` (90s) a host shows `unknown`, never quiet.

Add or change a host by editing `deployment/resources-collect/den-agents.json`,
then rerun `scripts/install-resources-collect.sh`. Add or change resources
through the page, `den-tool resources register FILE`, `touch` or `maintain`.
The directory lives in the service, not the config.

## Service and collector commands

```sh
# On CT106 (ssh -F ~/server-access/den-services/config target)
systemctl status den-go@resources.service
journalctl -u den-go@resources.service -n 50
curl -s http://127.0.0.1:8103/health

# On den-agents
systemctl status den-resources-collect.service
journalctl -u den-resources-collect.service -n 20
den-tool resources collect --dry-run       # one round, prints the writes instead of sending
scripts/install-resources-collect.sh       # (re)install config, service and asset-resources
```

The Windows probe is copied once, and again whenever it changes:

```sh
ssh den-win11 'New-Item -ItemType Directory -Force -Path C:/Users/agent/.local/share/asset-resources | Out-Null'
scp cmd/den-tool/resources_probe/probe-windows.ps1 den-win11:C:/Users/agent/.local/share/asset-resources/probe.ps1
```

Other Linux hosts that run `asset-resources run` get den-tool and the wrapper
with:

```sh
scripts/install-resources-cli.sh den-nimo den-m5 "-F $HOME/server-access/gpu-lab/config target"
```

Rerun that after a den-tool release that changes `resources`.

## Route a headless build

From den-agents, use a full Git commit SHA:

```sh
den-tool build run --repo rusty-engine --lane verify --ref "$(git -C ~/dev/rusty-engine rev-parse HEAD)"
```

The command uses [the build configuration](../deployment/build/den-agents.json)
installed at `~/.config/den-build/config.json`. To install it the first time,
run the following from the den-services checkout:

```sh
scripts/install-den-tool.sh
install -D -m 0640 deployment/build/den-agents.json ~/.config/den-build/config.json
```

Use `--config FILE` or `DEN_BUILD_CONFIG_PATH` to select a different file.
The config maps repositories and headless lanes to their scripts or CI
commands, required toolchain probes, and minimum free disk space. Engine has
`verify`, `verify-csharp`, `shader-matrix` and `pair`. The `pair` lane builds
and exercises the SDK/runtime archive on Ubuntu 26.04 LTS and prints its
worker-local output path. A lane's `os` is either an `/etc/os-release` `ID`
(any release) or `ID-VERSION_ID`. The pair lane pins `ubuntu-26.04` so
published pairs keep the LTS glibc baseline, and den-nimo and den-m5 (Ubuntu
26.10) are refused for it. Run GPU, window and performance lanes on their usual hosts.

The router ranks enabled workers using fresh Resources activity and CPU load.
It then checks toolchains and free space on the worker over SSH. Use
`--host HOST` to restrict selection to one enabled worker. Workers with a
GPU threshold also require a GPU utilization reading and skip fresh native
queues and active work reports. ComfyUI exposes its queue on both Strix Halo
hosts. For GPU work without a native queue report, the router uses active
Workbench reports and measured GPU utilization.

den-k8 fetches Git objects from the den-agents NFS mount and builds on its
own disk. The den-nimo and den-m5 entries use Git over SSH. Both machines are
provisioned identically by server-access `strix-halo/provision`, which
installs SSH access, sudo, and the Rust, .NET, Node and pnpm toolchains these
lanes probe for. Enable a worker entry after provisioning it. Both were
enabled on 2026-11-11 after provisioning. The configured job count caps Cargo, Rust test,
CMake and .NET parallelism on the worker.

Each worker uses `build_root/REPO/checkout` and runs one routed job at a time.
It resets the checkout to the requested commit and removes generated files
except the cache directories named in `keep` (Engine keeps `target/`).
Put retained outputs in `DEN_BUILD_OUTPUT_DIR`, which points outside the
checkout. The log streams to the caller and the command returns the lane's
exit code. The Resources ledger shows the worker, repo, lane, commit, and
completion state.

Rusty Dagger provides `ruleset`, `ruleset-1` through `ruleset-3`,
`import-suite`, `verify` and `data`. From the Dagger checkout,
`scripts/route.py ruleset` distributes the three parts across the workers.
Dagger test lanes keep a copy of the NAS data for the commit's importer
revision beside the worker checkout. The `data` lane publishes missing
revisions and uses den-k8 (Ubuntu 26.04) for consistent cinematic audio.

The `rusty-dagger-main-check.timer` user timer on den-agents runs
`scripts/route.py main` after boot and every half hour. Dagger handles worker
selection and records the result on the commit. Install or update the units
as `agent` from the den-services checkout.

```sh
install -d ~/.config/systemd/user
install -m 0644 deployment/build/rusty-dagger-main-check.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now rusty-dagger-main-check.timer
journalctl --user -u rusty-dagger-main-check.service -n 20
```

## Deploy, data and recovery

A `looks_good` review deploys the service through `scripts/deploy.sh`, which
also rebuilds den-tool on den-agents. After that, restart the collector so it
runs the new den-tool: `sudo systemctl restart den-resources-collect.service`.

The store holds the directory and the ledger. Telemetry is rebuilt within one
poll. The daily Proxmox backup of CT106 carries the store to den-nas.

To import the old Resource Board once, copy its database to CT106 and run:

```sh
sudo -u agent env RESOURCES_CONFIG_PATH=/data/services/resources/config/config.yaml \
  /data/services/resources/bin/resources import-board /path/to/board.sqlite3
```

The import is safe to repeat: rows that already exist are left alone.
