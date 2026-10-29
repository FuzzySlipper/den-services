-- Ops backup ledger. Timestamps are UTC RFC 3339 text with fixed nanosecond
-- width so text order is time order.

create table hosts (
    host_id text primary key,
    display_name text not null,
    role text not null,
    notes text not null,
    created_at text not null,
    updated_at text not null
) strict;

create table jobs (
    job_id text primary key,
    host_id text not null,
    display_name text not null,
    engine text not null,
    kind text not null,
    source text not null,
    scope text not null,
    target text not null,
    schedule_timezone text,
    schedule_times text,
    schedule_weekdays text,
    schedule_grace_seconds integer,
    heartbeat_stale_seconds integer not null,
    retention text not null,
    recovery_pointer text not null,
    notes text not null,
    registered integer not null,
    enabled integer not null,
    created_at text not null,
    updated_at text not null
) strict;

create table runs (
    job_id text not null references jobs (job_id),
    run_id text not null,
    host text not null,
    kind text not null,
    engine text not null,
    runner text not null,
    state text not null,
    phase text not null,
    started_at text not null,
    updated_at text not null,
    finished_at text,
    exit_code integer,
    error text not null,
    reason text not null,
    warnings_json text not null,
    notices_json text not null,
    target text not null,
    archive_id text not null,
    archive_name text not null,
    original_bytes integer,
    compressed_bytes integer,
    deduplicated_bytes integer,
    file_count integer,
    duration_seconds real,
    complete integer not null,
    log_path text not null,
    manifest_path text not null,
    run_directory text not null,
    recovery_pointer text not null,
    raw_report text not null,
    received_at text not null,
    report_count integer not null,
    primary key (job_id, run_id)
) strict;

create index runs_job_started on runs (job_id, started_at desc);
create index runs_state_finished on runs (state, finished_at desc);

create table verifications (
    job_id text not null references jobs (job_id),
    verification_id text not null,
    run_id text not null,
    kind text not null,
    check_type text not null,
    state text not null,
    at text not null,
    scope text not null,
    archive text not null,
    full_data_read integer,
    evidence_path text not null,
    checked_json text not null,
    raw_report text not null,
    received_at text not null,
    primary key (job_id, verification_id)
) strict;

create index verifications_job_kind_at on verifications (job_id, kind, at desc);
