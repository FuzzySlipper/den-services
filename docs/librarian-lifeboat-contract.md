# Librarian Lifeboat Contract

This is the extraction contract for `query_librarian`, the cross-domain
retrieval aggregator over project/task context, documents, messages, and
knowledge. It follows the projects, tasks, messages, documents, knowledge, and
guidance contracts: librarian owns query orchestration, source normalization,
ranking, response budgeting, and citation shape. It does not own the source
records it retrieves.

The successor service is:

- Go package/service: `den-services/librarian`
- Postgres schema: none for V1 source data ownership
- Runtime role: `den_librarian_app`
- Primary tables: none in V1
- Project authority: `den-services/projects`, checked through scope APIs or an
  approved projects read projection before querying project-scoped sources
- Source authorities: `den-services/tasks`, `den-services/messages`,
  `den-services/documents`, and `den-services/knowledge`
- Deployment pattern: the lifeboat substrate in
  [`docs/lifeboat-service-substrate.md`](./lifeboat-service-substrate.md)

Do not create a placeholder service only to satisfy a smoke test. Register
`librarian` in `deployment/services.yaml` only when the real retrieval contract
is implemented.

## Boundary Statement

`librarian` is a read aggregator. It receives a natural-language query, gathers
bounded candidate context from source services, normalizes those candidates into
cited source records, optionally asks a configured model or deterministic
reranker to select the most relevant items, and returns a bounded answer with
recommendations.

It must not become a hidden write owner. V1 librarian does not insert, update,
delete, archive, hide, or curate tasks, messages, documents, knowledge entries,
agent guidance, review packets, or worker records. It does not mutate read
state, notifications, task status, document visibility, knowledge curation
state, or guidance entries.

This service does not own:

- project/space lifecycle or archived-scope write policy;
- task lifecycle, dependencies, history, or review workflow;
- message storage, notification feed, read state, or packet storage;
- document content, document search implementation, discussion threads, or
  document visibility;
- knowledge entry storage, curation, revisions, or extractive guide behavior;
- agent guidance entries or resolved guidance packets;
- model provider infrastructure outside the single configured completion or
  reranking call used to produce the librarian response.

Librarian may own transient in-memory ranking state for one request. Any future
durable query logs, feedback, embeddings, or cached indexes are a separate
contract because they create a new write surface.

## Existing Core Inventory

Core librarian ownership is currently centered in:

- `LibrarianGatherer`
- `LibrarianService`
- `LibrarianResponse`
- `RelevantItem`
- `LibrarianRoutes`
- `LibrarianTools`

Current REST behavior:

- `POST /api/projects/{projectId}/librarian/query`
- request fields:
  - `query`
  - `task_id`
  - `include_global`, default `true`
- returns `400` when the LLM endpoint is not configured;
- returns `404` when a supplied `task_id` is not found;
- returns `400` when `task_id` belongs to a different project;
- returns a JSON librarian response on success.

Current MCP tool:

- `query_librarian(project_id, query, task_id = null, include_global = true)`

Current Core gathering behavior:

- Task context has highest priority and is gathered first when `task_id` is
  supplied.
- `task_id` is optional. When omitted, librarian still queries project-level
  document, message, and knowledge context without task enrichment.
- Task enrichment includes task detail, parent task, subtasks, dependencies, and
  recent task messages.
- Document search uses a sanitized FTS query built from the natural-language
  query plus task title and task tags when task context is present.
- Document search includes project documents and, when `include_global=true`,
  `_global` documents.
- Recent project messages are gathered after documents and are truncated first.
- Gathered context is bounded by a configured context token budget.
- If no context is gathered, Core returns an empty low-confidence response
  without calling the LLM.

Current response shape:

```json
{
  "relevant_items": [
    {
      "type": "task|document|message",
      "source_id": "exact ID from context",
      "project_id": "project ID if applicable",
      "summary": "what this item contains",
      "why_relevant": "why it matters for the query",
      "snippet": "specific supporting passage"
    }
  ],
  "recommendations": ["actionable suggestion"],
  "confidence": "high|medium|low"
}
```

