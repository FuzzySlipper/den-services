# MCP Core-Off Canary Runbook

Task: den-services #3645.

Status: purge staging and bounded canary plan. This document does not authorize
stopping Core by itself; run the Core-off portion only during an explicit quiet
window after the operator confirms the remaining Core-retained surfaces are
acceptable blockers.

## Purged MCP Surface

The MCP facade hides retired tools from `tools/list` and returns a structured
`den_mcp_tool_retired` result for stale direct `tools/call` clients. Retired
tools are hidden instead of deleted so old clients fail with a clear tombstone
before backend routing.

Retired categories:

- Blackboard tools.
- Legacy dispatch and legacy publish helpers.
- Topic and curation tools without a successor owner.
- Core capability wrappers, including `invoke_capability` and `analyze_image`.
- Agent stream readback and wake-mode send helpers.
- Worker context packet builders.
- Worker run, pool, assignment, checkpoint, no-capacity, orchestrator lease, and
  active-agent projection tools.

No pi-crew or Hermes worker compatibility shim is preserved. Worker pool,
worker run, and contract ownership moves to rusty-crew/future runtime owners.

## Retained Core-Routed Surface

These tools intentionally remain visible until their successor implementation or
ownership decision is complete:

- `get_agent_guidance`, `list_agent_guidance_entries`,
  `add_agent_guidance_entry`, and `delete_agent_guidance_entry`.
- `query_librarian`.
- `get_task_workflow_summary` if the tasks successor has not yet taken over the
  composed workflow summary route.

If Core is stopped before these are replaced, classify failures on these tools
as expected Core-retained blockers, not successor regressions.

## Preflight

Run from den-srv with an operator shell. Do not paste token values into task
logs.

1. Confirm den-services MCP and successor services are healthy.
2. Save current service state and config versions:

   ```sh
   systemctl status den-go@mcp.service den-core.service
   cp /data/services/mcp/config/routes.yaml \
     /data/services/mcp/config/routes.yaml.before-3645-$(date -u +%Y%m%dT%H%M%SZ)
   ```

3. Confirm `tools/list` omits retired tools and still includes essential
   successor tools such as `get_task`, `list_tasks`, `get_messages`,
   `store_document`, `search_documents`, `den_knowledge_search`, and review
   tools.
4. Direct-call one retired tool and confirm the result has
   `error=den_mcp_tool_retired`, `retired=true`, and `hidden_from=tools/list`.
5. Pause live agent work before any Core stop/isolation step.

## Core-Off Canary

Prefer a reversible service stop over network or database changes:

```sh
systemctl stop den-core.service
systemctl is-active den-go@mcp.service
```

Do not stop Postgres or `den-go@mcp.service`.

Run read-heavy smokes first:

- Projects: `list_projects`, then `get_project` for `den-services`.
- Tasks: `get_task`, `list_tasks`, and `next_task` on a known project.
- Messages: `get_messages` and `get_thread` on known existing records.
- Documents: `get_document`, `search_documents`, `list_archived_documents`, and
  `query_archived_documents` as applicable.
- Review: `list_review_rounds`, `list_review_findings`, and a non-mutating
  read against a recent task.
- Knowledge: `den_knowledge_search`, `den_knowledge_get`, and
  `den_knowledge_guide`.
- Guidance and librarian: run only if the operator has accepted that they may
  report expected Core-retained blockers until successor ownership lands.
- Retired behavior: `tools/list` remains purged and stale direct calls still
  return `den_mcp_tool_retired`.

Use disposable writes only if needed to prove a specific successor route. Avoid
wake, dispatch, worker, or curation writes during this canary.

## Failure Classification

- Essential successor failure: restore Core immediately and create a blocking
  follow-up for the owning successor service or MCP route.
- Expected Core-retained blocker: record the tool and owning follow-up; do not
  treat the canary as Core-free ready.
- Retired direct call reaches Core or another backend: restore Core if needed and
  fix the MCP tombstone before retrying.
- Retired tool appears in `tools/list`: fix the registry purge before retrying.

## Restore

Always restore Core after the bounded canary unless the operator explicitly
chooses to leave it off after all critical smokes pass.

```sh
systemctl start den-core.service
systemctl is-active den-core.service
curl -fsS http://127.0.0.1:<core-port>/health
curl -fsS http://127.0.0.1:<mcp-port>/health
```

Record the canary window, commands, failures, follow-up task IDs, and final Core
state on task #3645 or the deployment task that executes this runbook.
