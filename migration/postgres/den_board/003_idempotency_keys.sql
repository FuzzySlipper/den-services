create table den_board.idempotency_keys (
    request_key text primary key,
    operation text not null check (operation in ('post', 'comment')),
    post_id bigint references den_board.posts(id),
    comment_id bigint references den_board.comments(id),
    created_at timestamptz not null default now(),
    check (
        (operation = 'post' and comment_id is null)
        or (operation = 'comment' and post_id is null)
    )
);

comment on table den_board.idempotency_keys is
    'Opaque Board-owned replay keys. Callers retain their own source mapping and cannot query this table directly.';
