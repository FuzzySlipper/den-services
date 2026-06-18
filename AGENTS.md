# Agent bootstrap — den-services

This repository is the experimental Go successor-services workspace for the Den local-services revamp. It is intentionally small, strict, and boundary-heavy. Do not treat it as a place to casually port haunted legacy behavior.

## Required first reads

Before making design or implementation changes, read the project-specific guidance in:

- `agents-project.md` — local repo rules and Den document handles.
- Den document `den-services/architecture-guidelines` — service/lane/schema/gateway constitution.
- Den document `den-services/go-codestyle` — Go style and review rules.
- Den document `den-services/successor-service-registry` — migration/cutover registry.

If Den MCP tools are available, use `mcp_den_get_document(project_id="den-services", slug="...")` to read those documents. If they are not available, ask the planner/user for current copies before changing architecture.

## Operating stance

- Build clean successor services beside legacy Den services; do not perform in-place extraction unless a task explicitly asks for it.
- Preserve the three-lane invariant: conversation, observation, and executable actuation are different authorities with different replay rules.
- Prefer explicit, typed, boring Go over clever or permissive Go.
- Keep `shared/` small. Do not turn it into a cross-domain junk drawer.
- Route work to the owning service/domain. Do not recreate den-channels as a Go monolith.

## Review expectation

No substantial service code should be considered ready without:

- focused unit tests;
- integration or haunt-regression tests when lane/cutover behavior is involved;
- `gofmt`/`go test ./...` evidence once Go modules exist;
- Den task or document evidence for architectural decisions.
