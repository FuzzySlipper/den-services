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

To hook this into systemd without touching the service itself, add a drop-in
such as `ExecStopPost=-/usr/local/bin/den-tool ops report %h/.local/state/den-agents-backup/latest.json`:

- `ExecStopPost` runs whatever the backup's outcome was.
- The leading `-` keeps a reporting failure from changing the unit's result.
- Undelivered reports wait in the spool.

Wiring this up for den-agents is task 10446.

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

### Proxmox VM/CT backups (collector)

Proxmox runs vzdump on its own schedule, so Ops gets a *collector*, not a
wrapper. A small script on the Proxmox host (or anywhere with its SSH bundle)
reads the finished tasks and reports each one:

```sh
pvesh get /nodes/$(hostname)/tasks --typefilter vzdump --limit 50 --output-format json
```

For each finished task, send one report:

| Report field | Value |
|---|---|
| `job_id` | Stable per backup job or guest, e.g. `prox-mini-vzdump-106` |
| `run_id` | The task's `upid`, which is stable, so collecting the same task twice is harmless |
| `started_at` / `finished_at` | The task's `starttime` / `endtime` (epoch → RFC 3339 UTC) |
| `state` | From `status`: `OK` → `success`, a status containing `WARNINGS` → `warning`, anything else → `failed` |
| `error` / `warnings` | The status text |

Two rules for the collector:
- Archive inventory (`pvesh get /nodes/NODE/storage/STORE/content --content backup`)
  can fill in `archive_name` and size.
- A configured job with no task is *not* a backup. Leave it to Ops to show it as
  `never_reported` or `overdue`.

Building these collectors is task 10446.

### Anything else (curl)

```sh
curl -fsS -X PUT --data-binary @report.json \
  http://192.168.1.5:18080/api/v1/ops/jobs/JOB/runs/RUN
```

Required fields are `state` and `started_at`, plus `finished_at` once the run is
over. Resending the same body is always safe.

## 3. Read status

```sh
den-tool ops status            # every job; overall status first
den-tool ops job den-agents-git
den-tool ops errors            # jobs that are not ok, recent failures and warnings
den-tool ops status --json     # the API response, unchanged
```

The den-web Operations page (task 10445) reads the same `/api/v1/ops` endpoints.
