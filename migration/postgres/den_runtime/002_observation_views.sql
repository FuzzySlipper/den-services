create or replace view den_runtime.instance_states as
select
	instance_id as runtime_instance_id,
	profile_identity,
	host,
	pid,
	state,
	started_at,
	last_heartbeat_at,
	stopped_at,
	degraded_reason,
	state in ('stale', 'dead', 'stopped') as display_only
from den_runtime.runtime_instances;

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		grant usage on schema den_runtime to den_observation_app;
		grant select on den_runtime.instance_states to den_observation_app;
	end if;
end $$;
