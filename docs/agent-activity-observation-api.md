# Agent Activity Observation API

Status: backend contract for task #2810.

Observation is the display-only breadcrumb lane for agent activity. It stores compact activity projections for UI readback; it does not create, claim, wake, retry, or complete executable work.

## Endpoints

Trusted adapters write activity events to either endpoint:

```text
POST /v1/observation/activity-events
POST /v1/observation/lifecycle-events
```

`/v1/observation/activity-events` is the preferred green path for new Hermes and pi-crew producers. `/v1/observation/lifecycle-events` remains supported for existing callers and uses the same request contract.

Den Web and other read clients use:

```text
GET /v1/observation/lane
GET /v1/observation/agents/{id}/overview
GET /v1/observation/active-work
```

`agent-overview` includes `activity_events` for the requested canonical profile or concrete instance.

## Write Request

```json
{
  "source_domain": "runtime",
  "event_type": "work_checkpoint",
  "agent_identity": {
    "profile": "pi-crew-runner",
    "instance_id": "pi-crew-runner@pool-1"
  },
  "runtime_instance_id": "pi-crew-runner@pool-1",
  "payload": {
    "kind": "agent_activity.v1",
    "schema_version": 1,
    "summary": "Checkpointed task 2810.",
    "severity": "info",
    "visibility": "task",
    "adapter": "pi-crew",
    "surface": "worker",
    "work_ref": {
      "project_id": "den-services",
      "task_id": 2810,
      "assignment_id": "42",
      "run_id": "run-abc"
    }
  }
}
```

All accepted activity writes are stored with `display_only=true`.

## Payload Envelope

Required for every `agent_activity.v1` payload:

- `kind: "agent_activity.v1"`
- `schema_version: 1`
- `summary`, 1-240 characters
- `severity`: `info`, `success`, `warning`, or `error`
- `visibility`: `channel`, `task`, `agent`, or `debug`
- `adapter`: `hermes`, `pi-crew`, `den-services`, `den-channels`, or `den-web`
- `surface`: `channel`, `task`, `worker`, `review`, `direct-debug`, `gateway`, `runtime`, or `observation`

Known event types also require their event-specific fields:

- `agent_session_started`, `agent_session_resumed`, `agent_session_idle`, `agent_session_stopped`: `session_key`
- `agent_session_blocked`, `agent_session_failed`: `session_key`, `reason_code`
- `work_started`, `work_checkpoint`: `work_ref`
- `work_waiting`, `work_failed`: `work_ref`, `reason_code`
- `work_completed`: `work_ref`, `result_ref`
- `model_turn_started`, `model_turn_completed`: `session_key`
- `tool_call_started`: `tool_name`
- `tool_call_completed`: `tool_name`, `result_ref`
- `tool_call_failed`: `tool_name`, `reason_code`
- `adapter_disconnected`, `adapter_degraded`: `reason_code`

Unknown future `event_type` values are accepted when the common envelope is valid. Den Web should render them generically from `payload.summary`.

## Hermes Example

```json
{
  "source_domain": "runtime",
  "event_type": "agent_session_started",
  "agent_identity": {
    "profile": "den-mcp-runner",
    "instance_id": "den-mcp-runner@hermes-1"
  },
  "runtime_instance_id": "den-mcp-runner@hermes-1",
  "payload": {
    "kind": "agent_activity.v1",
    "schema_version": 1,
    "summary": "Hermes runner session started.",
    "severity": "info",
    "visibility": "agent",
    "adapter": "hermes",
    "surface": "worker",
    "session_key": "hermes-session-1"
  }
}
```

## pi-crew Example

```json
{
  "source_domain": "runtime",
  "event_type": "work_completed",
  "agent_identity": {
    "profile": "pi-crew-reviewer-worker",
    "instance_id": "pi-crew-reviewer-worker@pool-1"
  },
  "payload": {
    "kind": "agent_activity.v1",
    "schema_version": 1,
    "summary": "pi-crew reviewer completed task 2810 review.",
    "severity": "success",
    "visibility": "task",
    "adapter": "pi-crew",
    "surface": "review",
    "work_ref": {
      "project_id": "den-services",
      "task_id": 2810,
      "assignment_id": "42",
      "run_id": "run-pi-crew-1"
    },
    "result_ref": {
      "message_id": 15743
    }
  }
}
```

## Gateway And Auth Shape

Observation itself remains loopback-bound on den-srv. Browser clients should not call it directly.

Expected Gateway posture:

- Den Web read access: authenticated Gateway route to `GET /v1/observation/lane`, `GET /v1/observation/agents/{id}/overview`, and `GET /v1/observation/active-work`.
- Trusted adapter writes: authenticated Gateway route to `POST /v1/observation/activity-events` and `POST /v1/observation/lifecycle-events`.
- Read and write routes should be separable in policy. Do not expose trusted adapter writes as browser-callable routes.
- Gateway should forward to Observation with an Observation-accepted upstream credential, not by leaking the browser or adapter inbound token.

Route implementation is intentionally deferred from #2810. Track it as a Gateway/auth follow-up before Den Web or adapters depend on LAN-facing observation routes.

## Non-Actuation Rule

Observation events are append-only display evidence. They may carry `work_ref`, `result_ref`, delivery IDs, runtime IDs, or channel IDs, but they are not commands. Reading lane or overview endpoints must not create delivery intents, claim work, wake agents, or mutate delivery/runtime state.
