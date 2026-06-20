create table den_channels.channels (
	id bigserial primary key,
	slug text not null unique,
	display_name text not null,
	kind text not null,
	project_id text null,
	space_id text null,
	created_by text not null,
	visibility text not null,
	settings jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz null
);

create unique index channels_project_default_idx
	on den_channels.channels (project_id)
	where project_id is not null
		and kind = 'project_default'
		and archived_at is null;

create table den_channels.channel_messages (
	id bigserial primary key,
	channel_id bigint not null references den_channels.channels(id),
	sender_type text not null,
	sender_identity text not null,
	body text not null,
	message_kind text not null,
	source_kind text not null,
	source_id text null,
	source_project_id text null,
	target_project_id text null,
	target_task_id bigint null,
	assignment_id text null,
	worker_run_id text null,
	worker_role text null,
	profile_identity text null,
	agent_instance_id text null,
	pool_member_id text null,
	session_owner_id text null,
	session_id text null,
	summary text null,
	deep_link text null,
	thread_root_message_id bigint null references den_channels.channel_messages(id),
	reply_to_message_id bigint null references den_channels.channel_messages(id),
	metadata jsonb not null default '{}'::jsonb,
	dedupe_key text null unique,
	created_at timestamptz not null default now(),
	edited_at timestamptz null,
	deleted_at timestamptz null
);

create index channel_messages_channel_created_idx
	on den_channels.channel_messages (channel_id, created_at, id);
create index channel_messages_target_task_idx
	on den_channels.channel_messages (target_project_id, target_task_id)
	where target_project_id is not null
		and target_task_id is not null;

create table den_channels.channel_memberships (
	id bigserial primary key,
	channel_id bigint not null references den_channels.channels(id),
	member_type text not null,
	member_identity text not null,
	profile_identity text null,
	membership_status text not null,
	wake_policy text not null,
	can_send boolean not null default true,
	can_react boolean not null default true,
	can_invite boolean not null default false,
	membership_purpose text not null,
	settings jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	left_at timestamptz null,
	unique (channel_id, member_identity, membership_purpose)
);

create index channel_memberships_member_idx
	on den_channels.channel_memberships (member_identity, membership_status);
create index channel_memberships_profile_idx
	on den_channels.channel_memberships (profile_identity)
	where profile_identity is not null;

create table den_channels.channel_reactions (
	id bigserial primary key,
	message_id bigint not null references den_channels.channel_messages(id),
	channel_id bigint not null references den_channels.channels(id),
	reactor_type text not null,
	reactor_identity text not null,
	reaction text not null,
	created_at timestamptz not null default now(),
	deleted_at timestamptz null,
	unique (message_id, reactor_identity, reaction)
);

create index channel_reactions_channel_idx
	on den_channels.channel_reactions (channel_id, message_id);

create table den_channels.channel_read_cursors (
	channel_id bigint not null references den_channels.channels(id),
	reader_type text not null,
	reader_identity text not null,
	instance_id text null,
	last_read_message_id bigint null references den_channels.channel_messages(id),
	last_read_at timestamptz not null default now(),
	primary key (channel_id, reader_type, reader_identity)
);

create table den_channels.channel_project_links (
	id bigserial primary key,
	channel_id bigint not null references den_channels.channels(id),
	project_id text not null,
	link_kind text not null,
	created_by text not null,
	created_at timestamptz not null default now(),
	deleted_at timestamptz null,
	unique (channel_id, project_id, link_kind)
);

create index channel_project_links_project_idx
	on den_channels.channel_project_links (project_id)
	where deleted_at is null;

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
where m.deleted_at is null;

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_channels_app') then
		grant usage on schema den_channels to den_channels_app;
		grant select, insert, update on den_channels.channels to den_channels_app;
		grant select, insert, update on den_channels.channel_messages to den_channels_app;
		grant select, insert, update on den_channels.channel_memberships to den_channels_app;
		grant select, insert, update on den_channels.channel_reactions to den_channels_app;
		grant select, insert, update on den_channels.channel_read_cursors to den_channels_app;
		grant select, insert, update on den_channels.channel_project_links to den_channels_app;
		grant usage, select on all sequences in schema den_channels to den_channels_app;
		revoke delete on all tables in schema den_channels from den_channels_app;
		alter default privileges in schema den_channels revoke delete on tables from den_channels_app;
	end if;
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		grant usage on schema den_channels to den_observation_app;
		grant select on den_channels.chat_history to den_observation_app;
	end if;
end $$;
