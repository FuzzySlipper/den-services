# Projects/Spaces Scope Lifeboat Contract

This is the extraction contract for the Den Core project/space registry. It is
the first lifeboat domain to move because every other scoped service needs a
stable project ID, space visibility, root path metadata, and archive-state
policy before it can safely leave Core.

The successor service should be named `projects`. `scope` is the conceptual
authority: every row is a scope that other services may attach records to. The
module, deployment entry, and schema should stay aligned with the existing Core
vocabulary:

- Go package/service: `den-services/projects`
- Postgres schema: `den_projects`
- Runtime role: `den_projects_app`
- Physical table: `den_projects.projects`
- Deployment pattern: the lifeboat substrate in
  [`docs/lifeboat-service-substrate.md`](./lifeboat-service-substrate.md)

Do not create a placeholder service only to satisfy a smoke test. Register a
service in `deployment/services.yaml` only when it is ready to expose the real
project/space contract.

## Existing Core Inventory

Core currently stores projects and spaces in one table:

```sql
create table den_core.projects (
    id text primary key,
    name text not null,
    kind text not null default 'project',
    visibility text not null default 'normal',
    owner text,
    root_path text,
    description text,
    settings_json jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
```

Current stable kinds are `project`, `personal`, `assistant`, `knowledge_base`,
and `system`. Current stable visibility values are `normal`, `hidden`, and
`archived`. The successor service should validate those values in Go rather
than freezing them as database `CHECK` constraints; lifeboat services should be
strict at the API boundary but avoid schema churn for vocabularies that may
grow.

Core source owners:

- `ProjectRepository` owns create, get by ID, list, stats reads, visibility
  changes, metadata updates, dependent-count checks, and force delete.
- `ProjectRoutes` exposes `/api/projects`.
- `SpaceRoutes` exposes `/api/spaces`.
- `ProjectTools` exposes project MCP tools.
- `SpaceTools` exposes space MCP tools.

Current REST behavior to preserve:

- `POST /api/projects` creates a `kind='project'` scope.
- `GET /api/projects` lists only visible normal `kind='project'` rows.
- `GET /api/projects/{id}?agent=` returns the project row plus task/unread
  summary stats.
- `PATCH /api/projects/{id}` updates only non-null metadata fields.
- `POST /api/spaces` creates any supported `kind`, defaulting to `project`.
- `GET /api/spaces?kind=&includeHidden=&includeArchived=` lists visible spaces
  by default and includes hidden/archived only when requested.
- `GET /api/spaces/{id}?agent=` returns the project row plus summary stats.
- `PATCH /api/spaces/{id}/visibility` sets `normal`, `hidden`, or `archived`.
- `POST /api/spaces/{id}/archive` is a convenience archive operation.
- `DELETE /api/spaces/{id}?force=` is an admin-danger operation; ordinary users
  should archive or hide instead.

Current MCP tools to preserve or explicitly stage:

- `create_project`
- `list_projects`
- `get_project`
- `update_project`
- `create_space`
- `list_spaces`
- `get_space`
- `update_space_visibility`
- `archive_space`
- `delete_space`

## Inbound Dependencies

The project ID is the common scope key for Core-owned domains. The lifeboat must
not move all dependent domains at once, but it must give them a stable read API
and a durable ID contract.

Known dependent domains include:

- tasks, task dependencies, task history, review workflow, and task packets;
- messages, message read state, notifications, and user-facing message feeds;
- documents, archived documents, document discussion threads, and comments;
- agent guidance entries, knowledge/librarian lookups, and guidance resolution;
- worker pool assignments, runs, completions, orchestrator leases, and
  no-capacity diagnostics;
- agent stream entries and active agent/projection views;
- capabilities, capability ownership, and invocation/audit records;
- desktop/collaboration state that needs a project or space boundary;
- Den Web and local tooling that depend on `root_path`, `kind`, and
  `visibility` to decide what to show or open.

Dependent services should store `project_id text` or `space_id text` as their
own domain field. Cross-domain cascade deletes are forbidden. If a later schema
uses a foreign key to `den_projects.projects`, it must be `ON DELETE RESTRICT`
and must not grant the dependent service write authority over project rows.

