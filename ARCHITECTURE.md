# Den Services - Architecture Guidelines

Source: Den document `den-services/architecture-guidelines`.

This file is a repo-local snapshot for review convenience. The Den document is
authoritative if this copy ever diverges.

## Purpose

Den successor services are built to replace haunted legacy responsibilities
with small, typed, domain-owned Go modules. They should not port legacy
behavior casually, and they should not recreate den-channels as a Go monolith.

## Three-lane Invariant

Every record belongs to exactly one lane:

| Lane | Replay rule | Owner |
| --- | --- | --- |
| Conversation | Old rows display forever | Conversation service |
| Observation | Old rows display but never cause action | Observation service |
| Executable actuation | Old rows must not execute solely because they replay | Delivery service |

No single physical row may serve more than one lane. Conversation, observation,
and executable actuation have different schemas, writer roles, and modules.

## Service Boundaries

Split services by domain authority, schema ownership, state-machine ownership,
and failure domain. Do not split by handler count alone.

- One module owns exactly one domain schema.
- One module owns at most one tightly coupled state machine.
- Multiple `cmd/` binaries may live in a module when they share a schema and
  state machine.
- Services deploy at the module boundary.
- A service `internal/` package should stay small enough to review in one
  working session.

## Schema Ownership

All successors use one Postgres cluster with role-isolated schemas.

| Schema | Owner module | App role |
| --- | --- | --- |
| `den_delivery` | `delivery/` | `den_delivery_app` |
| `den_runtime` | `runtime/` | `den_runtime_app` |
| `den_observation` | `observation/` | `den_observation_app` |
| `den_channels` | conversation or slim legacy | `den_channels_app` |

Runtime services never run with migration credentials. Cross-schema writes are
forbidden. Cross-schema reads use explicit named views and grants.

## Monorepo Structure

```text
den-services/
  go.work
  shared/
  delivery/
  runtime/
  observation/
  gateway/
  conversation/
  migration/
  integration/
  Makefile
  ARCHITECTURE.md
  CODESTYLE.md
```

`shared/` is for infrastructure only: config loading, health helpers, Postgres
pool plumbing, API envelopes, auth primitives, identity types, idempotency
helpers, and logging primitives. It must not accumulate domain types or service
clients with business logic.

## Cross-Service Communication

Writes go through the owning service API synchronously, with typed requests and
idempotency keys. Reads go through named Postgres views. Projection views cannot
drive execution; haunt-regression tests enforce that read models do not become
actuation paths.

## Gateway Rules

The gateway is a router, not a domain service.

- No domain schema.
- No durable domain state.
- No lifecycle transitions.
- No silent fallback writes.
- Routing configuration is versioned and smoke-tested.

The first gateway milestone is transparent pass-through to legacy services with
zero behavior change. Identity-aware routing comes after pass-through is proven.

## Identity

Successor services use the canonical identity model:

| Level | Type | Field |
| --- | --- | --- |
| Logical | `ProfileIdentity` | `profile` |
| Runtime instance | `AgentInstanceID` | `instance_id` |
| Session | `SessionKey` | `session_key` |

Legacy identity columns are mapped at explicit boundaries and do not propagate
into successor schemas.

## Delivery Lifecycle

Delivery owns executable actuation. Claims are atomic and terminal states are
final.

```text
pending -> claimed -> running -> completed
                         running -> failed
pending -> expired
pending -> cancelled
```

Claim operations must use compare-and-swap SQL. Runtime liveness is checked
before claim. Reapers never touch terminal intents. Imported legacy wake rows
are display-only or terminal and cannot execute.

## Haunt-Regression Tests

Services must prove the motivating failure modes do not recur:

- display replay never wakes;
- observations display but never execute;
- projection reads do not create or claim work;
- terminal delivery intents cannot execute again;
- stale runtimes cannot claim fresh work;
- duplicate claims are rejected atomically;
- stale cursors replay history without inducing action;
- cutover watermarks keep legacy rows display-only.

## Deployment Posture

Go services deploy as static binaries on den-srv under systemd. Docker is not
the default. Build and install are separate phases: agents can build and stage
without privileges; install handles privileged filesystem and systemd actions.

## What This Architecture Avoids

- Same row serving multiple lanes.
- Runtime schema reconcilers and dual migration systems.
- Cross-schema raw-table reads.
- Gateway domain logic.
- Shared domain type assemblies.
- Premature event bus or outbox systems.
- `shared/` becoming a domain junk drawer.
