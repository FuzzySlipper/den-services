# den-services

Clean Go successor services for Den's local service layer.

This workspace exists to build new, small, boundary-heavy services beside the
legacy Den services. The migration strategy is strangler-fig: route callers
through a gateway, cut over one function at a time, and keep legacy history
display-only unless a task defines an explicit promotion path.

Authoritative project guidance lives in Den documents:

- `den-services/architecture-guidelines`
- `den-services/go-codestyle`
- `den-services/successor-service-registry`

Local snapshots are checked in as [ARCHITECTURE.md](ARCHITECTURE.md) and
[CODESTYLE.md](CODESTYLE.md) for repo-adjacent review, but the Den documents
remain the source of truth when they diverge.

## Workspace

The repository is a Go workspace with one module per service authority:

- `shared/`: cross-cutting infrastructure only
- `gateway/`: front-door proxy and route selection
- `runtime/`: runtime instance liveness and subscriptions
- `delivery/`: executable delivery intent lifecycle
- `observation/`: non-waking projections and composed read models
- `conversation/`: eventual conversation successor
- `migration/`: offline migration runner and SQL files
- `integration/`: cross-module tests

## Commands

```sh
go work sync
go build ./...
make test
make build SERVICE=gateway
make build-all
```
