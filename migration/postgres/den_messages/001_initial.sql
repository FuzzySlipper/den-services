create schema if not exists den_messages;

create table den_messages.messages (
    id bigint generated always as identity primary key,
    project_id text not null,
    task_id bigint,
    thread_id bigint references den_messages.messages(id) on delete set null,
    sender text not null,
    content text not null,
    intent text not null default 'general',
    metadata jsonb,
    created_at timestamptz not null default now()
);

create table den_messages.message_reads (
    message_id bigint not null references den_messages.messages(id) on delete cascade,
    agent text not null,
    read_at timestamptz not null default now(),
    primary key (message_id, agent)
);

create index messages_project_task_created_idx
    on den_messages.messages(project_id, task_id, created_at desc, id desc);

create index messages_project_created_idx
    on den_messages.messages(project_id, created_at desc, id desc);

create index messages_thread_created_idx
    on den_messages.messages(thread_id, created_at asc, id asc);

create index messages_project_intent_created_idx
    on den_messages.messages(project_id, intent, created_at desc, id desc);

create index messages_metadata_type_idx
    on den_messages.messages((metadata->>'type'));

create index messages_metadata_packet_kind_idx
    on den_messages.messages((metadata->>'packet_kind'));

create index messages_metadata_run_id_idx
    on den_messages.messages((metadata->>'run_id'));

create index message_reads_agent_message_idx
    on den_messages.message_reads(agent, message_id);

grant usage on schema den_messages to den_messages_app;
grant select, insert on den_messages.messages to den_messages_app;
grant select, insert on den_messages.message_reads to den_messages_app;
grant usage, select on all sequences in schema den_messages to den_messages_app;
