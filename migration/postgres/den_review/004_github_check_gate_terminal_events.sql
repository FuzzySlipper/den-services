create table den_review.github_check_gate_terminal_events (
    id bigserial primary key,
    schema text not null default 'den_review.github_check_gate_terminal_event',
    schema_version integer not null default 1,
    gate_id bigint not null unique references den_review.github_check_gates(id),
    project_id text not null,
    task_id bigint not null,
    repository text not null,
    commit_sha text not null,
    ref text not null,
    status text not null check (status in ('passed', 'failed', 'timed_out', 'superseded')),
    terminal_reason text not null,
    required_checks jsonb not null default '[]'::jsonb,
    check_runs jsonb not null default '[]'::jsonb,
    observed_check_runs jsonb not null default '[]'::jsonb,
    missing_required_checks jsonb not null default '[]'::jsonb,
    summary text,
    failure_summary text,
    requested_by text not null,
    agent_profile text,
    agent_instance_id text,
    session_key text,
    gate_created_at timestamptz not null,
    completed_at timestamptz not null,
    created_at timestamptz not null
);

create index github_check_gate_terminal_events_project_cursor_idx
    on den_review.github_check_gate_terminal_events(project_id, id);
create index github_check_gate_terminal_events_task_cursor_idx
    on den_review.github_check_gate_terminal_events(project_id, task_id, id);

comment on table den_review.github_check_gate_terminal_events is
    'Append-only machine wake facts for terminal exact-SHA GitHub check gates; task messages are only a human projection.';
comment on column den_review.github_check_gate_terminal_events.id is
    'Global monotonic at-least-once consumption cursor; consumers acknowledge by persisting the last handled ID.';

grant select, insert on den_review.github_check_gate_terminal_events to den_review_app;
grant usage, select on sequence den_review.github_check_gate_terminal_events_id_seq to den_review_app;
