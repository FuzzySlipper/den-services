create or replace view den_channels.chat_history as
select
	null::bigint as message_id,
	null::bigint as channel_id,
	null::jsonb as author_identity,
	null::text as body,
	null::timestamptz as created_at
where false;

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_observation_app') then
		grant usage on schema den_channels to den_observation_app;
		grant select on den_channels.chat_history to den_observation_app;
	end if;
end $$;
