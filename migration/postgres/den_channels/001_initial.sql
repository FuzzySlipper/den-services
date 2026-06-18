create table den_channels.conversation_successor_placeholder (
	key text primary key,
	value text not null,
	updated_at timestamptz not null default now()
);

insert into den_channels.conversation_successor_placeholder (key, value)
values ('status', 'p3 placeholder schema')
on conflict (key) do update
set value = excluded.value,
	updated_at = now();

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_channels_app') then
		grant usage on schema den_channels to den_channels_app;
		grant select, insert, update, delete on all tables in schema den_channels to den_channels_app;
		grant usage, select on all sequences in schema den_channels to den_channels_app;
		alter default privileges in schema den_channels grant select, insert, update, delete on tables to den_channels_app;
		alter default privileges in schema den_channels grant usage, select on sequences to den_channels_app;
	end if;
end $$;
