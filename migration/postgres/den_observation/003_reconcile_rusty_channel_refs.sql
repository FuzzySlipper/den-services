update den_observation.activity_events
set payload = jsonb_set(payload, '{work_ref,channel_id}', to_jsonb(7593), false)
where payload #>> '{work_ref,project_id}' = 'rusty-crew'
	and payload #>> '{work_ref,channel_id}' = '43';
