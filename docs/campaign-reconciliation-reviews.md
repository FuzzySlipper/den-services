# Campaign reconciliation reviews

Campaign reconciliation is a first-class review target for a parent task whose
acceptance spans multiple already-reviewed child tasks and repositories. It is
not a synthetic branch diff.

Use `request_campaign_review` with:

- the parent `task_id` and `requested_by`;
- `children`: immutable `{project_id, task_id, review_round_id}` references;
- `repositories`: immutable `{repository, head_sha}` tuples using full Git SHAs;
- optional campaign-level tests, notes, thread, and run correlation.

The parent review round stores the resolved child head, approval verdict, and
membership kind beside those exact repository heads. It deliberately has no
parent branch, base, or head commit.

## Membership

A child is an explicit campaign member when either:

1. it is a direct subtask in the same project (`parent_id` is the campaign task);
2. the parent and child both carry the exact tag
   `campaign:<parent_project_id>:<parent_task_id>`.

The tag path is the cross-project mechanism. Dependencies, title similarity,
and a merely related message do not establish membership.

## Validation

At request time the Review service fails closed unless every child:

- exists in the cited project;
- points to that task's latest review round;
- has a `looks_good` verdict;
- has no unresolved `blocking_bug` or `acceptance_gap` finding;
- has a reviewed head matching one of the exact repository/head tuples.

Duplicate child tasks, review rounds, or repositories are rejected. The
repository snapshot must use unique `owner/name` entries and full 40-character
SHAs.

Ordinary `request_review` remains the code-diff path. Campaign packets and
workflow summaries expose `target_kind: campaign_reconciliation`, child
snapshots, and repository snapshots instead of diff metadata.

Reviewers use the normal structured finding and `finalize_review` workflow.
Finalization remains idempotent: retry the identical decision after an
ambiguous or interrupted result.
