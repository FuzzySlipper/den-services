alter table den_review.github_check_gates
    add column terminal_reason text,
    add column missing_required_checks jsonb not null default '[]'::jsonb,
    add column observed_check_runs jsonb not null default '[]'::jsonb;

comment on column den_review.github_check_gates.terminal_reason is
    'Stable machine reason for a terminal gate, distinct from the compatibility status enum.';

comment on column den_review.github_check_gates.missing_required_checks is
    'Exact requested check-run names that GitHub had not exposed at the latest evaluation.';

comment on column den_review.github_check_gates.observed_check_runs is
    'All latest-by-name check runs observed for the exact commit SHA, including unrequested runs.';
