# Conversation Wake Target Contract

Task #4208 adds optional wake-target enrichment to conversation membership
responses so den-web can create Delivery wake intents for @mentions without
guessing a runtime instance.

## Membership Shape

Conversation membership responses may include:

```json
{
  "member_type": "agent",
  "member_identity": "den-mcp-runner",
  "profile_identity": "den-mcp-runner",
  "wake_policy": "mentions_only",
  "wake_target": {
    "profile": "den-mcp-runner",
    "instance_id": "den-mcp-runner@den-k8plus"
  }
}
```

`wake_target` uses the same shape as Delivery `target_identity`.

The field is omitted when Conversation cannot prove a wakeable target:

- the participant is not an agent;
- the membership is `left`;
- `wake_policy` is `never`;
- `profile_identity` is missing;
- no live Runtime instance exists for that profile;
- Runtime wake-target lookup is unavailable.

Conversation selects the newest live Runtime instance for the membership
profile, considering Runtime states `active`, `idle`, and `busy` wakeable.

## Den-Web Mention Flow

When a user posts a message containing an @mention:

1. Resolve the mention to a conversation membership.
2. If `membership.wake_target` is absent, do not create a wake intent.
3. If present, call Delivery through the Gateway successor route:

```http
POST /v1/delivery/intents
Idempotency-Key: mention:<channel_id>:<profile>:<nonce>
```

```json
{
  "target_identity": {
    "profile": "den-mcp-runner",
    "instance_id": "den-mcp-runner@den-k8plus"
  },
  "idempotency_key": "mention:42:den-mcp-runner:message-1001",
  "source_ref": "conversation:channels/42/messages/1001",
  "channel_message_id": 1001
}
```

The idempotency key must follow Delivery's four-part key shape:
`<operation>:<channel_id>:<target_profile>:<nonce>`. The target profile in the
key must match `target_identity.profile`.

## Boundary Rule

Conversation only exposes addressability evidence. It must never create,
claim, wake, retry, complete, or cancel executable work. Delivery remains the
only owner of executable wake intents.

Den-web should not call legacy den-channels, Runtime, Hermes, or pi-crew paths
to infer wake targets. It should use the successor Conversation membership
response and then create an explicit successor Delivery intent when a
`wake_target` is present.
