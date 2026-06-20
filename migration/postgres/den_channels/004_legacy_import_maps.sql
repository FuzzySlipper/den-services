create table den_channels.legacy_import_channels (
	source text not null,
	legacy_id bigint not null,
	channel_id bigint not null references den_channels.channels(id),
	legacy_updated_at timestamptz null,
	imported_at timestamptz not null default now(),
	primary key (source, legacy_id),
	unique (channel_id)
);

create table den_channels.legacy_import_messages (
	source text not null,
	legacy_id bigint not null,
	message_id bigint not null references den_channels.channel_messages(id),
	legacy_channel_id bigint not null,
	legacy_created_at timestamptz null,
	imported_at timestamptz not null default now(),
	primary key (source, legacy_id),
	unique (message_id)
);

create table den_channels.legacy_import_memberships (
	source text not null,
	legacy_id bigint not null,
	membership_id bigint not null references den_channels.channel_memberships(id),
	legacy_channel_id bigint not null,
	imported_at timestamptz not null default now(),
	primary key (source, legacy_id),
	unique (membership_id)
);

create table den_channels.legacy_import_reactions (
	source text not null,
	legacy_id bigint not null,
	reaction_id bigint not null references den_channels.channel_reactions(id),
	legacy_message_id bigint not null,
	imported_at timestamptz not null default now(),
	primary key (source, legacy_id),
	unique (reaction_id)
);

create table den_channels.legacy_import_read_cursors (
	source text not null,
	legacy_id bigint not null,
	channel_id bigint not null references den_channels.channels(id),
	reader_type text not null,
	reader_identity text not null,
	imported_at timestamptz not null default now(),
	primary key (source, legacy_id)
);

create index legacy_import_channels_channel_idx
	on den_channels.legacy_import_channels (channel_id);

create index legacy_import_messages_message_idx
	on den_channels.legacy_import_messages (message_id);

create index legacy_import_messages_legacy_channel_idx
	on den_channels.legacy_import_messages (source, legacy_channel_id);

create index legacy_import_memberships_membership_idx
	on den_channels.legacy_import_memberships (membership_id);

create index legacy_import_reactions_reaction_idx
	on den_channels.legacy_import_reactions (reaction_id);

create or replace view den_channels.chat_history as
select
	m.id as message_id,
	m.channel_id,
	jsonb_build_object(
		'sender_type', m.sender_type,
		'sender_identity', m.sender_identity,
		'profile_identity', m.profile_identity,
		'agent_instance_id', m.agent_instance_id,
		'pool_member_id', m.pool_member_id,
		'session_owner_id', m.session_owner_id,
		'session_id', m.session_id
	) as author_identity,
	m.body,
	m.created_at
from den_channels.channel_messages m
where m.deleted_at is null
	and not (
		m.source_kind = 'legacy_import'
		and m.metadata->>'legacy_source_kind' = 'wake_event'
	);

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_channels_app') then
		grant select, insert, update on den_channels.legacy_import_channels to den_channels_app;
		grant select, insert, update on den_channels.legacy_import_messages to den_channels_app;
		grant select, insert, update on den_channels.legacy_import_memberships to den_channels_app;
		grant select, insert, update on den_channels.legacy_import_reactions to den_channels_app;
		grant select, insert, update on den_channels.legacy_import_read_cursors to den_channels_app;
	end if;
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		grant select on den_channels.chat_history to den_observation_app;
	end if;
end $$;
