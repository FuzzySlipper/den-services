# Reporting backups to Ops

How machines send backup and maintenance results to Ops. The API itself is in
[ops-api-contract.md](ops-api-contract.md).

**Ops only records what it is told.** The machine's own timer, scheduled task or
Proxmox job keeps running the backup. Reporting is *additive*:

- it never changes a backup's policy or exit status;
- it never starts a second copy;
- if Ops is down, the backup still runs and finishes normally.

## Endpoints

| From | Base URL |
|---|---|
| Any LAN host | `http://192.168.1.5:18080/api` (web-edge → Gateway → Ops) |
| On CT106 itself | `http://127.0.0.1:8079` (Gateway) or `http://127.0.0.1:8102` (Ops) |

Paths are the same everywhere: `…/v1/ops/...`. No credentials are needed.

## 1. Register the job once

Register each job before its first run, so a job that never reports still
shows as `never_reported`:

```sh
den-tool ops register-host den-nas host.json      # {"display_name":"den-nas","role":"NAS"}
den-tool ops register-job den-nas-config job.json # see the contract for job fields
```

Without den-tool, send the same JSON with
`curl -X PUT --data-binary @job.json $OPS/v1/ops/jobs/den-nas-config`.

## 2. Report runs

### Linux: wrap the command with den-tool

```sh
den-tool ops run --job den-k8-config --engine borg \
  --target den-agents:~/.local/state/den-config-backup/borg \
  --log-path /var/log/den-k8-config.log -- /usr/local/bin/backup-den-k8.sh
```

**Output and exit status:**
- The command's stdin, stdout and stderr pass through unchanged.
- den-tool exits with the command's own status, or 128+signal if the command
  was killed.
- INT and TERM are forwarded to the command.

**Reports:**
- den-tool sends a `running` report at start and every `--heartbeat`
  (default 1m), then one terminal report.
