# Gateway legacy den-channels catch-all retirement, 2026-06-23

Task: Den `den-services` #3168.

## Decision

The Gateway no longer sends arbitrary unmatched legacy den-channels traffic to
the legacy den-channels service. Legacy `/api/*` channel and gateway
compatibility paths now fail closed with:

```json
{"error":"legacy_den_channels_api_retired"}
```

Canonical successor paths remain:

- Conversation: `/api/v1/conversation/*`
- Timeline: `/api/v1/timeline/*`
- Observation: `/api/v1/observation/*`
- Delivery: `/api/v1/delivery/*`
- Runtime: `/v1/runtime/*` where routed for runtime adapters

This is consistent with task #3190: the remaining non-Rusty project-default
channel id splits are catalogued and classified, but legacy `/api/*` channel
routes are retired and should not drive data-id reconciliation.

## Live Smoke Evidence

Run on `den-srv` against Den Web/Gateway on `127.0.0.1:18080`.

Legacy paths fail closed:

```text
GET /api/channels?projectId=den-services&limit=1
-> 410 legacy_den_channels_api_retired

GET /api/gateway/memberships?channelId=42
-> 410 legacy_den_channels_api_retired

GET /api/definitely-legacy-catchall-smoke
-> 410 legacy_den_channels_api_retired
```

Successor paths still work:

```text
GET /api/v1/conversation/channels?project_id=den-services&kind=project_default&limit=1
-> 200, channel id 42

GET /api/v1/timeline/projects/den-services/items?limit=1
-> 200

GET /api/v1/observation/lane?limit=1
-> 200
```

## Rollback

Rollback should be treated as an emergency compatibility window, not normal
operation.

1. Re-enable the previous Gateway/Den Web legacy `/api/*` proxy configuration
   from the most recent deployment backup.
2. Restart the Gateway/Den Web process that owns `127.0.0.1:18080`.
3. Re-run the legacy path smoke and confirm `/api/*` no longer returns
   `legacy_den_channels_api_retired`.
4. Keep the window short, identify the caller, and migrate it to the successor
   route before retiring the path again.

Do not reintroduce a broad catch-all in source configuration without a new Den
task and a named owner for the compatibility surface.
