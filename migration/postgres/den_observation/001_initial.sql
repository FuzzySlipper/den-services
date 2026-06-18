create table den_observation.activity_events (
	id bigserial primary key,
	event_type text not null,
	source_identity jsonb null,
	subject_identity jsonb null,
	source_ref text null,
	payload jsonb not null default '{}'::jsonb,
	display_only boolean not null default true,
	created_at timestamptz not null default now()
);

create index activity_events_event_type_created_at_idx on den_observation.activity_events (event_type, created_at);
create index activity_events_created_at_idx on den_observation.activity_events (created_at);

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		grant usage on schema den_observation to den_observation_app;
		grant select, insert, update, delete on all tables in schema den_observation to den_observation_app;
		grant usage, select on all sequences in schema den_observation to den_observation_app;
		alter default privileges in schema den_observation grant select, insert, update, delete on tables to den_observation_app;
		alter default privileges in schema den_observation grant usage, select on sequences to den_observation_app;
	end if;
end $$;
