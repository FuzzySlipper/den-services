# Den Core Lifeboat Service Substrate

This is the substrate contract for the next Den Core lifeboat services. It does
not introduce a new fake service template. The template is the existing
`den-services` service shape already deployed on den-srv and enforced by the
repo deployment contract.

Use this document when starting lifeboat service tasks such as projects/scope,
tasks, messages, documents, review, knowledge, guidance, and librarian. The
projects/scope boundary has its first service-specific contract in
[`docs/projects-scope-lifeboat-contract.md`](./projects-scope-lifeboat-contract.md).

## Decision

New lifeboat services use the existing `den-services` Go module and deployment
pattern:

- one Go module per domain authority;
- one primary HTTP binary registered in `deployment/services.yaml`;
- `den-go@<service>.service` on den-srv;
- `/data/services/<service>` as the service root;
- `/etc/den-services/<service>.env` for secrets and the config path;
- typed YAML config loaded by the service;
- shared `/health`, `/version`, service-token auth, and JSON error envelope
  helpers from `shared/`;
- versioned SQL migrations in `migration/postgres/den_<domain>/`;
- runtime services using only app-role database credentials, never migration
  credentials.

Do not create a placeholder service only to prove health/version. The existing
registered services already prove the substrate: gateway, runtime, delivery,
observation, conversation, timeline, visual-contract, doc-publish,
visual-inspect, artifacts, and mcp.

## Service Module Shape

Create a new module only when a task owns a real domain boundary. The module
name should be the service name unless there is an explicit reason otherwise:

```text
<service>/
  go.mod
  cmd/
    <service>/main.go
  config/
    config.example.yaml
    <service>.env.example
  internal/
    config.go
    dto.go
    errors.go
    handler.go
    service.go
    store.go
    types.go
```

The internal package can omit files that are not needed yet, but it should not
invent a different layering model. `main.go` wires dependencies only. Handlers
own HTTP validation and response shaping. Services own invariants and
cross-service coordination. Stores own SQL.

A module may contain more than one `cmd/` binary only when the binaries share
the same schema and state machine. The deployable service is still the module's
primary HTTP binary.

## Deployment Registry

Every deployable HTTP service is registered in `deployment/services.yaml`.
The registry fields are the operational contract:

```yaml
- name: "<service>"
  module: "<service>"
  binary_name: "<service>"
  binary_path: "./<service>/cmd/<service>"
  config_example: "<service>/config/config.example.yaml"
  env_example: "<service>/config/<service>.env.example"
  health_url: "http://127.0.0.1:<port>/health"
  version_url: "http://127.0.0.1:<port>/version"
  systemd_unit: "den-go@<service>.service"
```

The root `deployment_contract_test.go` builds each registered binary and runs
`--version`. It also checks that health/version URLs are loopback and that the
systemd unit follows `den-go@<service>.service`.

Do not register a service until its primary binary actually supports the
contract. A planning-only module can exist without registry entry if it is not
deployable yet.

## den-srv Runtime Shape

All Go services run under the shared systemd template:

```text
/etc/systemd/system/den-go@.service
```

The live template starts services as:

```text
User=agent
Group=agents
WorkingDirectory=/data/services/%i
EnvironmentFile=-/etc/den-services/%i.env
Environment=SERVICE_NAME=%i
Environment=SERVICE_ROOT=/data/services/%i
ExecStart=/data/services/%i/bin/%i
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/data/services/%i
```

The deployed filesystem layout is:

```text
/data/services/<service>/
  bin/
    <service>
    <service>.previous
  config/
    config.yaml
  data/
  logs/
  tmp/
  backups/
  releases/
    <build-timestamp>/
      <service>
      build-info.json
```

Use `scripts/den-services-deploy.sh <service>` for the standard install path.
The script reads `deployment/services.yaml`, refuses dirty deploys, runs
`go test ./...`, builds with version metadata, installs to
`/data/services/<service>`, restarts the registered systemd unit, smokes
`/health` and `/version`, and rolls back the binary if smoke fails.

## Build Metadata

Each primary binary has these package variables in `main`:

```go
var (
    version = "dev"
    commit  = "unknown"
    builtAt = "1970-01-01T00:00:00Z"
)
```

The binary must support:

```sh
<service> --version
```

with output containing the service name, version, commit, and build timestamp.
The deploy script injects metadata with:

```sh
-ldflags "-s -w -X main.version=${version} -X main.commit=${commit} -X main.builtAt=${built_at}"
```

