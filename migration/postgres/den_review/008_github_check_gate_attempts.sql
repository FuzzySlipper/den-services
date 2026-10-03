-- A failed or timed-out gate can be re-evaluated when registered again (for
-- example after a GitHub re-run or a later containing commit) and reopen as a
-- new attempt. Each attempt emits at most one terminal event.
alter table den_review.github_check_gates
    add column attempt integer not null default 1 check (attempt >= 1);

alter table den_review.github_check_gate_terminal_events
    add column attempt integer not null default 1 check (attempt >= 1);

alter table den_review.github_check_gate_terminal_events
    drop constraint github_check_gate_terminal_events_gate_id_key;

alter table den_review.github_check_gate_terminal_events
    add constraint github_check_gate_terminal_events_gate_attempt_key unique (gate_id, attempt);

comment on column den_review.github_check_gates.attempt is
    'Evaluation attempt; incremented when a failed or timed-out gate reopens on re-registration.';
