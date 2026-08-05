create schema if not exists den_handoff;

create table den_handoff.handoffs (
    label text primary key,
    body_markdown text not null,
    revision bigint not null check (revision > 0),
    created_at timestamptz not null,
    updated_at timestamptz not null,
    updated_by text,
    check (octet_length(label) between 1 and 128),
    check (octet_length(body_markdown) between 1 and 65536),
    check (updated_at >= created_at)
);

create table den_handoff.handoff_revisions (
    label text not null references den_handoff.handoffs(label) on delete cascade,
    revision bigint not null check (revision > 0),
    body_markdown text not null,
    updated_by text,
    submitted_at timestamptz not null,
    primary key (label, revision),
    check (octet_length(body_markdown) between 1 and 65536)
);

comment on table den_handoff.handoffs is
    'One current non-executable resume packet per exact handoff label.';
comment on table den_handoff.handoff_revisions is
    'Service-owned replacement history for operator recovery; not an executable or default retrieval surface.';

grant usage on schema den_handoff to den_handoff_app;
grant select, insert, update on den_handoff.handoffs to den_handoff_app;
grant select, insert on den_handoff.handoff_revisions to den_handoff_app;
