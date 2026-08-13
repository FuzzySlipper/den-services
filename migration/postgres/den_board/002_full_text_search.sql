alter table den_board.posts
    add column search_vector tsvector generated always as (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(body_markdown, '')), 'B')
    ) stored;

alter table den_board.comments
    add column search_vector tsvector generated always as (
        to_tsvector('english', coalesce(body_markdown, ''))
    ) stored;

create index posts_search_vector_idx
    on den_board.posts using gin(search_vector)
    where status = 'active';

create index comments_search_vector_idx
    on den_board.comments using gin(search_vector)
    where status = 'active';
