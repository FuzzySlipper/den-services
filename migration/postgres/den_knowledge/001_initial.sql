create schema if not exists den_knowledge;

create table den_knowledge.knowledge_entries (
    id bigint generated always as identity primary key,
    slug text not null unique,
    title text not null,
    summary text,
    body_markdown text not null,
    kind text not null default 'reference',
    status text not null default 'draft',
    curation_state text not null default 'unreviewed_import',
    audience_json jsonb,
    aliases_json jsonb,
    source_refs_json jsonb,
    accuracy_notes text,
    replacement_slug text,
    last_reviewed_at timestamptz,
    review_due_at timestamptz,
    created_by text,
    updated_by text,
    search_vector tsvector generated always as (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(summary, '')), 'B') ||
        setweight(to_tsvector('english', coalesce(body_markdown, '')), 'C') ||
        setweight(to_tsvector('english', coalesce(slug, '')), 'D')
    ) stored,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table den_knowledge.knowledge_entry_tags (
    entry_id bigint not null references den_knowledge.knowledge_entries(id) on delete cascade,
    tag text not null,
    primary key (entry_id, tag)
);

create table den_knowledge.knowledge_entry_revisions (
    id bigint generated always as identity primary key,
    entry_id bigint not null references den_knowledge.knowledge_entries(id) on delete cascade,
    revision_number integer not null,
    title text not null,
    summary text,
    body_markdown text not null,
    kind text not null,
    status text not null,
    curation_state text not null,
    tags_json jsonb,
    audience_json jsonb,
    aliases_json jsonb,
    source_refs_json jsonb,
    accuracy_notes text,
    replacement_slug text,
    changed_by text,
    change_note text,
    created_at timestamptz not null default now(),
    unique (entry_id, revision_number)
);

create table den_knowledge.knowledge_entry_links (
    id bigint generated always as identity primary key,
    from_entry_id bigint not null references den_knowledge.knowledge_entries(id) on delete cascade,
    to_entry_slug text not null,
    link_kind text not null default 'related',
    description text,
    created_at timestamptz not null default now()
);

create index knowledge_entries_status_kind_idx
    on den_knowledge.knowledge_entries(status, kind, updated_at desc, id desc);

create index knowledge_entries_review_due_idx
    on den_knowledge.knowledge_entries(review_due_at)
    where review_due_at is not null;

create index knowledge_entries_search_vector_idx
    on den_knowledge.knowledge_entries using gin(search_vector);

create index knowledge_entry_tags_tag_idx
    on den_knowledge.knowledge_entry_tags(tag);

create index knowledge_entry_revisions_entry_revision_idx
    on den_knowledge.knowledge_entry_revisions(entry_id, revision_number desc);

create index knowledge_entry_links_from_kind_idx
    on den_knowledge.knowledge_entry_links(from_entry_id, link_kind);

grant usage on schema den_knowledge to den_knowledge_app;
grant select, insert, update, delete on den_knowledge.knowledge_entries to den_knowledge_app;
grant select, insert, update, delete on den_knowledge.knowledge_entry_tags to den_knowledge_app;
grant select, insert, update, delete on den_knowledge.knowledge_entry_revisions to den_knowledge_app;
grant select, insert, update, delete on den_knowledge.knowledge_entry_links to den_knowledge_app;
grant usage, select on all sequences in schema den_knowledge to den_knowledge_app;