Current model prompt instructs the model not to invent information outside the
gathered context. The successor must preserve that cited-source posture and
tighten it by making citations structured before any model call.

Core does not currently gather knowledge entries for `query_librarian`; the
successor contract intentionally adds knowledge as a source because the
knowledge service now owns reviewed, citation-backed reusable reference
material.

## Ownership Decision

Librarian is its own service.

It is not part of `documents`, because it must search tasks, messages, and
knowledge. It is not part of `knowledge`, because knowledge is global curated
reference material and must not become project/task/message retrieval. It is
not part of `guidance`, because guidance resolves configured policy documents
and tool docs; librarian answers ad hoc natural-language queries over multiple
source domains.

The service owns:

- request validation for `query_librarian`;
- source selection and per-source budget allocation;
- calls to source APIs or approved read projections;
- source normalization into a common citation shape;
- ranking and reranking;
- final response validation and budgeting;
- MCP compatibility formatting.

The source services own:

- storage and mutation of their source records;
- their search indexes and visibility policy;
- source-specific authorization/read policy;
- source-specific ranking signals that only the producer can compute safely.

## No Hidden Write Ownership

V1 librarian has no domain tables.

If a runtime role is needed for database access, it receives only `SELECT` on
named read projections explicitly granted by producer schemas. It receives no
`INSERT`, `UPDATE`, `DELETE`, `CREATE`, or `ALTER` grants. If an implementation
uses only source HTTP APIs, the role may have no database grants at all.

Forbidden V1 writes:

- no task updates or task history writes;
- no message writes, notifications, read marks, or packet appends;
- no document writes, discussion comments, visibility changes, or archive
  preflight mutations;
- no knowledge entry writes, revision writes, status changes, or curation
  changes;
- no guidance entry writes or resolved guidance storage;
- no durable query logs, feedback records, embeddings, vector indexes, or cache
  tables.

If a future task needs embeddings, query feedback, or durable retrieval caches,
that task must define a new schema, retention policy, and source-of-truth
boundary. It must not be smuggled into the initial librarian service.

## Source Contracts

Librarian may use either HTTP APIs or approved read projections. Whichever path
is chosen for implementation, the producer service owns the contract and tests
the projection shape. Librarian must never read raw producer tables.

### Projects

Required scope reads:

- `GET /v1/scopes/{scope_id}`
- optional approved projection: `den_projects.librarian_scopes`

Required fields:

- `id`
- `name`
- `kind`
- `visibility`
- `archived_at` or equivalent archived indicator when available

Rules:

- The requested `project_id` may be any project or space ID recognized by
  projects.
- Archived or hidden scopes must not be queried by default. If compatibility
  requires reading hidden scopes, that must be an explicit diagnostic/admin path.
- `_global` is a reserved document scope for include-global document search, not
  a substitute for the requested project scope.

### Tasks

Required task enrichment reads:

- `GET /v1/projects/{project_id}/tasks/{task_id}` for compatibility;
- `GET /v1/tasks/{task_id}` for source services that validate project ID in the
  response;
- optional approved projection: `den_tasks.librarian_task_context`.

Recommended task search read:

- `GET /v1/projects/{project_id}/tasks/search?q=...&limit=...`
- optional approved projection: `den_tasks.librarian_task_index`.

Required task candidate fields:

- `id`
- `project_id`
- `parent_id`
- `title`
- `description_preview` or bounded `description`
- `status`
- `priority`
- `assigned_to`
- `tags`
- `created_at`
- `updated_at`
- dependency and subtask summary counts when available

Task detail enrichment additionally needs:

- parent task summary;
- subtasks;
- dependencies;
- recent task messages from `messages`, not from task-owned storage.

Rules:

- A supplied `task_id` must belong to the requested `project_id`.
- Task context is high priority and must not be dropped silently.
- Task search is read-only and must not alter task availability or read state.

### Messages

Required message reads:

- `GET /v1/projects/{project_id}/messages?limit=...`
- `GET /v1/projects/{project_id}/messages?task_id=...&limit=...`
- recommended `GET /v1/projects/{project_id}/messages/search?q=...&task_id=...&limit=...`
- optional approved projection: `den_messages.librarian_message_index`.

