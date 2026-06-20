# Conversation Gateway Canary Routes

Status: task #2916 route-staging note.

Gateway remains a router only. The conversation successor exposes clean
`/v1/conversation/...` service routes; Gateway stages access with the existing
`X-Den-Migrated-Functions: true` canary header and forwards a configured
successor service token to the upstream.

## Staged Route Shape

Add these routes before the `legacy-den-channels-all` catch-all:

```yaml
  - name: "conversation-writes-canary"
    path_pattern: "/v1/conversation"
    methods: ["POST", "PUT"]
    legacy_upstream_url: "http://127.0.0.1:18081"
    successor_upstream_url: "http://127.0.0.1:8084"
    successor_caller_auth:
      bearer_token: "${DEN_GATEWAY_CONVERSATION_WRITE_TOKEN}"
    successor_auth:
      bearer_token: "${DEN_GATEWAY_CONVERSATION_UPSTREAM_TOKEN}"

  - name: "conversation-reads-canary"
    path_pattern: "/v1/conversation"
    methods: ["GET"]
    legacy_upstream_url: "http://127.0.0.1:18081"
    successor_upstream_url: "http://127.0.0.1:8084"
    successor_caller_auth:
      bearer_token: "${DEN_GATEWAY_CONVERSATION_READ_TOKEN}"
    successor_auth:
      bearer_token: "${DEN_GATEWAY_CONVERSATION_UPSTREAM_TOKEN}"
```

No-header traffic still selects the legacy upstream and uses the Gateway default
caller token. Header-gated successor reads require the conversation read canary
token. Header-gated successor writes require the conversation write canary token.
Gateway replaces those inbound caller tokens with
`DEN_GATEWAY_CONVERSATION_UPSTREAM_TOKEN` before proxying to the conversation
service.

## Rollback

Before editing live routes:

```sh
cp /data/services/gateway/config/routes.yaml \
  /data/services/gateway/config/routes.yaml.before-2916-$(date -u +%Y%m%dT%H%M%SZ)
```

Rollback is a config restore plus Gateway restart:

```sh
cp /data/services/gateway/config/routes.yaml.before-2916-<timestamp> \
  /data/services/gateway/config/routes.yaml
systemctl restart den-go@gateway.service
curl -fsS http://127.0.0.1:8079/health
```

If only conversation canary needs to be disabled, remove the two
`conversation-*-canary` route entries and restart Gateway. Existing delivery,
observation, and legacy catch-all routes are otherwise unchanged.

## Smoke

Use an operator shell with `/etc/den-services/gateway.env` sourced. Do not paste
token values into task logs.

Required checks:

- `GET /v1/conversation/channels` with no canary header still reaches legacy
  fallback.
- `GET /v1/conversation/channels` with `X-Den-Migrated-Functions: true` and the
  read canary token reaches conversation successor.
- A `POST /v1/conversation/channels/{channel_id}/messages` request with the
  write canary token and `Idempotency-Key` creates exactly one message, and
  retrying the same key returns the same message ID.
- `PUT /v1/conversation/channels/{channel_id}/read-cursors` with the write
  canary token updates a human read cursor.
- Delivery, runtime, and observation write tables remain unchanged by the
  conversation smoke.
