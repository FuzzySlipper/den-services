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

The compatibility tool names and schemas stay unchanged. `den-services/mcp`
uses the `mcp_knowledge_rest` adapter to call successor HTTP routes and wrap
responses in MCP `tools/call` result envelopes:

- `den_knowledge_search` -> `POST /v1/knowledge/search`
- `den_knowledge_get` -> `GET /v1/knowledge/entries/{slug}`
- `den_knowledge_guide` -> `POST /v1/knowledge/guide`
- `den_knowledge_store` -> `POST /v1/knowledge/entries`

`mcp/routes.example.yaml` and the deployed MCP route table now route these four
operations to the `knowledge` backend with `mcp_knowledge_rest` and
`mcp_tool_result_json`. The previous Core route remains the rollback target.

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

Rollback is route-table only unless the knowledge service itself is unhealthy
for unrelated reasons: point the four `den_knowledge_*` operations in
`/data/services/mcp/config/routes.yaml` back to:

- `backend: "den-core"`
- `method: "POST"`
- `path: "/mcp"`
- `request_adapter: "mcp_tools_call"`
- `response_adapter: "mcp_jsonrpc_result"`

Then restart `den-go@mcp.service`. This restores Core MCP ownership for the
knowledge tools without changing the successor knowledge service data.
