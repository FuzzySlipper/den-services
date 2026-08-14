# Campaign reconciliation reviews

Campaign review is a bounded current-state check across several Den projects
and repositories. It answers whether the named child review rounds and the
current repository state together support the parent campaign decision.

## Request shape

```json
{
  "requested_by": "campaign-agent",
  "children": [
    {"project_id": "den-services", "task_id": 6097, "review_round_id": 70},
    {"project_id": "asha-rulebench", "task_id": 6099, "review_round_id": 71}
  ],
  "repositories": [
    {"repository": "FuzzySlipper/den-services"},
    {"repository": "FuzzySlipper/asha-rulebench"}
  ],
  "tests_run": ["go test ./..."],
  "notes": "Campaign-level reconciliation notes."
}
```

Repository entries name the repositories in scope. Child entries identify the
project, task, and review-round records. Neither entry carries a source-control
revision. The reviewer opens the current checkout for each named repository
and uses the run timestamp plus the durable task/round handles to reconstruct
the review context.

## Validation

The Review service validates, in order:

1. the parent task is reviewable and writable;
2. at least one child and one repository are supplied;
3. repository names are valid and unique;
4. child identities are unique and each referenced round exists;
5. each child task belongs to the campaign through a direct-subtask or
   campaign-tag relationship;
6. each referenced round is the latest round for that child task;
7. each child round has the approved `looks_good` verdict;
8. no child has unresolved blocking or acceptance findings.

The service snapshots the named repositories and child membership into the
campaign round. Reordering an equivalent request is idempotent. A later child
round or a changed campaign decision creates a new campaign round; the old
round remains historical evidence and cannot finalize the new one.

## Review packet and context

The campaign request packet contains:

- parent project/task and review-round handles;
- the named repository list;
- child project/task/round handles and membership kind;
- tests, notes, finding summaries, and evidence/detail handles.

It does not contain source-control revision tuples or copied diffs. The
reviewer examines the current checkout. `get_review_context`
returns the current campaign snapshot in a bounded form and provides an opaque
detail reference when the full child/repository list is larger than the normal
budget.

## Completion

Campaign finalization uses the same current-round lock and idempotent receipt as
a single-task review. It applies the verdict, finding resolution, packet
delivery, and parent-task transition atomically at the Review/Tasks boundaries.
GitHub check gates remain a separate optional deterministic operation when a
campaign explicitly requires named CI checks; gate identity is not added to the
campaign request or packet.

## Evidence

Reviewers should report the current repository observations, child review-round
handles, tests run, visible findings, and any uncertainty. The campaign packet
is a durable pointer and audit record, not a source snapshot. A timestamp and
the persisted Den handles are sufficient for later reconstruction.