## New Schema Boundary

The owning schema is `den_projects`. The canonical table remains `projects` to
avoid a needless vocabulary fork from Core.

```sql
create table den_projects.projects (
    id text primary key,
    name text not null,
    kind text not null default 'project',
    visibility text not null default 'normal',
    owner text,
    root_path text,
    description text,
    settings_json jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);
```

Service rules:

- `id` is the stable cross-domain scope identifier.
- `kind='project'` is the project list subset; non-project kinds are spaces.
- `visibility='hidden'` removes the row from default lists but does not imply a
  write stop.
- `visibility='archived'` removes the row from default lists and creates write
  friction for dependent domains.
- `root_path` is optional and may be cleared by sending an empty string through
  the compatibility update path.
- `settings_json` is opaque JSON owned by the project registry; callers must
  not infer schema beyond their own documented keys.
- `created_at` is immutable after insert.
- `updated_at` changes on every metadata or visibility update.

Recommended read projections:

- `den_projects.project_refs`: `id`, `kind`, `visibility`, `owner`,
  `root_path`, `updated_at`.
- `den_projects.visible_projects`: normal `kind='project'` rows.
- `den_projects.visible_spaces`: all rows except hidden/archived rows.

Those projections are for internal read grants and smoke tests. Public callers
should prefer the service API rather than reading the schema directly.

## Service API

The successor service should expose native REST routes under `/v1`. The MCP
facade can adapt legacy tool names to these routes.

Project routes:

- `POST /v1/projects`
  - Request: `id`, `name`, optional `root_path`, `description`.
  - Creates `kind='project'`, `visibility='normal'`.
- `GET /v1/projects?include_hidden=false&include_archived=false`
  - Default response matches current `list_projects`: only normal project rows.
- `GET /v1/projects/{id}`
  - Returns the project row, including `kind`, `visibility`, metadata, and
    timestamps.
- `PATCH /v1/projects/{id}`
  - Updates non-null `name`, `root_path`, `description`, `owner`,
    `settings_json`.

Space routes:

- `POST /v1/spaces`
  - Request: `id`, `name`, optional `kind`, `visibility`, `owner`, `root_path`,
    `description`, `settings_json`.
  - Defaults: `kind='project'`, `visibility='normal'`.
- `GET /v1/spaces?kind=&include_hidden=false&include_archived=false`
  - Default response includes visible rows of every kind.
- `GET /v1/spaces/{id}`
  - Same row shape as `GET /v1/projects/{id}`.
- `PATCH /v1/spaces/{id}/visibility`
  - Request: `visibility`.
  - Reversible; unarchive is `visibility='normal'`.
- `POST /v1/spaces/{id}/archive`
  - Convenience wrapper for `visibility='archived'`.

Scope helper routes for dependent services:

- `GET /v1/scopes/{id}`
  - Alias of the canonical row read. Dependent domains may use this when they
    do not care whether the row is called a project or a space.
- `POST /v1/scopes/{id}/assert-writable`
  - Returns success for `normal` and `hidden`.
  - Returns a structured conflict for `archived` unless the request includes a
    documented admin override.

`delete_space` should not be part of the green-path REST API. If retained for
operator use, it must live behind an admin-only route such as
`POST /v1/admin/spaces/{id}/delete`, must report dependent counts before
deletion, and must reject protected IDs unless an explicit force flag is
present. The preferred operational path is archive, not deletion.

## Stats Compatibility

Current Core `get_project` and `get_space` responses include task counts and an
agent-specific unread message count. Those counters are not owned by the
project registry. Do not move task/message SQL into the projects service to
preserve that shape.

Compatibility should be staged:

1. The projects service owns canonical project/space metadata reads and writes.
2. MCP keeps `get_project` and `get_space` routed to Core, or composes their
   stats from Core, until task/message successor APIs exist.
3. Once task/message services expose read summaries, MCP can compose the legacy
   `ProjectWithStats` response from projects metadata plus those summaries.
4. The projects service may expose a lightweight metadata-only response for
   dependent lifeboat services at every phase.