- Exit 0 is `success`. A code listed in `--warning-exit-codes` is `warning`
  (`--engine borg` makes Borg's exit 1 a warning by default). Anything else is
  `failed`.
- The last 8 KiB of stderr goes into the report's warning or error text.

**When Ops is unreachable:**
- Heartbeats are dropped, with a warning printed once.
- The terminal report is written to the spool (`$DEN_OPS_SPOOL`, default
  `~/.local/state/den-tool/ops-spool`).
- The spool is replayed by the next `den-tool ops run` or `ops report`, or by
  `den-tool ops replay` (which you can put on a timer).
- Replays are idempotent by job and run ID, so sending the same report twice
  never creates a second run.
- A report Ops rejects as invalid (HTTP 4xx) moves to `rejected/` in the spool,
  so it can't block the reports behind it.

`--run-id` sets a stable run ID. The default is the UTC start time plus the
process ID.

### Linux: a job that already writes its own report

The den-agents Git backup (`~/.local/lib/den-agents-backup/backup.py`) already
writes Ops-compatible JSON: `latest.json`, plus `runs/RUN_ID/result.json` for
each run. Send those files unchanged:

```sh
den-tool ops report ~/.local/state/den-agents-backup/latest.json
den-tool ops verification den-agents-git restore-test-$(date -u +%Y%m%dT%H%M%SZ) \
  ~/.local/state/den-agents-backup/restore-test.json
```

On den-agents these files are reported automatically by the collector (below),
so the backup service itself is unchanged.

### Windows (PowerShell, no den-tool needed)

```powershell
$ops = 'http://192.168.1.5:18080/api/v1/ops'
$run = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ') + "-$PID"
$start = (Get-Date).ToUniversalTime().ToString('o')
& C:\Backup\run-backup.ps1
$code = $LASTEXITCODE
$end = (Get-Date).ToUniversalTime().ToString('o')
$state = if ($code -eq 0) { 'success' } else { 'failed' }
$body = @{ job_id='den-win11-config'; run_id=$run; host='den-win11'; kind='backup'; state=$state
           started_at=$start; updated_at=$end; finished_at=$end; exit_code=$code } | ConvertTo-Json
try { Invoke-RestMethod -Method Put -Uri "$ops/jobs/den-win11-config/runs/$run" -Body $body -ContentType 'application/json' }
catch { $body | Out-File "C:\ProgramData\den-ops-spool\$run.json"; Write-Warning "Ops unreachable: $_" }
exit $code
```

- The script exits with the backup's own `$code`.
- A failed report is saved locally instead. Resend the saved files later with
  the same `Invoke-RestMethod` call: they keep their `run_id`, so a resend is
  recorded as a duplicate.
- `den-tool.exe` also builds for Windows (`GOOS=windows go build ./cmd/den-tool`),
  and `den-tool ops run` works there too.

### Proxmox VM/CT backups

Proxmox runs vzdump on its own schedule, so the collector (below) reads what
Proxmox records instead of wrapping anything. Over SSH it reads the cluster
backup jobs, the guest list and each guest's config, the vzdump task logs and
the backup storage inventory:

- **Coverage:** every guest becomes an Ops host. A guest in an enabled backup
  job gets the job `<proxmox-host>-vzdump-<vmid>` with the job's schedule (in
  the node's time zone), target and retention. A guest in no job stays a host
  with no job, so it shows as *not configured*.
- **Exclusions:** the job scope and host notes list the disks vzdump leaves
  out: VM disks with `backup=0`, and container mount points that are bind
  mounts or lack `backup=1`.
- **Runs:** each guest's part of a task log is one run (`<UPID>:<vmid>`), with
  its archive volume, size and local start and finish times. `WARN:` lines make
  it a warning and `ERROR: Backup of VM … failed` a failure. A guest still
  running is reported as a heartbeat.
- **Not claimed:** archives on NFS storage are not verified and no restore is
  tested, so Ops shows no verification for these jobs.

### Anything else (curl)

```sh
curl -fsS -X PUT --data-binary @report.json \
  http://192.168.1.5:18080/api/v1/ops/jobs/JOB/runs/RUN
```

Required fields are `state` and `started_at`, plus `finished_at` once the run is
over. Resending the same body is always safe.

## The den-agents collector

`den-tool ops collect` reports evidence that jobs already leave behind. It
never starts, retries or stops a job. On den-agents,
`den-ops-collect.timer` runs it five minutes after the last run finished,
with the checked-in config `deployment/ops-collect/den-agents.json`
(installed to `~/.config/den-ops-collect/config.json`).

Each collection does this:

1. **Registers** the configured hosts and jobs, every time. A job that never
   reports still shows as `never_reported`, and a replaced Ops ledger gets its
   expectations back within five minutes. Run history comes back with
   `--resend`.
2. **Reads every source.** One source failing (say, den-nas unreachable) is
   printed as a warning, and the others still report.
3. **Sends only evidence that changed.** A cache (`~/.local/state/den-tool/ops-collect-cache.json`)
   remembers every body already delivered or spooled.
4. **Spools during an outage.** If Ops is unreachable, the rest of the
   collection goes to the spool and the next collection replays it first.

| Source | Reads | Reports |
|---|---|---|
| `borg-runner` | `~/.local/state/den-agents-backup`: `runs/*/result.json`, `latest.json`, restore-test files | `den-agents-git` runs, heartbeats, restore tests. Maintenance runs go to `den-agents-git-maintenance`; their repository check is also recorded on `den-agents-git` |
| `thin-config` | `~/.local/state/den-config-backup`: `reports/*.json`, `maintenance.json`, `latest.json`, `mirror-verification.json` | One run per host capture, with its local extract-and-check as the restore test; the local repository check on each host job; the NAS copy as `host-configs-nas-copy` runs (a busy NAS lock is a warning) with its own repository check and NAS restore test |
| `snapraid` | den-nas journal for `snapraid-daily` and `snapraid-weekly` | `den-nas-snapraid` maintenance runs (one per systemd invocation, exit code and error tail on failure; nonzero SnapRAID error counts as notices); the weekly scrub as a verification |
| `proxmox-vzdump` | den-prox-mini and den-prox, over the thin backup's SSH bundles | Guest hosts, per-guest vzdump jobs and runs (see above) |

The collector looks back 14 days, so a fresh ledger fills with that history.
SnapRAID rescans 14 days only when the collector has not run for a day;
otherwise it reads the last 3. Proxmox task logs are fetched once per finished
task.

```sh
systemctl status den-ops-collect.timer den-ops-collect.service
journalctl -u den-ops-collect.service -n 20
den-tool ops collect --dry-run   # print what would be sent, send nothing
den-tool ops collect --resend    # send everything again, e.g. after an Ops ledger restore
scripts/install-ops-collect.sh   # (re)install den-tool, the config and the timer
```

Stop it with `sudo systemctl disable --now den-ops-collect.timer`; no backup
job depends on it.

## 3. Read status

```sh
den-tool ops status            # every job; overall status first
den-tool ops job den-agents-git
den-tool ops errors            # jobs that are not ok, recent failures and warnings
den-tool ops status --json     # the API response, unchanged
```

The den-web Operations page (task 10445) reads the same `/api/v1/ops` endpoints.
