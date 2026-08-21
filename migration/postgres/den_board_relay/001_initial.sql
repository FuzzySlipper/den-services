create schema if not exists den_board_relay;

create table den_board_relay.mappings (
    project_id text not null,
    board_kind text not null check (board_kind in ('post', 'comment')),
    board_id bigint not null,
    github_kind text not null check (github_kind in ('issue', 'comment')),
    github_id bigint not null,
    issue_number bigint not null,
    github_url text not null,
    origin text not null check (origin in ('board', 'github')),
    github_updated_at timestamptz not null,
    created_at timestamptz not null,
    updated_at timestamptz not null,
    primary key (project_id, board_kind, board_id),
    unique (project_id, github_kind, github_id),
    check (board_id > 0),
    check (github_id > 0),
    check (issue_number > 0),
    check (updated_at >= created_at)
);

create index mappings_project_issue_number_idx
    on den_board_relay.mappings (project_id, issue_number);

comment on table den_board_relay.mappings is
    'Durable source-ID mapping for the manual Board/GitHub relay. The relay has no access to den_board tables.';

grant usage on schema den_board_relay to den_board_relay_app;
grant select, insert, update on den_board_relay.mappings to den_board_relay_app;
