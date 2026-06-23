# Project-default channel split catalogue, 2026-06-22

Task: Den `den-services` #3190.

Source query: `den_channels.project_default_channel_id_splits` on live
`den-srv` Postgres after task #3159 reconciled Rusty Crew.

## Decision

Do not bulk-rewrite the remaining 28 project-default channels immediately.
Rusty Crew needed data reconciliation because live legacy/Gateway membership
surfaces still observed channel `7593` while successor conversation/timeline
observed `43`.

For the remaining rows, live `/api/*` legacy channel routes now return
`410 legacy_den_channels_api_retired`, while the successor API serves
`/api/v1/conversation/*` with successor channel ids. The remaining mismatches
are therefore migration-provenance mismatches unless a live caller still depends
on legacy channel ids.

Migration `den_channels` v8 adds classified audit views:

- `den_channels.project_default_channel_id_split_audit`
- `den_channels.project_default_channel_id_reconciliation_candidates`

Use the candidate view for future cleanup if a caller still requires legacy ids.

## Legacy id unoccupied candidates

These could be reconciled to the legacy id without first moving an unrelated
channel row, but doing so would still change the successor channel id and must
move messages, memberships, read cursors, runtime subscriptions, observation
refs, and import maps.

| project_id | successor_channel_id | legacy_channel_id |
| --- | ---: | ---: |
| asha | 40 | 6963 |
| den-bridge | 36 | 667 |
| den-host | 37 | 668 |
| den-memory | 39 | 697 |
| den-services | 42 | 7288 |
| den-web | 31 | 584 |
| goblinbench | 32 | 601 |
| house | 34 | 634 |
| pi-crew | 35 | 642 |

Recommendation: leave successor ids canonical unless a specific live route or
caller is proven to require the legacy id.

## Legacy id reused by other channels

These must not be rewritten to the legacy id directly because that id currently
belongs to another channel row.

| project_id | successor_channel_id | legacy_channel_id | current owner of legacy id |
| --- | ---: | ---: | --- |
| agora-os | 10 | 1 | `store-test-20260620030941.814184830` |
| den-channels | 11 | 2 | `store-test-20260620030954.167186736` |
| den-core | 12 | 3 | `store-test-20260620031037.075200394` |
| den-desktop | 15 | 6 | `gateway-canary-2916-20260620035440` |
| den-gateway | 13 | 4 | `store-test-20260620031129.821662634` |
| den-hermes-bridge | 14 | 5 | `store-test-20260620033737.818318667` |
| den-mcp | 16 | 7 | `review-2916-20260620040307` |
| den-network | 17 | 8 | `pilot-canary-2917-20260620T044852Z-3046490` |
| den-publish | 26 | 17 | `project-den-network` |
| den-router | 18 | 9 | `pilot-canary-2917-20260620T044927Z-3048011` |
| discourser-mcp | 22 | 13 | `project-den-gateway` |
| md-post | 19 | 10 | `project-agora-os` |
| memory-smoke-shared-kb | 27 | 18 | `project-den-router` |
| memory-smoke-solo-researcher | 28 | 19 | `project-md-post` |
| patch | 20 | 11 | `project-den-channels` |
| quillforge | 21 | 12 | `project-den-core` |
| research | 23 | 14 | `project-den-hermes-bridge` |
| ruleweaver | 24 | 15 | `project-den-desktop` |
| voxelforge | 25 | 16 | `project-den-mcp` |

Recommendation: treat as expected divergence under successor-id authority. Any
future cleanup would need a broader channel-id compaction plan, not per-project
legacy-id reconciliation.
