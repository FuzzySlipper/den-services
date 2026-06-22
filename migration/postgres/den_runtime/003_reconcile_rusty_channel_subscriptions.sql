do $$
declare
	superseded_channel_id constant bigint := 43;
	canonical_channel_id constant bigint := 7593;
begin
	update den_runtime.channel_subscriptions target
	set cursor_position = greatest(target.cursor_position, source.cursor_position),
		last_polled_at = case
			when target.last_polled_at is null then source.last_polled_at
			when source.last_polled_at is null then target.last_polled_at
			else greatest(target.last_polled_at, source.last_polled_at)
		end,
		wake_policy_override = coalesce(target.wake_policy_override, source.wake_policy_override),
		created_at = least(target.created_at, source.created_at)
	from den_runtime.channel_subscriptions source
	where source.channel_id = superseded_channel_id
		and target.channel_id = canonical_channel_id
		and target.runtime_instance_id = source.runtime_instance_id;

	update den_runtime.channel_subscription_cursors target_cursor
	set cursor_position = greatest(target_cursor.cursor_position, source_cursor.cursor_position),
		updated_at = case
			when target_cursor.updated_at is null then source_cursor.updated_at
			when source_cursor.updated_at is null then target_cursor.updated_at
			else greatest(target_cursor.updated_at, source_cursor.updated_at)
		end
	from den_runtime.channel_subscriptions source_subscription
	join den_runtime.channel_subscriptions target_subscription
		on target_subscription.runtime_instance_id = source_subscription.runtime_instance_id
		and target_subscription.channel_id = canonical_channel_id
	join den_runtime.channel_subscription_cursors source_cursor
		on source_cursor.subscription_id = source_subscription.subscription_id
	where source_subscription.channel_id = superseded_channel_id
		and target_cursor.subscription_id = target_subscription.subscription_id
		and target_cursor.cursor_kind = source_cursor.cursor_kind;

	delete from den_runtime.channel_subscription_cursors source_cursor
	using den_runtime.channel_subscriptions source_subscription
	join den_runtime.channel_subscriptions target_subscription
		on target_subscription.runtime_instance_id = source_subscription.runtime_instance_id
		and target_subscription.channel_id = canonical_channel_id
	join den_runtime.channel_subscription_cursors target_cursor
		on target_cursor.subscription_id = target_subscription.subscription_id
	where source_subscription.channel_id = superseded_channel_id
		and source_cursor.subscription_id = source_subscription.subscription_id
		and target_cursor.cursor_kind = source_cursor.cursor_kind;

	update den_runtime.channel_subscription_cursors cursor_row
	set subscription_id = target_subscription.subscription_id
	from den_runtime.channel_subscriptions source_subscription
	join den_runtime.channel_subscriptions target_subscription
		on target_subscription.runtime_instance_id = source_subscription.runtime_instance_id
		and target_subscription.channel_id = canonical_channel_id
	where source_subscription.channel_id = superseded_channel_id
		and cursor_row.subscription_id = source_subscription.subscription_id;

	delete from den_runtime.channel_subscriptions source_subscription
	using den_runtime.channel_subscriptions target_subscription
	where source_subscription.channel_id = superseded_channel_id
		and target_subscription.channel_id = canonical_channel_id
		and target_subscription.runtime_instance_id = source_subscription.runtime_instance_id;

	update den_runtime.channel_subscriptions
	set channel_id = canonical_channel_id
	where channel_id = superseded_channel_id;
end $$;

create or replace view den_runtime.rusty_channel_subscription_split_refs as
select
	subscription_id,
	runtime_instance_id,
	channel_id,
	cursor_position,
	last_polled_at,
	wake_policy_override,
	created_at
from den_runtime.channel_subscriptions
where channel_id = 43;

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_runtime_app') then
		grant select on den_runtime.rusty_channel_subscription_split_refs to den_runtime_app;
	end if;
end $$;
