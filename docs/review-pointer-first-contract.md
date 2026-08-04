# Pointer-first review contract

This document defines the runtime-neutral review envelope used by the normal
Den review workflow. It is a contract between Den Review/MCP, managed runtimes
such as Rusty Crew, and procedural clients such as the Codex `den-review`
skill. It is not a second review authority and it does not make a distributed
transaction claim.

## Normal green path

```text
managed implementer  submit_task_for_review
direct implementer    request_review
reviewer              get_review_context
reviewer              inspect the exact diff and focused probes
reviewer              finalize_review
routed closeout       complete_routed_review
manual closeout       return the compact receipt in chat
```

`submit_task_for_review` is the managed Rusty Crew entry point. `request_review`
is the direct/unmanaged Den fallback. Neither path is a synonym for the other,
and a reviewer must not reconstruct a submission by calling low-level tools
one at a time during normal work.

## Ownership

| Concern | Authority | Boundary |
| --- | --- | --- |
| review rounds, findings, gates, finalization, receipts | Den Review | Review REST/MCP APIs and `den_review` schema |
| managed submission, session identity, wake coalescing, routed reply | Rusty Crew | Crew-owned durable workflow and adapter calls |
| model-facing procedure | Codex skill | Guidance only; never an authority boundary |
| CI execution | GitHub Actions | Exact-SHA check runs and URLs |
| human-readable task-thread projection | Messages | Idempotent packet/message append; no lifecycle ownership |
| task status/history | Tasks | Conditional transitions; Review never writes task storage |

Review packets and messages are pointers to evidence, not substitutes for the
owning records. A pointer may be stale; the reader must resolve it against the
current exact round/SHA before acting.

## Versioned envelopes

All model-facing/adapter-facing envelopes use explicit, independently
versioned schema identifiers. The version suffix is part of the compatibility
contract; `schema_version` is retained for generic envelope tooling.

```json
{
  "schema": "den_review.<envelope_kind>.v1",
  "schema_version": 1,
  "workflow_key": {
    "project_id": "den-services",
    "task_id": 6604,
    "review_round_id": 123,
    "head_commit": "0123456789abcdef0123456789abcdef01234567",
    "correlation_id": "review-6604-123-01234567"
  },
  "revision": 7,
  "material_digest": "sha256:canonical-semantic-json",
  "handles": {},
  "state": ""
}
```

`workflow_key` is the idempotency and staleness identity. `revision` increases
only when the material state represented by the envelope changes. The
`material_digest` is SHA-256 over canonical, generated-time-free semantic JSON;
map ordering and timestamps must not affect it. Replaying an identical
envelope with the same key, revision, and digest is a no-op. A new exact SHA,
round, or correlation is a new workflow; it must not reuse the old workflow's
wake or reply.

### Review request envelope

`den_review.review_pointer.v1` contains only the fields needed to accept work.
Managed runtimes may wrap it in their own durable request envelope (for Rusty
Crew, `rusty_crew.routed_review_request.v1`) with submission identity, expiry,
and runtime-owned correlation. The model never supplies the recipient or
correlation target.

```json
{
  "schema": "den_review.review_pointer.v1",
  "schema_version": 1,
  "workflow_key": {
    "project_id": "den-services",
    "task_id": 6604,
    "review_round_id": 123,
    "head_commit": "0123456789abcdef0123456789abcdef01234567",
    "correlation_id": "crew-session-abc"
  },
  "revision": 1,
  "material_digest": "sha256:canonical-semantic-json",
  "repository": "FuzzySlipper/den-services",
  "root_path": "/home/dev/den-services",
  "ref": "main",
  "base_commit": "fedcba9876543210fedcba9876543210fedcba98",
  "delta_base_commit": "fedcba9876543210fedcba9876543210fedcba98",
  "gate": {
    "id": 456,
    "status": "pending",
    "required_checks": ["Verify Offline"],
    "terminal_reason": ""
  },
  "handles": {
    "request_packet_id": 789,
    "request_message_id": 790,
    "detail_ref": "opaque-review-request-detail"
  },
  "correlation": {
    "agent_instance_id": "crew-a",
    "session_key": "session-abc",
    "reply_target": "session-abc"
  },
  "risk_focus": "Review the exact MCP profile boundary and pointer budget."
}
```

`root_path` is an optional checkout hint, not repository authority; the
canonical repository identity is the repository/ref/exact SHA tuple. The
meaning of `delta_base_commit` is the prior reviewed head when one exists,
otherwise the submitted base. `review_summary_md`, test logs, full guidance bodies, and full packet Markdown
do not belong in this envelope. They remain in the canonical packet and are
opened by handle when a reviewer needs them.

### Reviewer context envelope

`den_review.reviewer_context.v1` is read-only and derives all identity and exact
revision fields from Den authorities. It contains:

- task ID, project, title, status, and repository/root handle;
- current round and exact branch/base/head/delta metadata;
- current-round and prior finding IDs, keys, categories, statuses, concise
  summaries, and evidence/detail handles;
- exact-SHA gate ID, status, required checks, terminal reason, and evidence
  handles;
- request/implementation packet headers and opaque detail refs;
- required guidance handles, not guidance bodies;
- `next_state` (`source_review_ready`, `gate_pending`, `gate_failed`,
  `round_superseded`, or `not_reviewable`);
