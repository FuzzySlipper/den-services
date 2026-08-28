# Knowledge context manifests: operator and agent guide

## Paved path

1. Write a narrow assignment. Keep instructions separate from optional
   background and name the agent profile or capabilities only when they affect
   applicability.
2. Compose the packet with `den-tool den compose_assignment_manifest`. Pass the
   canonical task ID, assignment, optional profile/capabilities, explicit
   Knowledge slugs, inherited handles, and finite limits.
3. Hand the worker the returned Markdown and structured references. These are
   body-free cards with selection reasons, authority/freshness metadata, and
   stable handles; the Markdown is a projection, not another source of truth.
4. Read `must_read` material, then inspect other cards or a Knowledge Map before
   opening bodies. Use `den_knowledge_cards` for a batch decision and
   `den_knowledge_read` with `outline`, `section`, or explicit `full` views.
5. Reuse `known_revision` and `known_digest` on later reads. An unchanged entry
   returns a compact receipt instead of its body.
6. When reusable material is wrong or stale, correct the canonical Knowledge
   Entry with provenance and a change note. Do not copy a corrected body into
   Guidance bindings, Maps, task messages, or local instruction files.

Example composition:

```sh
den-tool den compose_assignment_manifest --args-json '{
  "task_id": 7428,
  "assignment": "Audit Knowledge retrieval and report the handles opened.",
  "agent_profile": "luna-diagnostician",
  "capabilities": ["knowledge-curation"],
  "explicit_knowledge_refs": ["den-guidance-source-of-truth"],
  "limits": {"max_handles": 8, "inline_budget": 512, "librarian_items": 2}
}'
```

## Task 7428 pilot evidence

The pilot used one real task and two sibling Luna assignments. The composer
produced different eight-handle packets: the source/catalog packet rendered to
1,768 bytes and the retrieval/curation packet to 1,807 bytes. Task Guidance,
profile/capability bindings, explicit selections, a curated Map, and bounded
Librarian suggestions all resolved successfully. No Knowledge bodies appeared
in either packet.

The small packet replaced repeated prose with canonical handles, but the first
workers still opened every offered handle. That is useful negative evidence:
showing a pointer is not enough to make optionality obvious. A focused follow-up
offered the same eight handles, imposed a four-handle retrieval budget, and was
completed from source-of-truth code/tests with zero handle reopens. Handoffs
should say that `must_read` is the required set, `on_demand` is an offer, and
ordinary work should use an explicit retrieval budget. The composer must not
silently convert every visible pointer into required context.

The curator found the deliberately stale entry
`den-services-context-manifest-pilot-decision`. Revision 1 claimed that all
progressive Knowledge operations should enter ordinary MCP discovery. Live
catalog evidence showed 39 direct-profile tools and 34 managed-runtime tools;
both kept only `den_knowledge_get` and `den_knowledge_guide` visible while the
complete 94-operation den-tool catalog retained exact-callable long-tail
operations. The same canonical entry was corrected at revision 2 with task
#7428 provenance and `agent_curated` state. Reading with known revision 1
returned the changed body; repeating revision 2 and its digest returned
`unchanged: true` without body content.

The Map `den-services-context-manifest-pilot` contains five ordered, grouped
cards and no prose bodies. It demonstrates a curated navigation root without
making the Map another document authority.

## Retained decisions and deferred work

- Retain assignment-specific, body-free manifests; scoped Guidance bindings;
  compact cards; progressive reads; unchanged receipts; and curated Maps.
- Keep `den_knowledge_get` and `den_knowledge_guide` in routine MCP discovery.
  Keep progressive navigation, Maps, link maintenance, writes, and deletes in
  the exact-callable long-tail den-tool catalog until observed frequency
  justifies promoting one operation individually.
- Migrate incrementally. Bind a small reviewed canonical set when a real task
  needs it; preserve existing document Guidance and source history. Do not bulk
  import task chatter or project Markdown.
- Defer direct Map bindings, assignment-local read telemetry, automatic graph
  reconciliation, and broad semantic extraction. A Map can be passed today as
  an explicit inherited handle.
- Treat `read_cost_bytes` as summary-card cost in the current contract, not body
  size. Consider clearer naming separately.
- Investigate bounded concurrency or batch retrieval for large explicit-card
  sets. The current composer fetches explicit cards sequentially, so callers
  should keep explicit selections small even though the schema maximum is
  higher.

## Authority rule

Knowledge owns reusable canonical content, revision, provenance, and curation
state. Guidance owns applicability and read policy. Maps own ordered navigation.
Tasks and messages own execution history. Local documents remain valid authored
sources or assemblies, but a manifest link never makes a copied body canonical.
