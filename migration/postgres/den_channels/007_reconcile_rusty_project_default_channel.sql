do $$
declare
	rusty_project_id constant text := 'rusty-crew';
	rusty_slug constant text := 'project-rusty-crew';
	legacy_source constant text := 'legacy_den_channels_sqlite';
	legacy_channel_id constant bigint := 7593;
	source_channel_id bigint;
	source_slug text;
	source_display_name text;
	source_kind text;
	source_project_id text;
	source_space_id text;
	source_created_by text;
	source_visibility text;
	source_settings jsonb;
	source_created_at timestamptz;
	source_updated_at timestamptz;
	source_archived_at timestamptz;
begin
	select
		c.id,
		c.slug,
		c.display_name,
		c.kind,
		c.project_id,
		c.space_id,
		c.created_by,
		c.visibility,
		c.settings,
		c.created_at,
		c.updated_at,
		c.archived_at
	into
		source_channel_id,
		source_slug,
		source_display_name,
		source_kind,
		source_project_id,
		source_space_id,
		source_created_by,
		source_visibility,
		source_settings,
		source_created_at,
		source_updated_at,
		source_archived_at
	from den_channels.channels c
	join den_channels.legacy_import_channels lic
		on lic.channel_id = c.id
		and lic.source = legacy_source
		and lic.legacy_id = legacy_channel_id
	where c.project_id = rusty_project_id
		and c.kind = 'project_default'
		and c.archived_at is null
		and c.id <> legacy_channel_id
	order by c.id
	limit 1;

	if source_channel_id is not null then
		if exists (
			select 1
			from den_channels.channels existing
			where existing.id = legacy_channel_id
				and existing.id <> source_channel_id
				and (
					existing.kind <> 'project_default'
					or existing.project_id is not null
					and existing.project_id <> rusty_project_id
				)
		) then
			raise exception 'cannot reconcile rusty-crew project default channel: channel id % exists but is not the Rusty Crew project default', legacy_channel_id;
		end if;

		update den_channels.channels
		set slug = rusty_slug || '-superseded-' || source_channel_id::text,
			project_id = null,
			archived_at = coalesce(source_archived_at, now()),
			updated_at = now()
		where id = source_channel_id;

		insert into den_channels.channels (
			id,
			slug,
			display_name,
			kind,
			project_id,
			space_id,
			created_by,
			visibility,
			settings,
			created_at,
			updated_at,
			archived_at
		) values (
			legacy_channel_id,
			source_slug,
			source_display_name,
			source_kind,
			source_project_id,
			source_space_id,
			source_created_by,
			source_visibility,
			source_settings,
			source_created_at,
			now(),
			null
		)
		on conflict (id) do update set
			slug = excluded.slug,
			display_name = excluded.display_name,
			kind = excluded.kind,
			project_id = excluded.project_id,
			space_id = excluded.space_id,
			created_by = excluded.created_by,
			visibility = excluded.visibility,
			settings = excluded.settings,
			updated_at = excluded.updated_at,
			archived_at = null;

		update den_channels.channel_messages
		set channel_id = legacy_channel_id
		where channel_id = source_channel_id;

		update den_channels.channel_reactions
		set channel_id = legacy_channel_id
		where channel_id = source_channel_id;

		update den_channels.channel_memberships target
		set member_type = source.member_type,
			profile_identity = coalesce(source.profile_identity, target.profile_identity),
			membership_status = case
				when source.membership_status = 'active' or target.membership_status = 'active' then 'active'
				else source.membership_status
			end,
			wake_policy = source.wake_policy,
			can_send = source.can_send or target.can_send,
			can_react = source.can_react or target.can_react,
			can_invite = source.can_invite or target.can_invite,
			settings = target.settings || source.settings,
			created_at = least(target.created_at, source.created_at),
			updated_at = greatest(target.updated_at, source.updated_at),
			left_at = case
				when source.left_at is null or target.left_at is null then null
				else greatest(source.left_at, target.left_at)
			end
		from den_channels.channel_memberships source
		where source.channel_id = source_channel_id
			and target.channel_id = legacy_channel_id
			and target.member_identity = source.member_identity
			and target.membership_purpose = source.membership_purpose;

		update den_channels.legacy_import_memberships lim
		set membership_id = target.id,
			legacy_channel_id = 7593,
			imported_at = now()
		from den_channels.channel_memberships source
		join den_channels.channel_memberships target
			on target.channel_id = legacy_channel_id
			and target.member_identity = source.member_identity
			and target.membership_purpose = source.membership_purpose
		where source.channel_id = source_channel_id
			and lim.membership_id = source.id
			and not exists (
				select 1
				from den_channels.legacy_import_memberships existing
				where existing.membership_id = target.id
			);

		delete from den_channels.legacy_import_memberships lim
		using den_channels.channel_memberships source
		join den_channels.channel_memberships target
			on target.channel_id = legacy_channel_id
			and target.member_identity = source.member_identity
			and target.membership_purpose = source.membership_purpose
		where source.channel_id = source_channel_id
			and lim.membership_id = source.id;

		delete from den_channels.channel_memberships source
		using den_channels.channel_memberships target
		where source.channel_id = source_channel_id
			and target.channel_id = legacy_channel_id
			and target.member_identity = source.member_identity
			and target.membership_purpose = source.membership_purpose;

		update den_channels.channel_memberships
		set channel_id = legacy_channel_id,
			updated_at = now()
		where channel_id = source_channel_id;

		update den_channels.channel_read_cursors target
		set instance_id = coalesce(source.instance_id, target.instance_id),
			last_read_message_id = case
				when source.last_read_at >= target.last_read_at then source.last_read_message_id
				else target.last_read_message_id
			end,
			last_read_at = greatest(source.last_read_at, target.last_read_at)
		from den_channels.channel_read_cursors source
		where source.channel_id = source_channel_id
			and target.channel_id = legacy_channel_id
			and target.reader_type = source.reader_type
			and target.reader_identity = source.reader_identity;

		delete from den_channels.channel_read_cursors source
		using den_channels.channel_read_cursors target
		where source.channel_id = source_channel_id
			and target.channel_id = legacy_channel_id
			and target.reader_type = source.reader_type
			and target.reader_identity = source.reader_identity;

		update den_channels.channel_read_cursors
		set channel_id = legacy_channel_id
		where channel_id = source_channel_id;

		update den_channels.channel_project_links target
		set created_by = source.created_by,
			created_at = least(target.created_at, source.created_at),
			deleted_at = case
				when source.deleted_at is null or target.deleted_at is null then null
				else greatest(source.deleted_at, target.deleted_at)
			end
		from den_channels.channel_project_links source
		where source.channel_id = source_channel_id
			and target.channel_id = legacy_channel_id
			and target.project_id = source.project_id
			and target.link_kind = source.link_kind;

		update den_channels.legacy_import_project_links lipl
		set project_link_id = target.id,
			legacy_channel_id = 7593,
			imported_at = now()
		from den_channels.channel_project_links source
		join den_channels.channel_project_links target
			on target.channel_id = legacy_channel_id
			and target.project_id = source.project_id
			and target.link_kind = source.link_kind
		where source.channel_id = source_channel_id
			and lipl.project_link_id = source.id
			and not exists (
				select 1
				from den_channels.legacy_import_project_links existing
				where existing.project_link_id = target.id
			);

		delete from den_channels.legacy_import_project_links lipl
		using den_channels.channel_project_links source
		join den_channels.channel_project_links target
			on target.channel_id = legacy_channel_id
			and target.project_id = source.project_id
			and target.link_kind = source.link_kind
		where source.channel_id = source_channel_id
			and lipl.project_link_id = source.id;

		delete from den_channels.channel_project_links source
		using den_channels.channel_project_links target
		where source.channel_id = source_channel_id
			and target.channel_id = legacy_channel_id
			and target.project_id = source.project_id
			and target.link_kind = source.link_kind;

		update den_channels.channel_project_links
		set channel_id = legacy_channel_id
		where channel_id = source_channel_id;

		update den_channels.legacy_import_messages lim
		set legacy_channel_id = 7593,
			imported_at = now()
		where legacy_channel_id = source_channel_id
			and source = legacy_source;

		update den_channels.legacy_import_read_cursors lirc
		set channel_id = legacy_channel_id,
			imported_at = now()
		where channel_id = source_channel_id
			and source = legacy_source;

		update den_channels.legacy_import_channels
		set channel_id = legacy_channel_id,
			imported_at = now()
		where channel_id = source_channel_id
			and source = legacy_source
			and legacy_id = legacy_channel_id;

		perform setval(
			pg_get_serial_sequence('den_channels.channels', 'id'),
			(select greatest(coalesce(max(id), 1), legacy_channel_id) from den_channels.channels),
			true
		);
	end if;
end $$;

create or replace view den_channels.project_default_channel_id_splits as
select
	c.project_id,
	c.slug,
	c.id as successor_channel_id,
	lic.legacy_id as legacy_channel_id,
	lic.source as legacy_source,
	lic.imported_at
from den_channels.channels c
join den_channels.legacy_import_channels lic
	on lic.channel_id = c.id
where c.project_id is not null
	and c.kind = 'project_default'
	and c.archived_at is null
	and lic.legacy_id <> c.id;

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_channels_app') then
		grant select on den_channels.project_default_channel_id_splits to den_channels_app;
	end if;
end $$;
