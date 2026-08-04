# Review Service Route Readiness

Task: 3696
Service: `den-services/review`
Schema: `den_review`
Role: `den_review_app`

Task #3696 implements the first review successor service from
[`docs/review-lifeboat-contract.md`](./review-lifeboat-contract.md).

No production MCP routes are flipped by this task. The service is ready for
local and staged verification, then a later explicit cutover task can update
`mcp/routes.example.yaml` and deployed MCP routing.

## Implemented Native Routes

- `POST /v1/projects/{project_id}/tasks/{task_id}/review/rounds`
- `POST /v1/projects/{project_id}/tasks/{task_id}/review/request`
- `GET /v1/projects/{project_id}/tasks/{task_id}/review/rounds`
- `GET /v1/projects/{project_id}/tasks/{task_id}/review/findings`
- `POST /v1/projects/{project_id}/tasks/{task_id}/review/findings/split-follow-up`
- `GET /v1/projects/{project_id}/tasks/{task_id}/review/workflow-summary`
- `POST /v1/projects/{project_id}/tasks/{task_id}/review/packets/validate`
- `POST /v1/projects/{project_id}/tasks/{task_id}/review/packets`
- `POST /v1/review/rounds/{review_round_id}/findings`
- `POST /v1/review/finalizations`
- `POST /v1/review/rounds/{review_round_id}/verdict`
- `POST /v1/review/findings/{finding_id}/response`
- `POST /v1/review/findings/{finding_id}/status`

## MCP Mapping Guidance

These Core-style review tools can route to `review` after import/parity
verification:

- `create_review_round`
- `list_review_rounds`
- `finalize_review` (normal `looks_good` / `changes_requested` closeout)
- `create_review_finding`
- `list_review_findings`
- `respond_to_review_finding`
- `set_review_finding_status`
- `request_review`
- `post_review_findings`
- `split_review_findings_to_follow_up`

`set_review_verdict` remains resolvable only as hidden compatibility behavior
for the exceptional `follow_up_needed` and `blocked_by_dependency` verdicts.
Normal reviewers must use `finalize_review`; it owns the canonical findings
packet and task transition as one retryable workflow.

The pointer-first review envelope and bounded wake/event contract are defined in
[`docs/review-pointer-first-contract.md`](./review-pointer-first-contract.md).
Normal managed submission is owned by Rusty Crew's `submit_task_for_review`;
Review supplies exact-round/SHA facts, packet/gate handles, and deterministic
receipts but does not own Crew routing or wake scheduling.

New Markdown packet tools can route here once accepted by MCP/tool docs:

- `validate_review_packet_markdown`
- `post_review_packet_markdown`
- `get_review_workflow_summary`

Do not route old legacy dispatch tools through this service. Review state is
conversation/task evidence; it must not create, claim, wake, retry, complete, or
cancel executable work.

## Operational Notes

The service validates project writability through `projects`, task/project
ownership and reviewable task states through `tasks`, and durable task-thread
packet records through `messages`. Missing upstream URLs fail closed.

Markdown packet validation rejects malformed front matter, wrong project/task,
invalid verdict/status/category values, stale reviewed head commits, and
unchecked required `verify` items before durable acceptance. Validation errors
include `field` and `docs_ref` values suitable for focused tool-documentation
lookup.

## Required Before Cutover

1. Apply `den_review` migrations and app-role bootstrap in a staging database.
2. Import/sync existing `den_core.review_rounds` and
   `den_core.review_findings`.
3. Backfill `project_id` from task ownership.
4. Compare Core and review-service read results for rounds, findings, verdicts,
   and workflow summaries.
5. Run representative request-review, finding lifecycle, verdict,
   split-to-follow-up, and Markdown packet validation/posting flows.
6. Verify messages side effects create task-thread packet evidence with
   compatible metadata.
7. Update MCP route mapping only in an explicit cutover task.

## Finalization Cutover And Recovery

`POST /v1/review/finalizations` accepts a review round, green-path verdict, and
decision identity. Review stores the verdict, reserves the canonical
`review_findings` packet, and creates the finalization record in one database
transaction. It then resumes three durable checkpoints:

1. append the packet through Messages;
2. transition the task through Tasks (`looks_good` to `done`,
   `changes_requested` to `in_progress`);
3. mark the finalization complete.

Retries with the same round, verdict, and `decided_by` resume the first
incomplete checkpoint. Messages deduplicates the canonical packet by
`review_packet_id`, and Review reads current task state before retrying a task
transition, so response loss does not create duplicate packet or task-history
evidence. A different verdict or decision identity for an already-finalized
round is a conflict.

Migration order is Messages migration 002, Review migration 005, then Review
and MCP rollout. Existing review rounds require no eager backfill. On first
finalization, Review adopts an existing round-scoped `review_findings` packet
when present; otherwise it creates one. `post_review_findings` remains an
advanced repair/repost operation and also reuses that packet after the task has
left `review`.

Rollback is application-first: restore MCP discovery/routing and the prior
Review/Messages binaries while leaving both additive schema objects in place.
Do not drop `review_finalizations` or the Messages idempotency index until every
row is `complete` and no rollback binary is writing review packets. If a
finalization is incomplete, retry the same request after restoring the
successor binaries; do not manually replay its packet or task transition.
