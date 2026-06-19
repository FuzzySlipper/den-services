do $$
begin
	if exists (
		select 1
		from information_schema.columns
		where table_schema = 'den_observation'
			and table_name = 'activity_events'
			and column_name = 'id'
	) and not exists (
		select 1
		from information_schema.columns
		where table_schema = 'den_observation'
			and table_name = 'activity_events'
			and column_name = 'event_id'
	) then
		alter table den_observation.activity_events rename column id to event_id;
	end if;
end $$;

alter table den_observation.activity_events
	add column if not exists source_domain text not null default 'observation',
	add column if not exists agent_identity jsonb null,
	add column if not exists runtime_instance_id text null;

update den_observation.activity_events
set agent_identity = source_identity
where agent_identity is null
	and source_identity is not null;

alter table den_observation.activity_events
	drop column if exists source_identity,
	drop column if exists subject_identity,
	drop column if exists source_ref;

alter table den_observation.activity_events
	alter column source_domain drop default;

drop index if exists den_observation.activity_events_event_type_created_at_idx;
drop index if exists den_observation.activity_events_created_at_idx;

create index if not exists activity_events_source_domain_created_at_idx
	on den_observation.activity_events (source_domain, created_at);
create index if not exists activity_events_event_type_created_at_idx
	on den_observation.activity_events (event_type, created_at);
create index if not exists activity_events_created_at_idx
	on den_observation.activity_events (created_at);

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		revoke update, delete on all tables in schema den_observation from den_observation_app;
		grant usage on schema den_observation to den_observation_app;
		grant select, insert on den_observation.activity_events to den_observation_app;
		grant usage, select on all sequences in schema den_observation to den_observation_app;
		alter default privileges in schema den_observation revoke update, delete on tables from den_observation_app;
		alter default privileges in schema den_observation grant select, insert on tables to den_observation_app;
		alter default privileges in schema den_observation grant usage, select on sequences to den_observation_app;
	end if;
end $$;
