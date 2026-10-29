# Ops API contract (v1)

Ops is Den's central record of backup and maintenance status. Machines keep
running their own jobs with systemd timers, Task Scheduler or Proxmox. Each job
reports what happened, and Ops stores those reports and judges freshness. Ops
never starts, retries or wakes anything, and a stored report is only ever read.

- **Source:** `ops/` in den-services (Handler → Service → Store).
- **Storage:** the service's own SQLite file (`database_path`, deployed at
  `/data/services/ops/data/ops.sqlite3`). The owner chose SQLite deliberately
  (campaign 10442). That is an exception to the usual Postgres posture: Ops has
  no Postgres or NAS dependency for startup or status reads.
- **Service address:** `127.0.0.1:8102`. Gateway and web-edge expose the same
  paths under `/v1/ops` and `/api/v1/ops` respectively.
- **Auth:** none, because Den runs on a trusted LAN.
- **Data format:** JSON with snake_case fields. Timestamps are UTC RFC 3339; UIs
  convert them to local time.
- **Errors:** use the standard envelope
  `{"error":{"code":"...","message":"..."}}`. Codes include
  `validation_failed` and `bad_request` (400) and `job_not_found` and
  `run_not_found` (404).

## Concepts

- **Host:** a machine, for example `den-agents`, `den-nas` or `den-win11`. A
  registered host with no enabled job has `coverage: "not_configured"` and
  status `unknown`. It is never shown as healthy.
- **Job:** an *expectation*, identified by a stable `job_id` such as
  `den-agents-git` or `den-nas-config`. Register it before its first report so
  a job that never runs stays visible. A report for an unregistered job is
  still kept, under an implicit job with `registered: false` and no schedule.
- **Run:** one execution of a job, identified by a stable `run_id`. Reports are
  idempotent on `(job_id, run_id)`.
- **Verification:** a repository check (`kind: "verification"`, with
  `check_type`) or a restore test (`kind: "restore_test"`). Both are kept
  separate from backup runs.

## Run lifecycle rules

1. **A report is terminal only when it has `finished_at`.** A report without
   it is a heartbeat whatever its `state` says, and is stored as `running`. The
   Borg runner, for example, reports `success` while it is still pruning.
2. **`state`** is one of `running`, `success`, `warning`, `failed` or
   `skipped`. A report with `finished_at` must have one of the terminal states.
3. **Resending the same report** (same `updated_at` and terminal flag) returns
   `outcome: "duplicate"` and changes nothing.
4. **A late heartbeat cannot reopen a finished run**
   (`outcome: "ignored_terminal"`).
5. **An older copy never overwrites a newer one** (`outcome: "ignored_stale"`).
   This makes replaying a local spool safe in any order.
6. **A newer terminal report for a finished run is accepted as a correction**
   (`outcome: "updated"`).
7. **`complete`** says whether the run produced a usable restore point:
   - It is false for `failed`, `skipped` and `running`.
   - For `success` or `warning` it is `backup_complete` when the report sends
     it, and otherwise `state == "success"`.
   - A Borg warning that only affected retention keeps `backup_complete: true`.
8. **A failure never hides the last restore point.** `last_success` (the
   newest complete run) and `last_archive` (the newest run that reported an
   archive ID) are separate from `last_attempt`.

## Job status

Every job has these fields, computed when the response is generated:

| Field | Meaning |
|---|---|
| `status` | Worst first: `unknown`, `failed`, `overdue`, `never_reported`, `warning`, `running`, `ok`, `disabled` |
| `status_reason` | One sentence explaining the status |
| `freshness` | `fresh`, `overdue` or `unscheduled` |
| `expected_by` | Deadline for the most recent schedule slot whose grace period has ended |
| `next_due_at` | The next scheduled start |
| `current_run` | The newest run if it hasn't finished. `heartbeat_state` is `fresh`/`stale`, and `heartbeat_age_seconds` is how long since its last update |
| `last_attempt` | The newest run of any state, including `skipped` (shows skipped mornings) |
| `last_finished` | The newest terminal run that wasn't skipped |
| `last_success` | The newest complete run: the last good restore point |
| `last_archive` | The newest run with an archive ID, whatever its state |
| `last_verification` | The newest repository check |
| `last_restore_test` | The newest restore test |

How each status is decided:

| Status | When |
|---|---|
| `never_reported` | No report has ever arrived. Absence is never success |
| `unknown` | A running run's last heartbeat is older than its `heartbeat_stale_after` (the job's value, else the config default). Lost reporting is treated as unknown, not as healthy |
| `unknown` | Only skipped runs have been reported |
| `failed` | The newest finished, non-skipped run failed |
| `overdue` | No non-skipped run started for the latest slot once its grace period has ended. A run counts for a slot if it started after the previous slot's grace period ended, so an early manual run counts for the next morning |
| `warning` | The newest finished run had warnings. This is distinct from both success and failure |

## Endpoints

### Status (reads)

| Method | Path | Response |
|---|---|---|
| GET | `/v1/ops/summary` | `{generated_at, status, counts{status→n}, hosts[], jobs[]}`. The overall `status` is the worst of the enabled jobs, or `unknown` when there are none |
| GET | `/v1/ops/jobs` | `{generated_at, jobs[]}` |
| GET | `/v1/ops/jobs/{job_id}?limit=` | `{job, runs[], verifications[]}`, newest first |
| GET | `/v1/ops/jobs/{job_id}/runs?limit=` | `{runs[]}` |
| GET | `/v1/ops/jobs/{job_id}/runs/{run_id}` | `{run, report}`. `report` is the newest stored body exactly as it was sent |
| GET | `/v1/ops/jobs/{job_id}/verifications?limit=` | `{verifications[]}` |
| GET | `/v1/ops/hosts` | `{hosts[]}` with `coverage`, `job_count` and `status` |
| GET | `/v1/ops/errors?limit=` | `{generated_at, problems[], problem_runs[], failed_verifications[]}`. `problems` lists the jobs that aren't `ok`, `running` or `disabled`; `problem_runs` lists recent failed or warning runs |