- `source_status` and `truncation` metadata.

The context must never silently substitute a generic task briefing when no
current review round exists. It returns a typed `review_context_unavailable`
error with a reason such as `no_current_round`, `round_superseded`, or
`task_not_reviewable`.

### Completion receipt

`den_review.completion_receipt.v1` is returned by `finalize_review` and contains
the durable result, not a copy of the packet:

```json
{
  "schema": "den_review.completion_receipt.v1",
  "schema_version": 1,
  "workflow_key": {
    "project_id": "den-services",
    "task_id": 6604,
    "review_round_id": 123,
    "head_commit": "0123456789abcdef0123456789abcdef01234567",
    "correlation_id": "review-6604-123-01234567"
  },
  "revision": 2,
  "material_digest": "sha256:canonical-semantic-json",
  "verdict": "looks_good",
  "task_status": "done",
  "finding_statuses": [],
  "gate": {"id": 456, "status": "passed", "terminal_reason": "checks_passed"},
  "handles": {"finalization_id": 800, "packet_id": 789, "message_id": 801},
  "reason": "complete",
  "retry": {"safe": true, "same_request_is_idempotent": true}
}
```

An identical retry returns the same IDs and statuses. A different normalized
verdict or finding result for a committed round is a typed conflict, not a new
finalization. Actionable findings may expand the receipt beyond the normal
budget, but finding IDs, statuses, and evidence handles remain bounded and
deterministic.

### Routed result / wake envelope

`rusty_crew.routed_review_result.v1` is the adapter handoff between Den Review and a
managed runtime. Den supplies facts; Rusty Crew decides whether/how to wake or
reply:

```json
{
  "schema": "rusty_crew.routed_review_result.v1",
  "schema_version": 1,
  "workflow_key": {
    "project_id": "den-services",
    "task_id": 6604,
    "review_round_id": 123,
    "head_commit": "0123456789abcdef0123456789abcdef01234567",
    "correlation_id": "crew-session-abc"
  },
  "revision": 3,
  "material_digest": "sha256:canonical-semantic-json",
  "state": "gate_failed",
  "gate": {"id": 456, "status": "failed", "terminal_reason": "required_checks_missing"},
  "handles": {"gate_event_id": 900, "packet_id": 789, "message_id": 901},
  "reply_target": "opaque-runtime-correlation",
  "action": "wake_submitter",
  "reason": "required_checks_missing"
}
```

The reply target is resolved by Rusty Crew from trusted persisted identity; it
is never model-supplied. The same workflow revision must not produce a second externally visible wake.
A terminal gate event is material once; pending reminders and unchanged
readbacks are coalesced.

## Budgets and truncation

These are serialized UTF-8 byte budgets at service boundaries, not promises about a
model's token window:

| Envelope | Normal target | Over-budget behavior |
| --- | ---: | --- |
| routed request body | 4 KiB, excluding protocol envelope | reject with typed `review_request_too_large`; do not silently truncate |
| reviewer context | 8 KiB | deterministic field ordering/truncation plus `truncation` metadata and detail refs |
| terminal receipt/reply | 2 KiB | compact receipt by default; actionable finding details may expand within the typed finding budget |
| unchanged wake/status event | 0 additional events | coalesce by workflow key + revision |

Truncation is only legal for fields explicitly marked bounded. Never truncate
exact SHAs, IDs, statuses, gate terminal reasons, packet/message handles,
finding keys, or reply correlations. Detail refs must be opaque, expiring, and
read-only.

## State, staleness, and fallback

1. Persist the managed submission before external calls.
2. Reuse exact-SHA gates and request packets; do not rerun CI for a repeated
   pending event.
3. Review finalization must atomically prove that the round is still current;
   a stale round returns a typed conflict with a pointer to the current round.
4. A newer SHA supersedes older nonterminal work. The old workflow may finish
   as historical evidence, but must not wake or reply as if it were current.
5. Duplicate, reordered, timed-out, and restarted adapter deliveries converge
   on one workflow key and one material revision.
6. An unavailable optional source is explicit in `source_status`; it is never
   presented as an empty successful source.
7. Direct/unmanaged sessions retain the primitive Review/GitHub operations as
   typed recovery paths. They do not use managed wake/reply ownership.
8. Manual review returns its receipt in chat and sends no Crew reply. Routed
   review sends exactly one correlated Crew reply after durable finalization.

## Verification contract

The fixture corpus in `review/testdata/pointer-first/` covers initial review,
rereview, gate failure, and superseded-round states. Each fixture records the
serialized byte count and expected material-event key. Tests must prove:

- exact-SHA, round, gate, packet, finding, and reply handles survive compaction;
- canonical material digests remain stable across map ordering and timestamps;
- UTF-8 boundaries at 2048, 4096, and 8192 bytes never split a code point;
- repeated pending/status events coalesce;
- terminal/material state changes do not coalesce incorrectly;
- reordered/restarted deliveries return the same deterministic receipt;
- stale SHA results cannot advance a newer workflow;
- manual and routed reply cardinality remain distinct;
- representative pre-change broad context is larger than the bounded context.

This contract is intentionally additive. Later schema versions may add fields,
but a breaking change to identity, ownership, or replay semantics requires a
new version and an explicit migration/compatibility plan. Unknown major schema
versions fail closed; optional additive fields may be ignored by older readers.