HTTP services build `shared/health.BuildInfo` from the same values and expose:

- `GET /health`
- `GET /version`

Health and version endpoints should remain unauthenticated so deployment smoke
checks can verify liveness and build identity.

## Configuration And Secrets

Each service owns a typed `Config` in `internal/config.go` and a documented
`config/config.example.yaml`.

The environment file should contain only the config path and secrets:

```text
<SERVICE>_CONFIG_PATH=/data/services/<service>/config/config.yaml
DEN_<SERVICE>_DATABASE_URL=postgres://...
DEN_<SERVICE>_SERVICE_TOKEN=...
```

The config file references secret values with environment expansion:

```yaml
bind_addr: "127.0.0.1:<port>"
database_url: "${DEN_<SERVICE>_DATABASE_URL}"
http:
  read_header_timeout: "5s"
```

Load config from the service-specific config path environment variable, default
to `config/config.yaml` for local development, expand secrets with
`shared/config`, and validate every loaded value at startup.

Do not scatter tunables across environment variables. Ports, URLs, limits,
timeouts, and thresholds belong in YAML. Secrets and deployment-specific
credentials belong in `/etc/den-services/<service>.env`.

## HTTP And MCP-Compatible Errors

Use `shared/api` for service-token authentication and response envelopes.
The standard error shape is:

```json
{
  "error": {
    "code": "not_found",
    "message": "not found: task 123"
  }
}
```

This shape is compatible with `den-services/mcp` backend routing because the
MCP facade can preserve backend status/error evidence without learning domain
logic.

Expected failures should be sentinel or typed errors mapped through
`api.WriteServiceError`. Domain packages may implement
`api.CodedStatusError` when they need a stable MCP-facing code. Avoid ad hoc
`map[string]any` error payloads.

`den-services/mcp` remains a facade. New domain SQL, state machines, and
business rules belong in the owning lifeboat service, not in `mcp`.

## Postgres And Migrations

Each domain service owns one schema and one app role:

```text
schema:   den_<domain>
app role: den_<domain>_app
```

Add migrations under:

```text
migration/postgres/den_<domain>/<version>_<description>.sql
```

The shared migration runner discovers embedded migrations, creates the schema
and `schema_migrations` table if needed, and applies migrations transactionally
with the migration role.

Runtime services must connect with the app role only. They must not run
migrations or hold migration credentials. Cross-schema writes are forbidden.
Cross-schema reads require named views and explicit grants in migrations.

For a new lifeboat service, add app-role bootstrap to
`deployment/postgresql-app-roles.psql` and document the environment variables in
`deployment/postgresql.md`.

## Smoke Pattern

Local development smoke for a real service uses the service's config file plus
the required secret environment values. For DB-backed services, do not expect
`config.example.yaml` to run by itself unless the referenced environment
variables are set.

```sh
go test ./<service>/...
go build -o /tmp/<service> ./<service>/cmd/<service>
/tmp/<service> --version
set -a
. ./<service>.env
set +a
<SERVICE>_CONFIG_PATH=<service>/config/config.yaml /tmp/<service>
curl -fsS http://127.0.0.1:<port>/health
curl -fsS http://127.0.0.1:<port>/version
```

Live deployment smoke is owned by `scripts/den-services-deploy.sh`. Do not add
one-off deploy scripts unless the service has a real operational exception.

For MCP-routed services, add a route-table entry and an MCP facade smoke only
after the service route exists and is backed by real behavior.

## Lifeboat Service Task Acceptance

Future service tasks should reference this substrate instead of redefining it.
Use this acceptance baseline:

- module follows the existing den-services Handler -> Service -> Store pattern;
- primary binary supports `--version`, `/health`, and `/version`;
- service is registered in `deployment/services.yaml` only when deployable;
- config example and env example exist and are validated by tests;
- schema migrations live under `migration/postgres/den_<domain>/`;
- runtime code uses app-role credentials only;
- service errors use the shared JSON envelope and stable codes;
- mcp routing remains facade-only and contains no domain SQL/business logic;
- no fake service skeleton is created just to satisfy substrate smoke.

## Current Exemplars

Use these real services as references:

- `conversation`: current full CRUD/service/store example with Postgres and
  service-token auth.
- `runtime`: liveness state machine, background sweep loop, and app-role store.
- `delivery`: executable actuation state machine and multiple `cmd/` binaries
  sharing one domain.
- `mcp`: facade-only service with backend route table and no domain SQL.
- `artifacts`: recent small deployed service registered in the shared deploy
  contract.
