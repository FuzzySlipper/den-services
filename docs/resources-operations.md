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
