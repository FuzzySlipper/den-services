create or replace view den_delivery.active_intents as
select
	id as intent_id,
	target_identity,
	target_identity ->> 'profile' as target_profile,
	state,
	claimed_by,
	claimed_by ->> 'profile' as claimed_profile,
	claimed_by ->> 'instance_id' as claimed_instance_id,
	source_ref,
	channel_message_id,
	created_at
from den_delivery.delivery_intents
where state in ('pending', 'claimed', 'running');

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		grant usage on schema den_delivery to den_observation_app;
		grant select on den_delivery.active_intents to den_observation_app;
	end if;
end $$;
