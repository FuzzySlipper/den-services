-- Resources ledger. Timestamps are UTC RFC 3339 text with fixed nanosecond
-- width so text order is time order. Telemetry and app probes are the
-- collectors' JSON, kept whole.

create table hosts (
    host_id text primary key,
    name text not null,
    role text not null,
    access text not null,
    note text not null,
    position integer not null,
    telemetry text,
    error text,
    attempted_at text,
    sampled_at text,
    received_at text,
    apps text not null default '[]',
    apps_checked_at text,
    created_at text not null,
    updated_at text not null
) strict;

create table resources (
    resource_id text primary key,
    host_id text not null,
    name text not null,
    kind text not null,
    path text not null,
    probe_path text not null,
    runtime_path text not null,
    access text not null,
    sharing text not null,
    lifecycle text not null,
    notes text not null,
    guide text not null,
    match text not null,
    updated_at text not null,
    last_used_at text,
    last_used_by text not null,
    observed_activity_at text,
    confirmed_at text,
    confirmed_by text not null,
    path_present integer,
    path_checked_at text
) strict;

create table work (
    work_id text primary key,
    host_id text not null,
    source text not null,
    external_id text,
    title text not null,
    owner text not null,
    project text not null,
    resources text not null,
    state text not null,
    note text not null,
    url text not null,
    ttl_seconds real not null,
    created_at text not null,
    started_at text,
    updated_at text not null,
    expires_at text not null,
    ended_at text
) strict;

create unique index work_external on work (source, host_id, external_id) where external_id is not null;
create index work_updated on work (updated_at);
create index work_active on work (state) where state in ('queued', 'running');

create table settings (
    key text primary key,
    value text not null
) strict;