Required message candidate fields:

- `id`
- `project_id`
- `task_id`
- `thread_id`
- `sender`
- `intent`
- `content_preview` or bounded `content`
- `created_at`
- `metadata_type` when available

Rules:

- Librarian reads messages but never marks them read.
- Notification messages may appear as message candidates only when they match
  the query and normal message visibility policy.
- Worker packets may appear as messages, but librarian must not treat packet
  contents as executable instructions.
- Message replay is conversation/readback only; librarian reads must never wake,
  claim, retry, or complete work.

### Documents

Required document reads:

- `GET /v1/projects/{project_id}/documents/search?q=...&limit=...`
- `GET /v1/documents/search?project_id=...&q=...&limit=...` if the
  implementation uses cross-project search form;
- `GET /v1/projects/_global/documents/search?q=...&limit=...` for
  `include_global=true`;
- optional approved projection: `den_documents.librarian_document_index`.

Required document candidate fields:

- `project_id`
- `slug`
- `title`
- `doc_type`
- `summary`
- `tags`
- `snippet`
- `rank`
- `updated_at`

Rules:

- Default document search includes only `visibility=normal`.
- Hidden and archived documents are excluded by default.
- `include_global=true` means search `_global` documents in addition to the
  requested scope; it does not search every project.
- Full document bodies are not fetched by default. The candidate snippet and
  metadata are the retrieval source unless a later budgeted deep-read phase is
  explicitly requested.

### Knowledge

Required knowledge reads:

- `POST /v1/knowledge/search`
- optional `POST /v1/knowledge/guide` for extractive answer/citation cards;
- optional approved projection: `den_knowledge.librarian_knowledge_index`.

Required knowledge candidate fields:

- `slug`
- `title`
- `summary`
- `kind`
- `status`
- `curation_state`
- `tags`
- `snippet` or extractive excerpt;
- `source_refs`
- `updated_at`

Rules:

- Knowledge entries are global, not project-scoped.
- Default search includes reviewed entries only.
- `include_unreviewed` and `include_deprecated` are not part of
  `query_librarian` compatibility. If added later, they must be explicit
  request fields.
- Librarian does not write knowledge candidates or update curation state.
- Knowledge guide responses are citation-backed and extractive. Librarian may
  include them as candidates, but must not rewrite them into uncited claims.

## Candidate Model

All source results are normalized before ranking or model calls.

```json
{
  "source": {
    "kind": "task|document|message|knowledge",
    "id": "stable source ID",
    "project_id": "scope when applicable",
    "uri": "den://documents/den-services/slug"
  },
  "title": "short display title",
  "summary": "bounded source summary",
  "snippet": "bounded supporting excerpt",
  "tags": ["optional", "source", "tags"],
  "score": 0.42,
  "updated_at": "2026-06-29T00:00:00Z",
  "metadata": {}
}
```

Stable source ID formats:

- task: `task:{project_id}:{task_id}`
- document: `document:{document_project_id}:{slug}`
- message: `message:{project_id}:{message_id}`
- knowledge: `knowledge:{slug}`

Human-readable compatibility IDs may still appear in MCP output:

- task `#47`;
- document `den-services/guidance-lifeboat-contract`;
- message `msg#123`;
- knowledge `knowledge:service-topology`.

The normalized candidate is the only thing the final model prompt may see. It
must include enough citation metadata that the model cannot invent source
identifiers.

## Retrieval Flow

V1 flow:

1. Validate the request and scope.
2. If `task_id` is supplied, load task detail and verify project ownership.
3. Build a sanitized search query from the natural-language query plus task
   title and task tags when available.
4. Gather task candidates.
5. Gather document candidates from project scope and `_global` when requested.
6. Gather message candidates from recent project/task messages and message
   search when available.
7. Gather reviewed knowledge candidates.
8. Normalize candidates into the common candidate model.
9. Apply source budgets and deterministic dedupe.
10. Rank or rerank candidates.
11. Build a bounded response with citations, recommendations, and confidence.

Fallback behavior:

