create table den_channels.legacy_import_project_links (
	source text not null,
	legacy_id bigint not null,
	project_link_id bigint not null references den_channels.channel_project_links(id),
	legacy_channel_id bigint not null,
	legacy_is_primary boolean not null default false,
	legacy_settings jsonb not null default '{}'::jsonb,
	imported_at timestamptz not null default now(),
	primary key (source, legacy_id),
	unique (project_link_id)
);

create index legacy_import_project_links_link_idx
	on den_channels.legacy_import_project_links (project_link_id);

create index legacy_import_project_links_legacy_channel_idx
	on den_channels.legacy_import_project_links (source, legacy_channel_id);

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_channels_app') then
		grant select, insert, update on den_channels.legacy_import_project_links to den_channels_app;
	end if;
end $$;
