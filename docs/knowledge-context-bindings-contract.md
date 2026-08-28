# Guidance-owned scoped Knowledge bindings

Task #7425 establishes the smallest durable applicability record for canonical
Knowledge. A `den_guidance.knowledge_bindings` row says that the globally
slug-keyed Knowledge target is applicable in a named operational scope. It is
not a second Knowledge Entry, a body cache, or a project-local identity.

## Stored contract

Each binding stores only `target_kind` (`knowledge` in V1), `target_ref`,
`scope_kind`, `scope_ref`, optional `audience`, `priority`, `sort_order`,
`read_policy`, `read_when`, explicit `shadows_global`, and audit timestamps.
No canonical body, title, summary, tags, provenance, status, curation state,
replacement, review marker, or revision is stored in Guidance.

Scopes are `global` (`scope_ref` is exactly `_global`), `project`, `task`
(positive decimal task ID), `agent_profile`, and `capability`. Same-target
rows remain inspectable contextual provenance; assignment-level deduplication
belongs to #7426. Resolution reads a canonical target once per request and
reuses that metadata for every applicable binding row.

## Resolution contract

`POST /v1/guidance/knowledge-bindings/resolve` accepts applicable scopes and
audiences. Global is always applicable. Selection order is deterministic:
task, agent profile, capability, project, global; then `must_read`, `inline`,
`on_demand`, `latent`; then priority descending, sort order ascending, target
reference, and binding ID.

`shadows_global=true` is valid only for a non-global binding. It suppresses an
applicable global binding with the *same canonical target reference*; it never
creates a project-local Knowledge identity. If no global candidate exists it
has no effect.

The response preserves binding ID, scope, and `selection_source=knowledge_binding`.
Metadata is read live from Knowledge. The current API supplies status, curation
state, replacement, update time, and review marker. Guidance derives the latest
revision from the revisions endpoint when available; otherwise it reports
`revision_known=false` rather than copying or inventing a revision.

Inline policy admits only the canonical summary to an explicit request budget
of 0–4096 bytes; zero disables inline rendering. Cards always include summary
metadata, so this budget controls only inline admission rather than card data.
It never returns the body. `must_read`, `on_demand`, and `latent` are handles
with their stated retrieval urgency, not body transport.

Missing, archived, and replaced targets are excluded from ordinary selections
and appear in `excluded_bindings` with `missing`, `archived`, or `superseded`.
Deprecated targets without a replacement remain selected but are labelled
`deprecated` for the curator and caller. This makes stale bindings inspectable
without silently changing a canonical target.

## Migration seam

Existing `agent_guidance_entries` and document Guidance endpoints remain
unchanged. `POST /v1/guidance/context-resolve` projects their applicable
entries as `target_kind=document` and
`selection_source=legacy_document_guidance`; it does not persist a duplicate
row and does not call a Document a Knowledge Entry. The Knowledge-only
binding resolver remains available at `/v1/guidance/knowledge-bindings/resolve`.

Binding listing defaults to 50 rows, caps at 200, and returns `next_offset`.
Resolution caps returned binding rows at 100 and marks `truncated`; shadowing
is evaluated before that output limit. Task/profile/capability validation stays
at the trusted service-token boundary; this slice intentionally does not add a
task or profile authority client. Guidance reads live canonical metadata from
Knowledge's bounded `/card` endpoint; it neither receives the entry body nor
duplicates or caches the card.
