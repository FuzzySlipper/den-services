# den-services project guidance

## Purpose

`den-services` is the clean Go successor-services program for Den's local service layer. It exists to replace haunted legacy responsibilities through strangler-fig migration, not to copy legacy complexity into a new language.

Current durable planning truth lives in Den documents, especially:

- `den-services/architecture-guidelines`
- `den-services/go-codestyle`
- `den-services/successor-service-registry`

Treat those Den docs as authoritative over stale local notes. This file is a repo bootstrap so fresh agents know what to read and how to behave before the repo has much code.

## Architecture rules

1. **Three-lane invariant**
   - Conversation rows display to humans and never execute.
   - Observation rows are breadcrumbs/projections and never execute.
   - Executable actuation rows have a fresh claim lifecycle and terminal finality.
   - No physical row may serve more than one lane.

2. **Authority boundaries are enforced, not conventional**
   - Each domain owns its schema and runtime writer role.
   - Cross-domain writes go through the owning service API.
   - Cross-domain reads use approved views/contracts.
   - A service must not write another service's schema.

3. **Successor codebase, not legacy surgery**
   - Legacy services continue running while successors are built cleanly beside them.
   - Gateway cutover moves one function at a time.
   - Legacy data is display-only unless explicitly validated and promoted through a cutover path.
   - Do not bulk-import haunted tables and call the migration complete.

4. **Gateway stays boring**
   - The gateway routes and authenticates.
   - It owns no domain schema and performs no lifecycle transitions.
   - Runtime routing config should be reviewed config/artifacts, not an ad hoc mutable planning doc.

## Go style rules

Use Go with C#-like structural discipline:

- explicit structs for domain/request/response concepts;
- constructor injection for dependencies;
- clear Handler → Service → Store layering;
- no SQL outside store code;
- no business logic in handlers;
- no HTTP concerns in service/store logic;
- no package-level mutable state;
- no `init()` side effects;
- no `panic`/`log.Fatal` outside initialization/`main.go`;
- `context.Context` first for I/O methods;
- typed constants for enum-like values with validation at boundaries;
- narrow consumer-defined interfaces only where a seam is useful;
- DTO/domain separation when response shape, secrecy, or lifecycle differs;
- no hardcoded configuration values — every tunable (ports, timeouts, TTLs, thresholds) lives in a typed `Config` struct loaded from a YAML file; config files ship with documented `config.example.yaml`; env vars are for secrets and the config file path only.

Avoid clever Go:

- no `map[string]any` domain bags;
- no reflection-driven domain behavior;
- no goroutines launched from handlers without explicit lifecycle ownership;
- no dynamic JSON blobs crossing service boundaries unless the task explicitly defines that contract;
- no giant god stores/services/handlers.

## Expected repo shape

The target shape is a Go workspace with separate modules/domains, roughly:

```text
den-services/
  go.work
  shared/
  migration/
  gateway/
  delivery/
  runtime/
  observation/
  integration/
```

This repo may start smaller than that. The scaffold task (#2666) creates the full directory tree with `go.mod` files pre-emptively; individual module tasks fill in their `cmd/` and `internal/` code when assigned. Do not manually create extra module directories beyond what the scaffold task produces unless a Den task explicitly calls for it.

## First implementation wave preference

Unless a task says otherwise, prefer this sequence:

**Wave 0 — Substrate (blocks everything).**
1. Scaffold monorepo structure (`go.work`, all module directories with go.mod, Makefile, `.golangci.yml`).
2. Build `shared/` infrastructure module (config, health, postgres, api, identity, idempotency, logging).
3. Build `migration/` module and initial schema files.
4. Stand up Postgres schemas, roles, and grants on den-srv.

**Wave 1 — First module deployments (parallel).**
5. Gateway Phase 1: transparent pass-through proxy (routes to legacy only).
6. Runtime module: presence + subscriptions (parallel track; deploys after gateway, before delivery).
7. Delivery module: intent lifecycle — ingest, claim, lifecycle, reaper (parallel build using mock runtime client, deploys after runtime is live).
8. Gateway Phase 2: identity translation layer (unlocks first cutover).
9. Repoint all callers (den-host, agents, den-web, den-mcp) to the gateway (Wave 1 exit gate).

**First cutover.**
10. Flip delivery intent routes from legacy to successor (hard flip, no dual-write).

**Wave 2+.**
11. Observation composed lane + lifecycle writer after delivery/runtime are authoritative.

This matches the dependency graph in `successor-service-registry` §8. See Den tasks for per-module acceptance criteria.

## Testing and closeout

For code tasks, include evidence appropriate to the change:

- `gofumpt`/`go test ./...` once modules exist;
- unit tests for service/store/handler layers;
- real Postgres tests for SQL-heavy store code where practical;
- golden haunt-regression tests for replay, stale cursor, duplicate claim, terminal finality, and legacy cutover behavior;
- gateway route smoke evidence for cutover work;
- Den task/document handles for architectural decisions.

## Deployment contract

Every deployable Go service is registered in `deployment/services.yaml`. The
registry is the operational contract for service name, primary binary path,
systemd unit, loopback health URL, loopback version URL, config example, and env
example.

Deployable HTTP services must support:

- `GET /health` with build metadata;
- `GET /version` with the same build metadata;
- `<binary> --version`, also reporting service name, version, commit, and build
  timestamp.

The deployment contract test builds every registered primary binary and runs
`--version`. Deployment tooling must also smoke `/health` and `/version` and
verify the reported commit matches the built commit. Add a service to the
registry only when this contract is implemented.

## Den task hygiene

When implementing under Den workflow:

- Start from the Den task/doc context, not only this repo.
- Update or comment on the relevant Den document if a boundary decision changes.
- Record branch/commit/test evidence in the Den task thread or handoff.
- Move implementation tasks to review rather than silently treating local code as done.