- If a source service is unavailable, librarian should return partial results
  with `warnings` rather than fail the whole query, unless the unavailable
  source is required for task ownership validation.
- If `task_id` validation fails, return a typed error and do not query other
  sources.
- If no candidates are gathered, return an empty low-confidence response and do
  not call the LLM.
- If the LLM is not configured, return a typed configuration error for
  compatibility or use an explicitly configured deterministic extractive mode.

## Ranking And Budgets

Budgets are configuration, not code constants:

- total source candidate budget;
- per-source candidate limits;
- maximum candidate snippet bytes;
- model context budget;
- response item limit;
- response recommendation limit;
- maximum response bytes;
- per-source timeout;
- total request timeout.

Recommended initial defaults:

- total gathered context: 8,000 estimated tokens, matching Core compatibility;
- task enrichment: highest priority;
- documents and knowledge: medium priority;
- recent messages: lowest priority and truncated first;
- maximum response relevant items: 10;
- maximum recommendations: 5.

Budget behavior:

- Task ownership validation is never skipped.
- Task enrichment is not silently dropped. If it cannot fit, return a truncation
  warning and include a compact task summary.
- Recent messages are truncated before documents, knowledge, or task context.
- Long snippets are truncated at line or sentence boundaries when possible.
- The response includes `budget` metadata so callers can see when source
  candidates were omitted.

## Response Contract

Native response:

```json
{
  "query": "natural-language query",
  "project_id": "den-services",
  "task_id": 3643,
  "include_global": true,
  "relevant_items": [
    {
      "source": {
        "kind": "document",
        "id": "document:den-services:librarian-lifeboat-contract",
        "project_id": "den-services",
        "uri": "den://documents/den-services/librarian-lifeboat-contract"
      },
      "summary": "what this item contains",
      "why_relevant": "why it matters for the query",
      "snippet": "specific supporting passage",
      "score": 0.91
    }
  ],
  "recommendations": ["actionable suggestion"],
  "confidence": "high",
  "warnings": [],
  "budget": {
    "context_token_budget": 8000,
    "estimated_context_tokens": 3120,
    "truncated": false,
    "omitted_candidate_counts": {
      "task": 0,
      "document": 0,
      "message": 4,
      "knowledge": 0
    }
  }
}
```

Compatibility MCP output may keep the current fields:

- `relevant_items`
- `recommendations`
- `confidence`

When using the compatibility shape, each `relevant_items[]` entry still needs
source attribution:

- `type`
- `source_id`
- `project_id` when applicable
- `summary`
- `why_relevant`
- `snippet`

Native responses may add `source`, `score`, `warnings`, and `budget`; MCP
compatibility must not drop citation fields.

Confidence values:

- `high`
- `medium`
- `low`

Validate model output. If the model returns malformed JSON, the service may use
the Core-style fallback of treating raw text as a low-confidence
recommendation, but it must not fabricate relevant item citations.

## Service API

Native routes:

- `POST /v1/projects/{project_id}/librarian/query`
- `POST /v1/librarian/query`

Project-scoped route request:

```json
{
  "query": "I'm starting work on task 3643",
  "task_id": 3643,
  "include_global": true,
  "source_limits": {
    "tasks": 5,
    "documents": 8,
    "messages": 20,
    "knowledge": 5
  }
}
```

Unscoped route request includes `project_id` in the body and exists for clients
that cannot route by path. The path-scoped route is preferred.

Error responses use the shared error envelope. Required codes:

- `librarian_not_configured`
- `scope_not_found`
- `scope_not_readable`
- `task_not_found`
- `task_project_mismatch`
- `invalid_query`
- `source_timeout`
- `source_unavailable`
- `invalid_model_response`

`source_timeout` and `source_unavailable` are warnings for optional sources and
errors only when the source is required for validation.

## MCP Mapping

Existing MCP tool:

- `query_librarian(project_id, query, task_id = null, include_global = true)`

Mapping:

- `project_id` maps to `POST /v1/projects/{project_id}/librarian/query`.
- `query` maps to request `query`.
- `task_id` maps to request `task_id`.
- `include_global` maps to request `include_global`.

