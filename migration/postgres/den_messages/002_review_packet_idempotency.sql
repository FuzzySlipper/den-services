create unique index messages_review_packet_idempotency_idx
    on den_messages.messages(project_id, (metadata->>'review_packet_id'))
    where metadata->>'review_packet_id' is not null;
