create schema if not exists den_timeline;

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_timeline_app') then
		grant usage on schema den_timeline to den_timeline_app;
		grant usage on schema den_channels to den_timeline_app;
		grant usage on schema den_observation to den_timeline_app;
		grant select on den_channels.channels to den_timeline_app;
		grant select on den_channels.channel_messages to den_timeline_app;
		grant select on den_channels.channel_reactions to den_timeline_app;
		grant select on den_observation.activity_events to den_timeline_app;
		revoke insert, update, delete on all tables in schema den_channels from den_timeline_app;
		revoke insert, update, delete on all tables in schema den_observation from den_timeline_app;
		revoke insert, update, delete on all tables in schema den_delivery from den_timeline_app;
		revoke insert, update, delete on all tables in schema den_runtime from den_timeline_app;
	end if;
end $$;
