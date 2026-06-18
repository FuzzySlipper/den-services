create table den_delivery.delivery_intents (
	id bigserial primary key,
	target_identity jsonb not null,
	state text not null,
	idempotency_key text not null,
	created_at timestamptz not null default now(),
	expires_at timestamptz not null,
	claimed_at timestamptz null,
	claim_token text null,
	claimed_by jsonb null,
	completed_at timestamptz null,
	source_ref text null,
	channel_message_id bigint null,
	cutover_watermark text null,
	constraint delivery_intents_idempotency_key_unique unique (idempotency_key),
	constraint delivery_intents_claim_token_unique unique (claim_token),
	constraint delivery_intents_expires_after_created check (expires_at > created_at),
	constraint delivery_intents_completed_after_created check (completed_at is null or completed_at >= created_at)
);

create index delivery_intents_state_idx on den_delivery.delivery_intents (state);
create index delivery_intents_expires_at_idx on den_delivery.delivery_intents (expires_at);
create index delivery_intents_channel_message_id_idx on den_delivery.delivery_intents (channel_message_id);

create table den_delivery.delivery_events (
	id bigserial primary key,
	intent_id bigint not null references den_delivery.delivery_intents (id),
	event_type text not null,
	payload jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now()
);

create index delivery_events_intent_id_created_at_idx on den_delivery.delivery_events (intent_id, created_at);
create index delivery_events_event_type_idx on den_delivery.delivery_events (event_type);

do $$
begin
	if exists (select 1 from pg_roles where rolname = 'den_delivery_app') then
		grant usage on schema den_delivery to den_delivery_app;
		grant select, insert, update, delete on all tables in schema den_delivery to den_delivery_app;
		grant usage, select on all sequences in schema den_delivery to den_delivery_app;
		alter default privileges in schema den_delivery grant select, insert, update, delete on tables to den_delivery_app;
		alter default privileges in schema den_delivery grant usage, select on sequences to den_delivery_app;
	end if;
end $$;
