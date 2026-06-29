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
- `POST /v1/review/rounds/{review_round_id}/verdict`
- `POST /v1/review/findings/{finding_id}/response`
- `POST /v1/review/findings/{finding_id}/status`

## MCP Mapping Guidance

These Core-style review tools can route to `review` after import/parity
verification:

- `create_review_round`
- `list_review_rounds`
- `set_review_verdict`
- `create_review_finding`
- `list_review_findings`
- `respond_to_review_finding`
- `set_review_finding_status`
- `request_review`
- `post_review_findings`
- `split_review_findings_to_follow_up`

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
