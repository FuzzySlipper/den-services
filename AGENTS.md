# Agent bootstrap — den-services

This repository is the experimental Go services workspace for Den's local service layer. It is intentionally small, strict, and boundary-heavy. Do not treat it as a place to casually port haunted legacy behavior.

## Required first reads

Before making design or implementation changes, read the project-specific guidance in:

- `agents-project.md` — local repo rules and Den document handles.
- Den document `den-services/architecture-guidelines` — service/lane/schema/gateway constitution.
- Den document `den-services/go-codestyle` — Go style and review rules.
- Den document `den-services/service-registry` — current service registry.

Read those documents with the Den MCP `get_document` tool (`project_id="den-services"`) or `den-tool den get_document`. If neither is available, ask the user for current copies before changing architecture.

## Operating stance

- Build clean, bounded services; do not perform in-place extraction unless a task explicitly asks for it.
- Preserve the three-lane invariant: conversation, observation, and executable actuation are different authorities with different replay rules.
- Prefer explicit, typed, boring Go over clever or permissive Go.
- Keep `shared/` small. Do not turn it into a cross-domain junk drawer.
- Route work to the owning service/domain. Do not recreate den-channels as a Go monolith.

## Review expectation

No substantial service code should be considered ready without:

- focused unit tests;
- integration or haunt-regression tests when lane/cutover behavior is involved;
- `gofumpt`/`go test ./...` evidence once Go modules exist;
- Den task or document evidence for architectural decisions.

## Deploying

A `looks_good` review of a den-services task deploys the reviewed commit by itself: crew-review runs `scripts/deploy.sh`, which redeploys only the services that changed and posts the result on the task thread. Run `scripts/deploy.sh` yourself only to retry a failed deploy (`--dry-run` shows what would change). See `deployment/proxmox.md`.
