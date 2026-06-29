create schema if not exists den_documents;

create table den_documents.documents (
    id bigint generated always as identity primary key,
    project_id text not null,
    slug text not null,
    title text not null,
    content text not null,
    doc_type text not null default 'spec',
    visibility text not null default 'normal',
    tags jsonb,
    summary text,
    search_vector tsvector generated always as (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(summary, '')), 'B') ||
        setweight(to_tsvector('english', coalesce(content, '')), 'C') ||
        setweight(to_tsvector('english', coalesce(tags::text, '')), 'D')
    ) stored,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (project_id, slug)
);

create table den_documents.discussion_threads (
    id bigint generated always as identity primary key,
    target_type text not null default 'document',
    target_project_id text not null,
    target_id bigint,
    target_slug text,
    target_anchor text,
    thread_key text not null,
    title text not null,
    status text not null default 'open',
    created_by text not null,
    summary text,
    resolution_summary text,
    metadata_json jsonb,
    last_comment_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table den_documents.discussion_comments (
    id bigint generated always as identity primary key,
    thread_id bigint not null references den_documents.discussion_threads(id) on delete cascade,
    parent_comment_id bigint references den_documents.discussion_comments(id) on delete cascade,
    author_identity text not null,
    body_markdown text not null,
    comment_kind text not null default 'comment',
    status text not null default 'active',
    mentions_json jsonb,
    source_refs_json jsonb,
    metadata_json jsonb,
    created_at timestamptz not null default now(),
    edited_at timestamptz,
    updated_at timestamptz not null default now()
);

create index documents_project_type_updated_idx
    on den_documents.documents(project_id, doc_type, updated_at desc, id desc);

create index documents_project_visibility_updated_idx
    on den_documents.documents(project_id, visibility, updated_at desc, id desc);

create index documents_visibility_updated_idx
    on den_documents.documents(visibility, updated_at desc, id desc);

create index documents_search_vector_idx
    on den_documents.documents using gin(search_vector);

create index documents_tags_idx
    on den_documents.documents using gin(tags jsonb_path_ops);

create unique index discussion_threads_unique_target_key_idx
    on den_documents.discussion_threads(
        target_type,
        coalesce(target_project_id, ''),
        coalesce(target_slug, ''),
        coalesce(target_id, -1),
        coalesce(target_anchor, ''),
        coalesce(thread_key, '')
    );

create index discussion_threads_target_status_updated_idx
    on den_documents.discussion_threads(target_project_id, target_slug, status, updated_at desc, id desc);

create index discussion_comments_thread_created_idx
    on den_documents.discussion_comments(thread_id, created_at asc, id asc);

create index discussion_comments_parent_idx
    on den_documents.discussion_comments(parent_comment_id)
    where parent_comment_id is not null;

grant usage on schema den_documents to den_documents_app;
grant select, insert, update, delete on den_documents.documents to den_documents_app;
grant select, insert, update, delete on den_documents.discussion_threads to den_documents_app;
grant select, insert, update, delete on den_documents.discussion_comments to den_documents_app;
grant usage, select on all sequences in schema den_documents to den_documents_app;
