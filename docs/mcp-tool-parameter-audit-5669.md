# MCP tool parameter audit — task 5669

Date: 2026-07-11

## Decision

`get_task_context` now accepts exactly one argument:

```text
get_task_context(task_id)
```

The canonical task read is the authority for project scope. `project_id` is not
retained as an optional assertion because it duplicates canonical identity,
adds a mismatch mode, and encourages callers to perform a preliminary task
lookup.

## Audit method

The audit reviewed the 65-tool visible Den MCP discovery surface and the local
`den-services/mcp` registry, adapters, and routes. Parameters were classified
by whether they express:

- canonical identity that the backend can derive;
- a genuine list/search filter;
- a safety or concurrency control;
- response presentation only; or
- one of several mutually exclusive operation modes.

This audit records follow-up candidates. Task 5669 changes only
`get_task_context`.

## Findings

### 1. Canonical task IDs are accompanied by redundant project IDs

After `get_task_context`, the same shape remains on these visible tools:

- `get_latest_task_packet`
- `post_review_findings`
- `request_review`
- `split_review_findings_to_follow_up`
- `watch_github_checks`
- `get_github_check_gate`
- `wait_for_github_checks`
- deprecated `await_github_checks`

These tools should be reviewed as one migration family. If task IDs remain
global and canonical, each adapter can resolve the task first and derive the
project used by downstream routes. Removing `project_id` piecemeal without a
shared adapter pattern would duplicate composition logic and error handling.

### 2. `verbose` is repeated across most resource tools

The live surface exposes `verbose` on 38 of 65 tools. This is presentation
policy repeated in domain contracts, and it makes otherwise small calls look
more configurable than they are.

Recommended direction:

1. Keep concise responses as the canonical default.
2. Replace repeated `verbose` inputs with one intentional read-only
   `get_details(detail_ref)` escape hatch. Concise resource results provide the
   opaque detail reference when a deeper representation exists.
3. Resolve detail references through an explicit allowlist of read-only owning
   routes. Do not accept arbitrary nested tool arguments or dispatch mutations.
4. Remove `verbose` from normal discovery once the detail-reference coverage
   and compatibility inventory are complete.

The 38-tool inventory resolves as follows:

- Intentional detail references: `get_task`, `get_task_workflow_summary`,
  `get_document`, `get_thread`, `get_discussion_thread`,
  `get_document_discussion`, `get_latest_task_packet`, `list_review_rounds`,
  and `list_review_findings`.
- Concise collection/search operations: `search_documents`, `next_task`,
  `list_discussion_threads`, `list_tasks`, `list_documents`, `get_messages`,
  and `get_user_notifications`. Their returned item identities lead to the
  canonical resource get tool; bulk verbose expansion is deliberately absent.
- Mutation/command response toggles: the remaining 22 occurrences. These were
  response-presentation churn and are removed from discovery without a detail
  reference. Legacy callers may continue sending the hidden field during
  migration.

### 3. Conditional multi-mode tools should be split

`mark_notifications_read` combines mutually exclusive explicit-ID and scoped
mark-all modes behind optional parameters. The schema cannot make the valid
call shape obvious to a small model.

Recommended direction: expose separate tools for marking explicit notification
IDs and marking a canonical project/task scope. Each tool should have one
required identity shape and no mode switch.

The same audit found `get_document_discussion(create_if_missing=true)` mixing a
read with state creation. The read remains side-effect-free and an explicit
`ensure_document_discussion` tool owns default-thread creation.

### 4. Project-level tools with optional task filters need separate treatment

`send_message`, `get_messages`, and `query_librarian` legitimately support
project-level operation while also accepting task context. Their `project_id`
cannot simply be removed because a task is not always present.

Recommended direction: retain the project-scoped primitives, and consider
small task-scoped convenience tools only when usage evidence shows agents are
regularly supplying both canonical task and project IDs.

### 5. List/search filters are generally legitimate

Filters such as status, tags, visibility, limit, cursor, and time bounds change
the selected result set or bound work. They are not the same class of optional
parameter as redundant identity or response presentation. They should remain
unless a specific tool has an ambiguous default or an invalid combination.

## Suggested sequencing

1. Ship task 5669 and observe `get_task_context(task_id)` across the facade.
2. Create one follow-up for task-derived project scope across review packet and
   GitHub gate tools.
3. Create one follow-up for MCP response-presentation policy and the repeated
   `verbose` parameter.
4. Split `mark_notifications_read` modes in its owning service rather than in a
   facade-only shim.

## Follow-up: one rule for task-keyed tools (2026-11-01)

Agents kept guessing which task tools wanted `project_id`: `update_task` and
`get_task` take none, while `submit_task_for_review` still required it. Patch
asked for one rule, since the task already carries its project:

- Every tool that requires `task_id` takes no `project_id`.
  `submit_task_for_review` joins the derived family; the facade reads the
  canonical task and fills in the project before dispatch. Passing
  `project_id` to these tools is still rejected, with an error that says to
  omit it. The one exception is `submit_task_for_review`, which required it
  until now: sessions keep the schema they started with, so it accepts the
  task's own project and rejects any other.
- `send_message`, `get_messages` and `query_librarian` stay project-scoped,
  but `project_id` is optional when `task_id` is given: the facade derives it
  from the task. A call that names its project is dispatched unchanged, with
  no task read. This settles finding 4 without adding task-scoped variants.

## Follow-up: MCP keeps only the frequent tools (2026-11-02)

Patch set the rule: MCP `tools/list` holds the tools agents use all the time,
and everything else is reached through `den-tool`. Ten tools moved to the
long tail (still callable by exact name and as `den-tool den <operation>`):

- `request_review`, `watch_github_checks`, `wait_for_github_checks`:
  crew-review owns review routing and check gates and calls these by exact
  name; listing them invited agents to bypass `submit_task_for_review`.
- `get_document_discussion`, `comment_on_document`: 1 and 4 calls in the 30
  days before the change.
- `query_librarian`: 35 calls in 30 days. Testing found it keyword-matches
  stopwords, misses the obvious task for an exact-topic query, pads results
  with unrelated early tasks, answers a nonsense query with
  `confidence: high`, and returns tens of kilobytes per call.
- `send_message`, `get_messages`, `wait_for_messages`, `get_thread`: Den
  messages are legacy now that crew messaging reaches sessions directly.
  Services (crew-review's task-thread posts) keep calling them by name.

Den Board left discovery the same day (#10555). `tools/list` is now 20 tools
(19 in the managed-runtime profile).