MCP tool description should continue to say that librarian returns relevant
tasks, documents, and messages with source attribution and recommendations. It
should be updated to mention knowledge once the successor route is live.

MCP compatibility constraints:

- Do not route `den_knowledge_*` tools through librarian.
- Do not route `search_documents`, `get_document`, or document discussion tools
  through librarian.
- Do not route `get_agent_guidance` or `tool_docs` through librarian.
- Do not route task/message write tools through librarian.
- Do not expose a librarian tool that writes query results back to tasks,
  messages, documents, or knowledge.

## Cross-Service Read Posture

Preferred implementation order:

1. Use producer HTTP APIs where they already expose bounded search/read routes.
2. Add producer-owned read projections where HTTP APIs would force expensive
   N+1 calls or expose unstable raw table shapes.
3. Grant `SELECT` on those projections to `den_librarian_app`.
4. Add compatibility tests that compare projection shapes with librarian
   expectations.

Approved projections must be named for librarian consumption:

- `den_tasks.librarian_task_context`
- `den_tasks.librarian_task_index`
- `den_messages.librarian_message_index`
- `den_documents.librarian_document_index`
- `den_knowledge.librarian_knowledge_index`

Projection rules:

- Producer schemas define the views in their own migrations.
- Librarian never creates or edits producer views.
- View changes are contract changes.
- Views expose bounded, display-safe fields and do not include secrets,
  full packet payloads by default, hidden document bodies, archived document
  bodies, or unreviewed knowledge unless explicitly requested by a future
  contract.
- Projection reads cannot drive execution. A librarian query must not create,
  claim, wake, retry, complete, or cancel work.

## Migration And Cutover

Cutover steps:

1. Ensure projects, tasks, messages, documents, and knowledge expose required
   read APIs or approved librarian projections.
2. Implement `den-services/librarian` with no domain tables.
3. Add config for source URLs, budgets, timeouts, and model/reranker mode.
4. Run fixture smokes against seeded task, document, message, and knowledge
   sources.
5. Run parity reads against Core for task enrichment, document include-global,
   recent messages, and malformed model fallback.
6. Switch MCP `query_librarian` to successor backing.
7. Keep source write tools on their owning services.

Rollback:

- Point MCP `query_librarian` back to Core.
- No source data rollback is needed because librarian V1 owns no source writes.
- If projection grants caused issues, revoke `SELECT` grants from
  `den_librarian_app`; source services continue operating.

## Smoke Tests

Required smokes before cutover:

- task enrichment: seeded task with parent, dependency, subtask, tags, and task
  messages appears with stable task citations;
- task project mismatch: querying project A with project B's task ID fails
  before other sources are queried;
- document retrieval: project document search returns a cited document snippet;
- include-global: `include_global=true` includes `_global` document candidates
  and `include_global=false` excludes them;
- message retrieval: recent or searched project messages return cited message
  snippets without changing read state;
- knowledge retrieval: reviewed knowledge entry appears as a cited knowledge
  source; draft/unreviewed entries are excluded by default;
- mixed-source answer: one query returns at least one task, document, message,
  and knowledge candidate with source attribution and bounded recommendations;
- budget truncation: seeded over-budget messages are truncated first and budget
  metadata reports omitted message candidates;
- source degradation: an optional source timeout returns partial results with a
  warning;
- no hidden writes: after a librarian query, fixture counts for tasks, messages,
  documents, knowledge entries, guidance entries, and read-state rows are
  unchanged.

Recommended haunt-regression cases:

- reading worker packet messages through librarian does not create or claim
  delivery work;
- replaying the same librarian query does not append messages or notifications;
- hidden and archived documents do not appear in default results;
- unreviewed/deprecated knowledge does not appear without an explicit future
  option;
- malformed model JSON cannot fabricate source citations.

## Non-Goals

- Do not build durable embeddings or vector indexes in V1.
- Do not write query logs or feedback rows in V1.
- Do not replace knowledge guide or document search with librarian.
- Do not turn librarian into agent guidance resolution.
- Do not use librarian as a task/message/document write convenience endpoint.
- Do not make source services expose raw tables for librarian convenience.
