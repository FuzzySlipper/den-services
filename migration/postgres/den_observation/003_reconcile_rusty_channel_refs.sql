update den_observation.activity_events
set payload = jsonb_set(payload, '{work_ref,channel_id}', to_jsonb(7593), false)
where payload #>> '{work_ref,channel_id}' = '43';

create or replace view den_observation.rusty_channel_activity_split_refs as
select
	event_id,
	event_type,
	source_domain,
	agent_identity,
	runtime_instance_id,
	payload,
	created_at
from den_observation.activity_events
where payload #>> '{work_ref,channel_id}' = '43';

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		grant select on den_observation.rusty_channel_activity_split_refs to den_observation_app;
	end if;
end $$;