This keeps the first service small and prevents it from becoming a new Core
hub.

## Archived-Project Write Friction

Archived scopes are not deleted. They remain readable and reversible, but new
domain writes should become noisy and intentional.

Dependent services must enforce this rule before creating or mutating scoped
records:

- `normal`: writes allowed.
- `hidden`: writes allowed; hidden is a listing/UI concern.
- `archived`: writes rejected with a structured conflict unless the operation
  is archival/restore work or carries an explicit admin override.

The projects service provides the authoritative `visibility` and the
`assert-writable` helper. Dependent services remain responsible for checking it
at their own write boundary because they own their domain writes.

## MCP Compatibility Mapping

All existing project/space operations currently route through `den-core` in
`mcp/routes.example.yaml`. The compatibility path should move in phases:

| MCP tool | Current backend | Successor backend | Cutover note |
| --- | --- | --- | --- |
| `create_project` | `den-core` | `projects` | Move after import/write parity smoke passes. |
| `list_projects` | `den-core` | `projects` | Move with create/update. Preserve default visible-project filter. |
| `update_project` | `den-core` | `projects` | Preserve non-null patch semantics and root-path clear behavior. |
| `create_space` | `den-core` | `projects` | Move with project writes. Preserve kind/visibility defaults. |
| `list_spaces` | `den-core` | `projects` | Preserve `kind`, `include_hidden`, and `include_archived`. |
| `update_space_visibility` | `den-core` | `projects` | Move with archive smokes. |
| `archive_space` | `den-core` | `projects` | Thin adapter over visibility update. |
| `get_project` | `den-core` | staged | Keep Core-routed or MCP-composed until stats sources move. |
| `get_space` | `den-core` | staged | Same as `get_project`. |
| `delete_space` | `den-core` | admin-only or retired | Do not green-path. Prefer archive/update visibility. |

The MCP facade should stay a routing and adaptation layer. It must not contain
project SQL or enforce project business logic beyond request/response
translation.

## Parity Smokes

Run these against a migrated staging copy before flipping MCP routes:

1. Snapshot Core and successor `list_projects` output with default filters.
   IDs, names, root paths, visibility, owner, description, and settings JSON
   must match for normal project rows.
2. Snapshot Core and successor `list_spaces` output for:
   - default filters;
   - `kind=assistant`;
   - `include_hidden=true`;
   - `include_archived=true`.
3. Create a temporary project through the compatibility path, read it back,
   update `root_path`, `description`, `owner`, and `settings_json`, then read it
   again.
4. Create a temporary non-project space, hide it, verify default lists exclude
   it, verify `include_hidden=true` includes it, then restore it to normal.
5. Archive a temporary space, verify default lists exclude it, verify
   `include_archived=true` includes it, then unarchive through
   `update_space_visibility`.
6. Verify `_global`, `den-core`, `core`, system, and personal scopes survive
   import and remain protected from ordinary deletion.
7. Verify service health/version endpoints and route-table readiness before
   enabling MCP traffic.

## Archive-State Smokes

Archive-state behavior needs its own smoke because dependent services can
accidentally bypass it:

1. For an archived scope, `GET /v1/scopes/{id}` returns the row and
   `visibility='archived'`.
2. `POST /v1/scopes/{id}/assert-writable` returns a structured conflict for an
   ordinary request.
3. A future task/document/message successor write against an archived scope
   fails before inserting domain data.
4. The same write either succeeds against `normal`/`hidden` or fails for a
   domain-specific reason unrelated to visibility.
5. Admin restore to `normal` makes ordinary writes possible again.

## Cutover Sequence

1. Create `den_projects` schema, roles, and import migration.
2. Run dual-read parity smokes without changing MCP routes.
3. Enable projects service native reads for internal consumers that can accept
   metadata-only responses.
4. Route create/list/update/archive MCP tools to `projects`.
5. Keep `get_project` and `get_space` staged until stats compatibility can be
   composed without reintroducing cross-domain SQL in the projects service.
6. Retire or admin-quarantine `delete_space`.

The goal is a stable scope registry, not a new Core-shaped hub.
