create or replace view den_channels.project_default_channel_id_split_audit as
select
	s.project_id,
	s.slug,
	s.successor_channel_id,
	s.legacy_channel_id,
	lc.slug as legacy_id_current_slug,
	lc.project_id as legacy_id_current_project_id,
	lc.kind as legacy_id_current_kind,
	lc.archived_at as legacy_id_current_archived_at,
	case
		when lc.id is null then 'legacy_id_unoccupied_reconciliation_candidate'
		when lc.project_id = s.project_id
			and lc.kind = 'project_default'
			and lc.archived_at is null
			then 'legacy_id_points_to_same_active_project_default'
		when lc.project_id = s.project_id
			then 'legacy_id_points_to_same_project_noncanonical_channel'
		else 'legacy_id_reused_by_other_channel_expected_divergence'
	end as classification,
	case
		when lc.id is null then true
		when lc.project_id = s.project_id then true
		else false
	end as needs_reconciliation_decision
from den_channels.project_default_channel_id_splits s
left join den_channels.channels lc
	on lc.id = s.legacy_channel_id;

create or replace view den_channels.project_default_channel_id_reconciliation_candidates as
select *
from den_channels.project_default_channel_id_split_audit
where needs_reconciliation_decision;

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_channels_app') then
		grant select on den_channels.project_default_channel_id_split_audit to den_channels_app;
		grant select on den_channels.project_default_channel_id_reconciliation_candidates to den_channels_app;
	end if;
end $$;
