# Resources API contract

The Resources service records what is using the workshop machines. It ported
the asset-pipeline Resource Board (task 10487). It keeps:

- **hosts:** registrations and each host's latest machine telemetry and app
  probes;
- **directory:** the shared installations, model trees, caches and tools,
  with their sharing notes and lifecycle;
- **work ledger:** advisory check-ins and job reports.

Claims are advisory. The service never starts, stops or schedules anything,
and it never polls. `den-tool resources collect` on den-agents probes the
hosts and pushes what it finds (see [resources-operations.md](resources-operations.md)).

| Where | URL |
|---|---|
| LAN (web-edge → Gateway) | `http://192.168.1.5:18080/api/v1/resources/...` |
| CT106 Gateway | `http://127.0.0.1:8079/v1/resources/...` |
| Service | `http://127.0.0.1:8103/v1/resources/...` |

Conventions:
- Bodies are JSON. Unknown fields are rejected, except inside `telemetry`.
- Times are RFC 3339 UTC strings, and a time that was never recorded is `null`.
- Errors use the den-services envelope `{"error":{"code","message"}}`. Codes:
  - 400 `validation_failed`;
  - 404 `host_not_found`, `resource_not_found`, `work_not_found`.
- IDs are 1–128 characters of letters, digits and `._:@-`, starting with a
  letter or digit. A work row's `id` is assigned by the service.

## Reading

### `GET /v1/resources/state`

Everything a dashboard needs in one read. `?work_limit=N` (default 250, max
1000) caps `work`. Rows that are queued or running are always included,
whatever the limit. Older finished rows are only in `GET /v1/resources/work`.

The `counts` fields are as follows:
- `hosts_fresh` counts hosts that are neither stale nor in error.
- `running` counts running reports whose heartbeat has not expired.
- `native_queued` totals the queued jobs in fresh, online app probes.

```json
{
  "generated_at": "2026-10-29T22:00:00Z",
  "poll_seconds": 20,
  "stale_after_seconds": 90,
  "counts": {"hosts": 6, "hosts_fresh": 5, "running": 1, "native_queued": 0, "resources": 39},
  "hosts": [Host],
  "resources": [Resource],
  "work": [Work]
}
```

**Host**:

```json
{
  "id": "den-m5", "name": "den-m5", "role": "Strix Halo · ComfyUI, training",
  "access": "ssh den-m5", "note": "…", "position": 2,
  "status": "working",
  "stale": false,
  "error": null,
  "attempted_at": "…", "sampled_at": "…", "received_at": "…",
  "telemetry": { …probe snapshot, see below… } ,
  "apps": [App], "apps_checked_at": "…"
}
```

- `status` is one of the following:
  - `unknown`: the telemetry is stale, missing or failed;
  - `working`: CPU is over 65%, a GPU is over 10%, a fresh native queue is
    running, or a reported job is running;
  - `queued`: an active report or a fresh native queue is waiting;
  - `quiet`: none of the above.
- `quiet` is one sample. It does not promise the machine is free.
- `stale` is true when `sampled_at` is older than `stale_after_seconds`.
- `error` is the last collection failure, or `null`. The last good
  `telemetry` stays visible beside it.
- Hosts are ordered by `position`, then `id`.

**App** (one probe of a native queue or HTTP health):

```json
{"name": "ComfyUI", "kind": "comfy", "url": "http://192.168.1.24:8188",
 "resources": ["m5-comfy"], "online": true, "running": 1, "queued": 0,
 "jobs": [{"id": "…", "state": "running", "node_types": ["KSampler"]}],
 "error": null, "checked_at": "…", "stale": false}
```

`kind:"health"` apps have `running`/`queued` `null`: health is known, the
queue is not.

**Telemetry**: the probe snapshot is passed through unchanged (the schema of
`probe.py`/`probe-windows.ps1`):

- `sampled_at`: Unix seconds;
- `platform`, `cpu_count`, `cpu_percent`, `load_average`;
- `memory`: `total_bytes`, `available_bytes`, `swap_total_bytes`,
  `swap_free_bytes`;
- `gpus[]`: `name`, `utilization_percent`, `memory_total_bytes`,
  `memory_used_bytes`, `gtt_used_bytes`, `memory_note`;
- `processes[]`: `pid`, `command`, `cwd`, `cpu_percent`, `rss_bytes`;
- `containers[]`: `name`, `image`, `state`, `status`, `cpu`, `memory`,
  `socket`;
- `disks[]`: `path`, `total_bytes`, `free_bytes`;
- `paths`: `{path: bool}`;
- `warnings[]`.

Any metric may be `null` for unknown.

**Resource**:

```json
{
  "id": "m5-comfy", "host": "den-m5", "name": "ComfyUI · strix-beast",
  "kind": "installation", "path": "/home/agent/comfyui-strix-docker",
  "probe_path": "", "runtime_path": "strix-beast:/app", "access": "http://…",
  "sharing": "…", "lifecycle": "maintained", "notes": "…", "guide": "docs/…",
  "match": "",
  "updated_at": "…",
  "last_used_at": "…", "last_used_by": "agent-x",
  "observed_activity_at": "…",
  "confirmed_at": "…", "confirmed_by": "…",
  "path_present": true, "path_checked_at": "…"
}
```

- `lifecycle` is one of `maintained`, `experimental`, `retired` or `unknown`.
- The four dates mean different things:
  - `last_used_at` is recorded use (a check-in naming the resource, or
    `touch`);
  - `observed_activity_at` is a matching process or a running native queue;
  - `confirmed_at` is an explicit lifecycle confirmation;
  - `path_checked_at` is when the path was last checked for presence only.
- `path_present` is `null` until the probe has checked the path.

**Work**:

```json
{
  "id": "6f0c…", "host": "den-m5", "source": "command", "external_id": "…",
  "title": "Generate material candidates", "owner": "my-agent",
  "project": "rusty-craftsurvive", "resources": ["m5-comfy"],
  "state": "running", "note": "", "url": "",
  "ttl_seconds": 180,
  "created_at": "…", "started_at": "…", "updated_at": "…",
  "expires_at": "…", "ended_at": null,
  "stale": false
}
```

- `state` is `queued`, `running`, `completed`, `failed` or `cancelled`.
- `stale` means the row is queued or running but its heartbeat expired.
  Stale never means free or abandoned.

### Other reads

| Route | Returns |
|---|---|
| `GET /v1/resources/directory` | `{"resources": [Resource]}` ordered by host, then id |
| `GET /v1/resources/directory/{id}` | `Resource` |
| `GET /v1/resources/work` | `{"work": [Work]}`, newest report first. `?active=true` gives queued and running only, `?source=` and `?host=` filter, and `?limit=` defaults to 250 (max 1000) |
| `GET /v1/resources/work/{id}` | `Work` |

## Writing

### `POST /v1/resources/work`: report or check in

```json
{"host": "den-m5", "title": "Generate", "owner": "my-agent", "project": "",
 "resources": ["m5-comfy"], "state": "running", "ttl_seconds": 180,
 "source": "check-in", "external_id": null, "note": "", "url": "",
 "observed_at": null}
```

- `host` must be a registered host, and each resource must belong to that
  host.
- `state` defaults to `running`, and `ttl_seconds` defaults to 180 and must
  be positive.
- `source` + `host` + `external_id` name one job, and a repeated report
  updates that row. Without `external_id`, every report creates a new row.
- `observed_at` (default now) is when the reporter saw the state. If a report
  is older than the row's `updated_at`, or reports an active state for a row
  that already finished, the row is returned unchanged. A delayed heartbeat
  never revives finished work.
- Reports that are running or terminal (but not queued, and not a cancel
  that never started) set `last_used_at`/`last_used_by` on the named
  resources.
- The response is the `Work` row plus `"warnings": [...]`, which lists other
  unexpired active reports on the same host. Overlaps warn and continue.

### `POST /v1/resources/work/{id}`: heartbeat or check out

`{}` renews the row. `{"state": "completed"}` (or failed/cancelled) checks it
out. `note` replaces the note, and `observed_at` is optional. A finished row
is returned unchanged.

### `POST /v1/resources/directory`: register or update a resource

- The body takes any of the resource fields (`id` is required), plus:
  - `"touch": true`, which records use;
  - `"confirm": true`, which confirms the lifecycle;
  - `"owner"`, the name recorded for either.
- The update merges: fields that are left out keep their values, and usage
  dates are never overwritten.
- A new resource needs `host`, `name` and `path`. Sending an existing
  resource's own `host` is accepted, but a different `host` is refused: it
  cannot move to another machine.

### Host collection (den-tool resources collect)

| Route | Body |
|---|---|
| `PUT /v1/resources/hosts/{id}` | `{"name","role","access","note","position"}`. Registers or updates a host, and keeps its telemetry |
| `POST /v1/resources/hosts/{id}/telemetry` | `{"telemetry": {…}}` or `{"error": "…"}`. A snapshot also updates `path_present` and observed activity for that host's resources |
| `POST /v1/resources/hosts/{id}/apps` | `{"apps": [App without checked_at/stale]}`. Running comfy apps mark their `resources` as observed |
| `PUT /v1/resources/settings` | `{"poll_seconds": 20}`. Lets the collector publish its cadence for display |

Telemetry is checked before it is saved:
- the listed metrics must be finite numbers or `null`, with no units;
- `gpus`, `processes`, `disks` and `containers` must be arrays of objects.

A malformed snapshot is rejected and does not replace the last good one.
`sampled_at` is clamped to the receive time, so an old snapshot that is
pushed again stays stale.
