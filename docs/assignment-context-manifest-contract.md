# Assignment Context Manifest Contract

Task #7426 adds `compose_assignment_manifest` at the MCP composition boundary.
It is a read-only, per-assignment projection: it creates no rows, caches no
bodies, and does not become a Knowledge, Guidance, or Librarian authority.

## Input and authority

`task_id` is canonical and is resolved first through Tasks; its project scope
is never supplied by the caller. `assignment` is required and separate from
optional `background`. Optional `agent_profile` and `capabilities` become
Guidance applicability scopes. Explicit Knowledge slugs and inherited handle
cards are orchestrator selections, not new bindings.

The composer calls Guidance `context-resolve`, asks Librarian a bounded
assignment query, and reads explicit Knowledge metadata only. It never reads
or returns a Knowledge/document body. Missing optional services produce an
honest `source_status` entry; a missing canonical task remains an error.

## Projection

The structured response includes compact references and Markdown. Each
Knowledge reference is stable as `knowledge:<slug>` and carries title, intrinsic
summary, kind, authority and authority state, tags, revision when known (otherwise update marker),
estimated summary reading cost, selection source, read policy, and `read_when`.
Document and Librarian cards use their own stable prefixed references.

References are deduplicated by stable reference and ordered deterministically:
must-read, explicit/inherited task-local, relevant Guidance, then Librarian
nearby maps; ties use source precedence, priority, sort order, and reference.
Non-must-read Guidance cards are included only when their card metadata is
lexically relevant to the assignment/background. This keeps narrow sibling
assignments from inheriting every parent handle. `max_handles` (1–24),
`inline_budget` (0–4096, passed to Guidance), and `librarian_items` (0–8) are
bounded inputs. Markdown is a concise projection, with instructions and
optional background visibly separate; it never contains entry bodies.
The raw request is capped at 64 KiB, assignment and background are each capped
at 16 KiB, shared backend reads are capped at 2 MiB, and the final manifest is
capped at 128 KiB. Librarian suggestions obey one aggregate item ceiling and
warnings surface as a partial source status.
