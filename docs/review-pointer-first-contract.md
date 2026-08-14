# Den review current-state contract

This document defines the bounded review envelope shared by Den Review, MCP,
managed runtimes, and direct review clients. A review pointer identifies the
task, current review round, repository, and evidence handles. It does not ask a
reviewer to reconstruct an earlier checkout.

The Den document `den-services/review-pointer-first-contract` is the canonical
guidance source. This checked-in contract carries the implementation-facing
boundary and must remain faithful to that document.

## Normal path

```text
managed implementer   submit_task_for_review | rusty_crew.submit_task_for_review
external implementer  role-bound review:cli submit/status
direct implementer    request_review
reviewer              get_review_context + inspect the current checkout
managed closeout      complete_routed_review | rusty_crew.complete_routed_review
direct closeout       finalize_review + compact receipt
```

Managed and direct submission are different authorities. A reviewer must use
the routed submission record when one exists and must not recreate it by
calling low-level tools one at a time. `request_review` is the direct fallback.

Review packets and messages point to durable task, round, finding, and
repository records. They are not copies of source code or checkout identity.
The reader resolves those pointers and examines the current repository state
when the review is handled. A run timestamp, task/review identifiers, and the
retained evidence handles are enough to reconstruct what was known at the
time.

## Ownership

| Concern | Authority | Boundary |
| --- | --- | --- |
| review rounds, findings, finalization, receipts | Den Review | Review REST/MCP APIs and `den_review` schema |
| managed submission, session identity, wake coalescing, routed reply | Rusty Crew | Crew-owned workflow and adapter calls |
| model-facing procedure | Codex skill | Guidance only |
| CI execution and check-run identity | GitHub Actions / Review gate | Separate deterministic gate APIs |
| human-readable task-thread projection | Messages | Idempotent packet/message append |
| task status/history | Tasks | Conditional transitions |

The separate GitHub check-gate APIs may accept a `commit_sha` when a caller
explicitly needs deterministic CI evidence. That field belongs to the gate
operation, not to review-round creation, review packets, campaign snapshots,
reviewer context, or finalization paperwork.

## Review pointer

The pointer contains only the current review context:

```json
{
  "schema": "den_review.review_pointer.v1",
  "schema_version": 1,
  "workflow_key": {
    "project_id": "den-services",
    "task_id": 6604,
    "review_round_id": 123,
    "correlation_id": "review-6604-123"
  },
  "state_revision": 1,
  "material_digest": "content:canonical-semantic-json",
  "repository": "FuzzySlipper/den-services",
  "root_path": "/home/dev/den-services",
  "branch": "main",
  "base_branch": "main",
  "handles": {
    "request_packet_id": 789,
    "request_message_id": 790,
    "detail_ref": "opaque-review-request-detail"
  },
  "correlation": {
    "agent_instance_id": "crew-a",
    "session_key": "session-abc",
    "reply_target": "session-abc"
  }
}
```

`root_path` and branch labels are useful checkout hints, not authority. The
workflow key prevents duplicate delivery and identifies the review round that
must still be current. `state_revision` is the envelope's own monotonic state
revision; it is not a source-control revision. `material_digest` is a bounded
semantic-content digest used for idempotency and does not identify a checkout.

The request packet carries the requested-by actor, task/review identifiers,
optional branch context, tests run, notes, and evidence handles. It does not
carry base/head source-control identifiers or diff substitutions.

## Reviewer context

`den_review.reviewer_context.v1` is read-only and bounded. It contains:

- task ID, project, title, status, repository, and root handle;
- the current round number, target kind, branch context, campaign children, and
  named campaign repositories;
- current and prior finding IDs, keys, categories, statuses, concise summaries,
  and detail handles;
- packet headers and guidance handles, not full packet or guidance bodies;
- an explicit `next_state` such as `source_review_ready`, `gate_pending`,
  `gate_failed`, `round_superseded`, or `not_reviewable`;
- source status and truncation metadata.

When no current round exists, return the typed
`review_context_unavailable` result with a reason such as
`no_current_round`. Do not substitute a generic task briefing.

Reviewers inspect the current checkout after receiving this context. They do
not wait for a packet to contain a source revision, compare a campaign's
repository entries to child source revisions, or reproduce an old packet
before beginning review.

## Finalization and replay

`den_review.completion_receipt.v1` returns the durable result:

```json
{
  "schema": "den_review.completion_receipt.v1",
  "schema_version": 1,
  "workflow_key": {
    "project_id": "den-services",
    "task_id": 6604,
    "review_round_id": 123,
    "correlation_id": "review-6604-123"
  },
  "state_revision": 2,
  "material_digest": "content:canonical-semantic-json",
  "verdict": "looks_good",
  "task_status": "done",
  "finding_statuses": [],
  "handles": {"finalization_id": 800, "packet_id": 789, "message_id": 801},
  "reason": "complete",
  "retry": {"safe": true, "same_request_is_idempotent": true}
}
```

An identical retry returns the same IDs and statuses. A different normalized
verdict or finding result for a committed round is a typed conflict. The
finalization lock proves that the round and task state are still current; it
does not compare source-control identifiers.

Routed results carry the same task/round/correlation workflow key, state, gate
status when a separate gate exists, and packet/message handles. Reply targets
come from trusted managed-runtime identity and are never model-supplied.

## Campaigns

Campaign reconciliation is a current-state review of named repositories and
approved child review rounds. Each repository entry contains only its name.
Each child entry identifies its project, task, and review-round record. The
service still validates membership, latest-round status, approval, duplicate
entries, and unresolved blocking/acceptance findings. See
[`campaign-reconciliation-reviews.md`](campaign-reconciliation-reviews.md).

## Budgets and recovery

- bounded request body: reject oversized packets rather than silently
  truncating them;
- reviewer context: retain deterministic field ordering, truncation metadata,
  opaque detail references, and current-state pointers;
- completion receipt: compact by default, with bounded finding details;
- unchanged pending/status events: coalesce by workflow key and state revision.

The pointer-first flow is recoverable after compaction or process restart from
Den task/review reads, the run timestamp, and the retained handles. Repeat a
submission only after a pre-persistence validation rejection. After persistence,
reconcile the same review round and submission instead of issuing a new one.

## Verification

The fixtures in `review/testdata/pointer-first/` cover initial review,
rereview, gate failure, and superseded-round states. They verify bounded
workflow keys, semantic content digests, UTF-8-safe budgets, event coalescing,
stable finalization identities, and stale-round protection. GitHub gate tests
separately verify deterministic check-run matching.

Breaking changes to identity, ownership, or replay semantics require a new
envelope version and an explicit compatibility plan.
