# Knowledge progressive navigation contract

Knowledge remains the authority for independently revisable reviewed entries. This contract adds bounded navigation metadata; it does not create a second document, Guidance, or session-state authority.

## Read paths

All card-shaped responses omit `body_markdown` and include the entry title, summary, kind, status, curation state, tags, source references, replacement slug/compact target (or `missing: true`), current revision, digest, and update timestamp.

| Endpoint | Purpose | Bounds |
| --- | --- | --- |
| `GET /v1/knowledge/entries/{slug}/card` | One compact decision card. | No body. |
| `POST /v1/knowledge/entries/cards?offset=N` | Batch decision cards for explicit handles. Missing handles are named, not fatal. | At most 100 supplied handles and 25 distinct handles per page; `next_offset` continues. |
| `GET /v1/knowledge/entries/{slug}/read?view=outline` | Heading IDs and levels. | At most 64 ATX Markdown headings. Fenced-code pseudo-headings are ignored. |
| `GET /v1/knowledge/entries/{slug}/read?view=section&section=heading-id` | One named section including nested headings. | 48 KiB maximum section body; a larger section returns `validation_failed`. |
| `GET /v1/knowledge/entries/{slug}/read?view=full` | Existing complete entry body. | Explicit full-read opt-in. |

Every `read` includes at most 20 resolved outgoing navigation links. Supplying either `known_revision` or `known_digest` equal to the current entry returns `unchanged: true` with only the compact receipt/card, never body, outline, or links. Callers own any read cache; Knowledge stores no assignment/session reads.

## Curated traversal

`PUT /v1/knowledge/entries/{slug}/links` replaces the explicit outgoing links for an existing entry. Link kinds are only `related`, `embed`, and `replacement`. Targets must exist when written and self-links/duplicates are rejected. Reads resolve target cards compactly; legacy or subsequently missing targets are returned with `missing: true` rather than producing a graph walk or an invalid success.

`POST /v1/knowledge/maps` stores a named curated map and `GET /v1/knowledge/maps/{slug}` returns its ordered, optionally grouped references as cards. A map has title and summary only; it cannot own Markdown prose. Map references must exist and are ordered positions from 0 to 99.

## Compatibility and ownership

The existing entry list, search, get, revision, guide, and write endpoints retain their response shapes. Progressive card/read/map/link operations are exact-callable long-tail MCP operations and therefore available through den-tool without enlarging ordinary `tools/list`; write operations remain deliberate den-tool workflows. Guidance consumes only the single-card metadata endpoint.
