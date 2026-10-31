# Operating Ops

Ops is Den's central record of backup and maintenance status (campaign 10442).
The API is described in [ops-api-contract.md](ops-api-contract.md), and how
machines report in [ops-reporting.md](ops-reporting.md).

## Where to look

| What | Where |
|---|---|
| Operator page | `http://192.168.1.5:18080/ops` (den-web, Admin → Operations) |
| CLI | `den-tool ops status`, `den-tool ops errors`, `den-tool ops job JOB` |
| API from the LAN | `http://192.168.1.5:18080/api/v1/ops/summary` (web-edge → Gateway → Ops) |
| API on CT106 | Gateway `http://127.0.0.1:8079/v1/ops/...`, Ops `http://127.0.0.1:8102/v1/ops/...` |
| Service | `den-go@ops.service` on CT106 (den-services, 192.168.1.5, on den-prox-mini) |
| Ledger | `/data/services/ops/data/ops.sqlite3` on CT106's local ZFS rootfs; no NAS or Postgres |
| Config | `/data/services/ops/config/config.yaml` (template: `ops/config/config.example.yaml`) |
| Ledger exports | `/data/services/ops/backups/daily/ops-*.sqlite3` |
| Evidence collector | `den-ops-collect.timer` on den-agents (see ops-reporting.md) |

## Service commands

Run these on CT106 (`ssh -F ~/server-access/den-services/config target` from
den-agents):

```sh
systemctl status den-go@ops.service
sudo systemctl restart den-go@ops.service     # only Ops; nothing else depends on it
sudo systemctl stop den-go@ops.service        # reporters spool until it is back
journalctl -u den-go@ops.service -n 50
curl -s http://127.0.0.1:8102/health; curl -s http://127.0.0.1:8102/version
```

## Deploy and rollback

A `looks_good` review deploys the reviewed commit through `scripts/deploy.sh`.
It redeploys Ops only when Ops changed, and it rolls back automatically if
health fails. To roll back by hand:

```sh
# On CT106: the previous binary is kept beside the current one.
sudo install -m 0755 /data/services/ops/bin/ops.previous /data/services/ops/bin/ops
sudo systemctl restart den-go@ops.service
# Or, from den-agents, redeploy an earlier commit:
scripts/deploy.sh --commit SHA
```

Older binaries are also kept under `/data/services/ops/releases/`. Ledger
migrations only add things, so an older binary can still read a newer ledger.

## Ledger backups

The Ops service backs up its own ledger. Every day at 05:30 Pacific, as job
`ops-sqlite-export` (host `den-services`), it:

1. writes a consistent `VACUUM INTO` snapshot, `ops-<UTC>.sqlite3`;
2. copies the snapshot to a scratch file, opens it as an Ops ledger, runs
   `integrity_check` and computes a status summary. That is the run's restore
   test, and its counts appear in the run;
3. keeps the newest 14 snapshots, pruning only after the new snapshot passes
   its restore test, so a bad export never pushes out a good one;
4. records the run in its own ledger. A failure shows as `failed`, and a
   missed day as `overdue`.

**Off-host copy:** the export directory is inside CT106. The 06:00 Proxmox
backup of CT106 (job `den-prox-mini-vzdump-106`, NFS on den-nas, keep-all)
therefore carries each morning's export to den-nas. Nobody has yet restored a
whole CT106 archive; that is untested.

**Manual export:** run `sudo -u agent env OPS_CONFIG_PATH=/data/services/ops/config/config.yaml /data/services/ops/bin/ops backup`.
It runs safely beside the service and prints the recorded report.

### Restore the ledger

```sh
sudo systemctl stop den-go@ops.service
sudo -u agent mv /data/services/ops/data /data/services/ops/data.replaced-$(date +%Y%m%d)
sudo -u agent install -d -m 0750 /data/services/ops/data
sudo -u agent cp /data/services/ops/backups/daily/ops-CHOSEN.sqlite3 /data/services/ops/data/ops.sqlite3
sudo systemctl start den-go@ops.service
den-tool ops collect --resend      # on den-agents: refill evidence newer than the export
```

To rebuild CT106 from scratch:
1. Restore CT106 from its vzdump archive with `pct restore` to a new ID.
2. Alternatively, deploy den-services fresh and copy an export into place as
   above.
3. With no export at all, start Ops empty and run `den-tool ops collect --resend`.
   The collector re-registers every job and refills 14 days of evidence.

## Coverage and known gaps

This table shows what Ops tracks, and what nothing protects. Unknown coverage
is shown as such and never as healthy.

| Machine | Tracked jobs | Not covered |
|---|---|---|
| den-agents (VM 100 on den-prox) | `den-agents-git` (Git projects to den-nas Borg), `den-agents-git-maintenance` | No Proxmox guest backup of the VM; its 2 TB `/data` disk is `backup=0`. Only Git project trees are backed up |
| den-nas | `den-nas-config`, `den-nas-snapraid`, `den-nas-offsite` (pictures, documents and bare Git repositories to B2 Restic), `den-nas-offsite-maintenance` | Other media, downloads and recordings have only SnapRAID parity, which is not a backup. Offsite Git is pushed history only. The offsite restore test is a sample restore; a full read of every pack is the first-Saturday `--read-data` check |
| den-k8, den-win11 | `den-k8-config`, `den-win11-config` | Configuration only; no whole-machine backup |
| den-prox, den-prox-mini | `den-prox-config`, `den-prox-mini-config` | Host configuration only |
| Guests on den-prox-mini | `den-prox-mini-vzdump-{100,103,104,105,106,107}` | `gate-deb` (VM 102) and `unifi` (CT 101) are in no backup job and show as *not configured*. vzdump archives are never verified or restore-tested |
| GitHub (FuzzySlipper) | `github-archives`, `github-fuzzyslipper-*` (8 repositories) | Default-branch source ZIPs only: no Git history, local uncommitted work, submodule repositories, and LFS payloads only if GitHub includes them. The ZIP CRC check is not a restore |
| Thin config NAS copy | `host-configs-nas-copy` | — |
| Ops itself (CT106) | `ops-sqlite-export`, `den-prox-mini-vzdump-106` | Whole-CT restore not tested |

## Recovery material on the NAS

Copies of this runbook, the API contract, the reporting guide, the Ops config
and the collector config are kept in `den-nas:/mnt/storage/backup/den-services/ops-recovery/`.
They hold no secrets. Refresh them after changing any of these files.
