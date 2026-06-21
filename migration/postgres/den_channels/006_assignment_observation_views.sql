create or replace view den_channels.assignment_transcript as
select
	m.id as message_id,
	m.channel_id,
	m.sender_type,
	m.sender_identity,
	m.body,
	m.message_kind,
	m.source_kind,
	m.target_project_id,
	m.target_task_id,
	m.assignment_id,
	m.worker_run_id,
	m.worker_role,
	m.profile_identity,
	m.agent_instance_id,
	m.pool_member_id,
	m.session_id,
	m.summary,
	m.deep_link,
	m.metadata,
	m.created_at
from den_channels.channel_messages m
where m.deleted_at is null
	and m.assignment_id is not null
	and m.assignment_id <> '';

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		grant usage on schema den_channels to den_observation_app;
		grant select on den_channels.assignment_transcript to den_observation_app;
	end if;
end $$;
