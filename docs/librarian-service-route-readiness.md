# Librarian Service Route Readiness

Task: 3880.

The librarian successor is a read-only aggregator at `den-services/librarian`.
It does not own domain tables in V1 and does not require PostgreSQL migrations
or application-role grants.

## Native routes

- `POST /v1/projects/{project_id}/librarian/query`
- `POST /v1/librarian/query`

Both routes return the MCP-compatible fields:

- `relevant_items`
- `recommendations`
- `confidence`

The native response also includes the query, scope, budget, scores, and source
degradation warnings.

## MCP route

`query_librarian` is routed in `mcp/routes.example.yaml` to backend
`librarian` with request adapter `mcp_librarian_rest`.

## Deploy

1. Deploy `librarian`.
2. Deploy or restart `mcp` so `config/routes.yaml` and backend config include
   the `librarian` backend.
3. Run the live MCP smoke with `DEN_MCP_SMOKE_LIBRARIAN_URL` set to
   `http://127.0.0.1:8098`.

## Rollback

If librarian health or `query_librarian` smoke fails, restore the previous MCP
route table entry for `query_librarian` pointing at `den-core`, restart `mcp`,
and stop `den-go@librarian.service`. The librarian service is read-only, so no
data rollback is required.
