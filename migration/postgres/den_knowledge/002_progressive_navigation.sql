alter table den_knowledge.knowledge_entry_links
    add constraint knowledge_entry_links_kind_check
    check (link_kind in ('related', 'embed', 'replacement')) not valid;

create unique index knowledge_entry_links_unique_edge_idx
    on den_knowledge.knowledge_entry_links(from_entry_id, to_entry_slug, link_kind);

create table den_knowledge.knowledge_maps (
    slug text primary key,
    title text not null,
    summary text,
    created_by text,
    updated_by text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table den_knowledge.knowledge_map_entries (
    map_slug text not null references den_knowledge.knowledge_maps(slug) on delete cascade,
    entry_slug text not null references den_knowledge.knowledge_entries(slug) on delete restrict,
    group_name text,
    position integer not null check (position >= 0 and position < 100),
    note text,
    primary key (map_slug, entry_slug),
    unique (map_slug, position)
);

create index knowledge_map_entries_entry_slug_idx
    on den_knowledge.knowledge_map_entries(entry_slug);

grant select, insert, update, delete on den_knowledge.knowledge_maps to den_knowledge_app;
grant select, insert, update, delete on den_knowledge.knowledge_map_entries to den_knowledge_app;