`limit` is between 1 and 500. When it's absent, the configured default is used.

### Registration

`PUT /v1/ops/hosts/{host_id}` takes `{display_name, role, notes}`.

`PUT /v1/ops/jobs/{job_id}` is an upsert:

```json
{
  "host_id": "den-agents",
  "display_name": "den-agents Git projects",
  "engine": "borg",
  "kind": "backup",
  "source": "den-agents:/home/agent,/data,/opt,/usr/local/src",
  "scope": "Git repositories, working trees, LFS stores",
  "target": "den-nas:/mnt/storage/backup/den-agents/borg",
  "schedule": {"timezone": "America/Los_Angeles", "times": ["07:00"], "weekdays": [], "grace": "3h"},
  "heartbeat_stale_after": "15m",
  "retention": "30 daily, 12 weekly, 12 monthly, 3 yearly",
  "recovery_pointer": "den-nas:/mnt/storage/backup/den-agents/recovery/",
  "notes": "",
  "enabled": true
}
```

- **Required:** `host_id`, `engine` and `kind`. All other fields are optional.
- **`schedule`:** omit it for jobs that only run manually.
- **`weekdays`:** an empty list means every day; otherwise use `sun` … `sat`.
- **`grace`:** how long after a scheduled start a run may begin before the job
  counts as overdue.

### Reporting a run

`PUT /v1/ops/jobs/{job_id}/runs/{run_id}` takes a run report. `POST
/v1/ops/reports` does the same but reads `job_id` and `run_id` from the body.

Report bodies are decoded leniently: fields Ops doesn't model are accepted and
kept verbatim in the stored `report`. **The den-agents Borg report files are
valid bodies as they are** (`latest.json`, `last-success.json` and
`runs/RUN_ID/result.json`):

```sh
curl -fsS -X POST --data-binary @"$HOME/.local/state/den-agents-backup/latest.json" \
  http://192.168.1.5:18080/api/v1/ops/reports
```

These are the fields Ops reads; the names match the Borg runner's schema
version 1:

| Field | Notes |
|---|---|
| `job_id`, `run_id` | Must match the path if present |
| `host`, `engine`, `runner`, `kind`, `phase`, `target` | Free text |
| `state` | Required |
| `started_at` | Required |
| `updated_at` | Optional. Orders heartbeats; defaults to `finished_at`, then `started_at` |
| `finished_at` | Makes the report terminal |
| `exit_code` | The engine or runner exit status, recorded unchanged |
| `error`, `reason` | `reason` explains a skip |
| `warnings[]`, `source_notices[]` | Lists of text |
| `archive` | `{id, name, duration, stats}` |
| `stats` | `{original_size, compressed_size, deduplicated_size, nfiles}`. Falls back to `archive.stats` |
| `duration_seconds` | Falls back to the run's wall time (`finished_at - started_at`), then `archive.duration` |
| `backup_complete` | See lifecycle rule 7 |
| `log_path`, `manifest`, `local_run_directory`, `recovery_pointer` | Pointers to logs, inventory and recovery material |
| `verification`, `restore_test` | Embedded checks, stored as verifications `RUN_ID:verification` and `RUN_ID:restore_test` |

The response is `{outcome, run, job}`: `outcome` is one of `created`,
`updated`, `duplicate`, `ignored_terminal` or `ignored_stale`, and `job` is the
job's recomputed status. The status code is 200 for every outcome, so a
reporter that retries never has to treat a repeat as an error.

### Reporting a verification or restore test

`PUT /v1/ops/jobs/{job_id}/verifications/{verification_id}` registers a check
on its own:

```json
{"kind": "restore_test", "state": "success", "at": "2026-10-29T04:30:10Z",
 "archive": "den-agents-git-2026-10-29T04-24-43", "scope": "isolated fixture ...",
 "restored_to": "/home/agent/.local/state/den-agents-backup/restore-results/...",
 "files_compared": ["main/source.txt"]}
```

| Field | Notes |
|---|---|
| `kind` | `restore_test`, `verification`, or a check name such as `repository-and-latest-archive`, which means a verification of that `check_type` |
| `state` | Required: `success`, `warning` or `failed` |
| `at` | Required |
| Other fields | `type`, `run_id`, `scope`, `archive`, `full_data_read`, `evidence_path`/`restored_to`, `checked[]`, `repositories_checked[]`, `files_compared[]` |

The job must already exist. A repeated `at` and `state` is a `duplicate`, and an
older `at` is `ignored_stale`. The restore-test files the den-agents runner
writes (`restore-test.json`, `real-project-restore.json`) are valid bodies as
they are.

## Reporter guidance

These rules apply to the reporters built in tasks 10444 and 10446:

- **Run IDs:** use the job's own stable `run_id` and send the same body again
  on retry. If Ops can't be reached, spool the report locally and replay it
  later; duplicates and replays out of order are harmless.
- **Exit status:** a reporter must never change the backup's exit status or
  stop a backup because Ops is down.
- **Heartbeats:** send a report without `finished_at` while a long run is in
  progress, then one final report with `finished_at`.
- **Missing evidence:** report nothing rather than inventing a success. Ops
  already shows silence as `never_reported`, `overdue` or `unknown`.
