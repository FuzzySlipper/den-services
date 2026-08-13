create schema if not exists den_board;

create sequence den_board.entry_id_seq;

create table den_board.posts (
    id bigint primary key default nextval('den_board.entry_id_seq'),
    project_id text not null,
    title text,
    body_markdown text,
    author_identity text,
    metadata_json jsonb,
    status text not null default 'active' check (status in ('active', 'deleted')),
    created_at timestamptz not null,
    updated_at timestamptz not null,
    deleted_at timestamptz,
    check (updated_at >= created_at),
    check (
        (status = 'active' and title is not null and length(btrim(title)) > 0
            and body_markdown is not null and length(btrim(body_markdown)) > 0
            and author_identity is not null and length(btrim(author_identity)) > 0
            and deleted_at is null)
        or
        (status = 'deleted' and title is null and body_markdown is null
            and author_identity is null and metadata_json is null and deleted_at is not null)
    )
);

create table den_board.comments (
    id bigint primary key default nextval('den_board.entry_id_seq'),
    post_id bigint not null references den_board.posts(id),
    parent_comment_id bigint,
    author_identity text,
    body_markdown text,
    metadata_json jsonb,
    status text not null default 'active' check (status in ('active', 'deleted')),
    created_at timestamptz not null,
    updated_at timestamptz not null,
    deleted_at timestamptz,
    unique (id, post_id),
    foreign key (parent_comment_id, post_id) references den_board.comments(id, post_id),
    check (parent_comment_id is null or parent_comment_id <> id),
    check (updated_at >= created_at),
    check (
        (status = 'active' and body_markdown is not null and length(btrim(body_markdown)) > 0
            and author_identity is not null and length(btrim(author_identity)) > 0
            and deleted_at is null)
        or
        (status = 'deleted' and body_markdown is null and author_identity is null
            and metadata_json is null and deleted_at is not null)
    )
);

create index posts_project_active_id_idx
    on den_board.posts(project_id, id)
    where status = 'active';

create index comments_post_parent_active_id_idx
    on den_board.comments(post_id, parent_comment_id, id);

create index comments_parent_idx
    on den_board.comments(parent_comment_id)
    where parent_comment_id is not null;

comment on table den_board.posts is
    'Passive project-scoped Board posts. Rows never authorize execution or agent wakes.';
comment on table den_board.comments is
    'Immediately-parented Board comments. Deleted rows retain only structural IDs and timestamps.';

grant usage on schema den_board to den_board_app;
grant select, insert, update on den_board.posts to den_board_app;
grant select, insert, update on den_board.comments to den_board_app;
grant usage, select on all sequences in schema den_board to den_board_app;
