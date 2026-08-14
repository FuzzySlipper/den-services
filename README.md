# den-services

Clean Go services for Den's local service layer.

This workspace contains small, boundary-heavy services for Den's local service
layer. Route callers through the owning service and gateway, and keep legacy
history display-only unless a task defines an explicit promotion path.

Authoritative project guidance lives in Den documents:

- `den-services/architecture-guidelines`
- `den-services/go-codestyle`
- `den-services/service-registry`

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
- `conversation/`: conversation authority
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

## Deployment

For a new, empty instance with PostgreSQL and all services on one machine, use
the [single-machine deployment guide](deployment/new-instance.md). Its Fedora
path and local-agent security profile were validated during the first
clean-machine deployment.

For repeatable updates on the external Den machines, use the [fleet update
helper](deployment/fleet-update.md):

```sh
scripts/update-den-fleet.sh --target m5 --preflight
scripts/update-den-fleet.sh --target m5
```
