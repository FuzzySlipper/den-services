# Knowledge Service Route Readiness

Task: 3641
Follow-up: 3784 (`den_knowledge_*` MCP successor HTTP adapter/flip)
Service: `den-services/knowledge`
Schema: `den_knowledge`
Role: `den_knowledge_app`

## Implemented Native Routes

- `POST /v1/knowledge/entries`
- `GET /v1/knowledge/entries`
- `GET /v1/knowledge/entries/{slug}`
- `GET /v1/knowledge/entries/{slug}/revisions`
- `POST /v1/knowledge/search`
- `POST /v1/knowledge/guide`

## MCP Mapping Guidance

The compatibility tool names and schemas stay unchanged. Route these tools to
`knowledge` only after `den-services/mcp` has an adapter or facade that can call
successor HTTP routes and return MCP `tools/call` results:

- `den_knowledge_search` -> `POST /v1/knowledge/search`
- `den_knowledge_get` -> `GET /v1/knowledge/entries/{slug}`
- `den_knowledge_guide` -> `POST /v1/knowledge/guide`
- `den_knowledge_store` -> `POST /v1/knowledge/entries`

The current `mcp/routes.example.yaml` remains on `den-core` for these tools
because the MCP gateway currently supports `mcp_tools_call` to
`mcp_jsonrpc_result` backends only. Pointing the route table directly at this
HTTP service before that adapter exists would make the live tool calls fail
instead of preserving compatibility.

## Preserved Behavior

- Knowledge entries are global; there is no `project_id` scope.
- Search and list default to `status='reviewed'`.
- `include_unreviewed=true` adds `draft` and `needs_review`.
- `include_deprecated=true` adds `deprecated`.
- `include_archived=true` is required to read archived entries through filtered
  paths.
- `required_tags` is strict AND gating.
- `any_tags` is OR gating.
- `den_knowledge_get` returns full `body_markdown`; search results do not.
- Guide responses are extractive and citation-backed. The service does not make
  LLM calls and reports uncertainty when no reviewed entries match.
- Knowledge is separate from document search. Do not route `search_documents`,
  `list_documents`, `query_archived_documents`, librarian tools, or
  agent-guidance tools to `knowledge`.

## Smoke Coverage

The package tests seed reviewed and draft entries in the service test store and
cover:

- store draft/reviewed entries;
- reviewed-only default search;
- `include_unreviewed` expansion;
- required/any tag gating;
- full get with `body_markdown`;
- revision creation on update;
- guide citations and uncertainty;
- document-search exclusion by service boundary.

`DEN_KNOWLEDGE_TEST_DATABASE_URL` enables the optional Postgres FTS smoke for
the generated `search_vector` path.

## Rollback

No production MCP route flip or deployment has been performed in this task. The
rollback point is therefore the existing den-core route table ownership. After a
future adapter/facade task flips the four `den_knowledge_*` tools, rollback is
to point those operations back to `den-core` `/mcp`.
