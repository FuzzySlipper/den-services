# Guidance Packet Audit

Status: task #3628 guidance context hygiene and MCP facade follow-up.

## Current Shape

Direct Core REST on den-srv was checked on 2026-07-01:

- route: `/api/projects/den-services/agent-guidance/entries?includeGlobal=true`
- entry count: 19
- resolved route: `/api/projects/den-services/agent-guidance`
- resolved JSON bytes: about 43 KB
- generated guidance content: about 33K characters

This is substantially smaller than the original 2026-06-28 observation of
about 116K guidance-content characters and a 149 KB Streamable MCP response.
The current packet already uses bootstrap replacements for the largest global
policies and den-services-specific policy docs.

## Entry Size Snapshot

Largest referenced document bodies at audit time:

| Chars | Scope | Reference | Importance | Audience |
| ---: | --- | --- | --- | --- |
| 4,134 | `_global` | `_global/agent-clarification-and-workdir-boundary-policy` | required | planner, runner, orchestrator, worker |
| 2,619 | `_global` | `_global/gateway-decommission-routing-policy` | required | planner, runner, orchestrator, worker |
| 2,124 | `_global` | `_global/agent-task-management-policy` | required | all |
| 1,796 | `_global` | `_global/agent-temp-file-policy` | important | all |
| 1,476 | `_global` | `_global/agent-guidance-source-policy` | required | all |
| 1,281 | `_global` | `_global/den-connectivity-policy` | required | all |
| 941 | `_global` | `_global/den-agent-operating-workflow-bootstrap` | required | all |
| 791 | `_global` | `_global/agent-review-loop-policy-bootstrap` | required | all |
| 777 | `_global` | `_global/agent-worker-runtime-policy-bootstrap` | required | all |
| 718 | `den-services` | `den-services/mcp-successor-service-and-den-mcp-retirement-bootstrap` | important | planner, runner, orchestrator, worker |
| 716 | `den-services` | `den-services/token-operating-policy-bootstrap` | important | planner, runner, coder, reviewer |

The remaining included documents are each under 800 body characters.

## Recommendation

The current default guidance packet is acceptable for bootstrap use. Keep the
bootstrap entries as the default guidance set and keep long-form originals as
read-on-demand documents or knowledge references.

Do not route guidance through librarian or knowledge. Guidance remains a
deterministic policy-reference packet, not a retrieval aggregator.

## Facade Fix

The den-services MCP facade still needed transport hardening:

- Core can return `404 {"message":"Session not found"}` when the facade has a
  stale backend MCP session. The facade now treats that response as a signal to
  reinitialize the backend session and retry the tool call.
- Streamable MCP responses can legitimately contain a guidance-sized single
  `data:` event. The SSE parser now raises its scanner buffer cap to 4 MiB and
  reports scanner errors distinctly instead of collapsing them into
  `missing message data`.

These fixes are transport compatibility only. They do not increase the default
guidance packet policy budget, and they do not change guidance entry ownership.
