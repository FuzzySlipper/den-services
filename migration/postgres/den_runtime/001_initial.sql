create table den_runtime.runtime_instances (
	instance_id text primary key,
	profile_identity text not null,
	host text not null,
	pid integer null,
	state text not null,
	started_at timestamptz not null,
	last_heartbeat_at timestamptz null,
	stopped_at timestamptz null,
	degraded_reason text null
);

create index runtime_instances_profile_identity_idx on den_runtime.runtime_instances (profile_identity);
create index runtime_instances_state_heartbeat_idx on den_runtime.runtime_instances (state, last_heartbeat_at);

create table den_runtime.channel_subscriptions (
	subscription_id bigserial primary key,
	runtime_instance_id text not null references den_runtime.runtime_instances (instance_id),
	channel_id bigint not null,
	cursor_position bigint not null default 0,
	last_polled_at timestamptz null,
	wake_policy_override text null,
	created_at timestamptz not null default now(),
	constraint channel_subscriptions_runtime_channel_unique unique (runtime_instance_id, channel_id)
);

create index channel_subscriptions_channel_id_idx on den_runtime.channel_subscriptions (channel_id);

create table den_runtime.channel_subscription_cursors (
	cursor_id bigserial primary key,
	subscription_id bigint not null references den_runtime.channel_subscriptions (subscription_id),
	cursor_kind text not null,
	cursor_position bigint not null default 0,
	updated_at timestamptz not null default now(),
	constraint channel_subscription_cursors_subscription_kind_unique unique (subscription_id, cursor_kind)
);

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_runtime_app') then
		grant usage on schema den_runtime to den_runtime_app;
		grant select, insert, update, delete on all tables in schema den_runtime to den_runtime_app;
		grant usage, select on all sequences in schema den_runtime to den_runtime_app;
		alter default privileges in schema den_runtime grant select, insert, update, delete on tables to den_runtime_app;
		alter default privileges in schema den_runtime grant usage, select on sequences to den_runtime_app;
	end if;
end $$;
