# Den Services - Go Codestyle

Source: Den document `den-services/go-codestyle`.

This file is a repo-local snapshot for review convenience. The Den document is
authoritative if this copy ever diverges.

## Philosophy

Write Go with C#-like structural discipline, not ceremony:

- Handler -> Service -> Store layering.
- Explicit typed domain, request, and response models.
- Constructor injection for dependencies.
- No hidden mutable state.
- No business logic in handlers or stores.
- State transitions as named methods.

Choose explicit, typed, boring Go.

## Service Structure

```text
<domain>/
  config/
    config.example.yaml
  cmd/
    <service>/main.go
  internal/
    types.go
    config.go
    state.go
    store.go
    service.go
    handler.go
    dto.go
  go.mod
```

`main.go` wires dependencies only. Handlers validate and shape HTTP. Services
enforce invariants and coordinate dependencies. Stores own SQL. DTOs are
separate from domain types.

## Types

- Domain concepts get named structs, not `map[string]any`.
- Invariant-bearing domain objects use private fields with accessors and
  transition methods.
- Enum-like values use typed string constants with `IsValid()` methods.
- Nullable fields use pointers, not sentinel values.

## Constructors And Rehydration

New-object constructors validate invariants and generate fresh values. Database
loads use separate package-local rehydration constructors that validate
persisted state without resetting IDs, timestamps, or lifecycle fields.

State changes go through transition methods. Contested transitions still depend
on atomic store methods for concurrency authority.

## Errors

Expected failures use sentinel errors checked with `errors.Is`. Structured
context can use typed errors. Wrap errors with context and never swallow them.
Handlers map service errors through the shared API error registry.

No `panic` in service code. No `log.Fatal` outside `main.go`.

## Layering

Handlers:

- validate request format and required fields;
- call service methods;
- return DTOs;
- contain no SQL and no business logic.

Services:

- take `context.Context` first for I/O methods;
- enforce invariants and coordinate cross-service calls;
- use injected clocks for time-dependent behavior;
- delegate contested state transitions to atomic store methods.

Stores:

- own SQL;
- use explicit column lists, never `SELECT *`;
- use parameterized Postgres placeholders;
- implement compare-and-swap transitions with preconditioned `where` clauses.

## Interfaces And Injection

Define interfaces at the consumer and keep them narrow. Use constructor
injection. Do not create service locators, global singletons, or decorative
interface hierarchies.

## Configuration

Nothing that varies by deployment is hardcoded. Each module owns a typed
`Config` struct loaded from a YAML or JSON config file. The config file path is
the normal environment variable. Secrets may use environment substitution, but
individual tunables should not be scattered across environment variables.

Every config file is validated at load time. Every module ships a documented
`config/config.example.yaml`.

## Forbidden Patterns

- `init()` functions.
- Package-level mutable state.
- Domain behavior driven by reflection.
- Goroutines launched from handlers without lifecycle ownership.
- Dynamic JSON blobs crossing service boundaries unless explicitly designed.
- `interface{}` or `any` in domain types.
- Clever concurrency in request paths.
- Hardcoded tunable ports, URLs, TTLs, thresholds, or timeouts.

## Testing

Tests live next to source. Use table-driven tests for state transitions and
edge cases. Service tests mock dependencies. Store tests use a real Postgres
test database when SQL behavior matters. Integration tests live in the
`integration/` module.

## DTO And JSON Conventions

API request and response structs are separate from domain types. Domain types do
not carry JSON tags unless they are simple shared value objects. JSON field
names are snake_case. Request DTOs have `Validate()` methods.

## Identity

Use `shared/identity` canonical types:

- `ProfileIdentity`
- `AgentInstanceID`
- `SessionKey`
- `AgentIdentity`

Do not invent local identity field names.

## Imports And Logging

Imports are grouped as standard library, external dependencies, then internal
packages. Use `log/slog` for service logging. Log to stdout/stderr and never
log secrets or full tokens.

## Review Checklist

- No `init()` functions.
- No package-level mutable state.
- Explicit structs for domain concepts.
- Private fields for invariant-bearing domain types.
- Constructors and rehydration paths are distinct.
- State transitions are explicit methods.
- Contested transitions use atomic store methods.
- Context is first for I/O methods.
- Handler -> Service -> Store layering holds.
- Interfaces are consumer-defined and narrow.
- Dependencies are constructor-injected.
- Time-dependent logic uses injected clocks.
- Tunables live in validated config files.
- DTOs are separate from domain types.
- SQL has explicit columns and parameterized placeholders.
- Haunt-regression tests cover the module's lane.
