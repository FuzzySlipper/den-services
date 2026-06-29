# Documents Service Route Readiness

Task: 3695
Service: `den-services/documents`
Schema: `den_documents`
Role: `den_documents_app`

## Implemented Native Routes

- `POST /v1/projects/{project_id}/documents`
- `GET /v1/projects/{project_id}/documents`
- `GET /v1/projects/{project_id}/documents/search`
- `GET /v1/projects/{project_id}/documents/archived`
- `GET /v1/projects/{project_id}/documents/archived/search`
- `GET /v1/projects/{project_id}/documents/{slug}`
- `DELETE /v1/projects/{project_id}/documents/{slug}`
- `PATCH /v1/projects/{project_id}/documents/{slug}/visibility`
- `POST /v1/projects/{project_id}/documents/{slug}/archive-preflight`
- `GET /v1/projects/{project_id}/documents/{slug}/discussion`
- `POST /v1/projects/{project_id}/documents/{slug}/discussion/comments`
- `GET /v1/projects/{project_id}/documents/{slug}/discussion/threads`
- `GET /v1/documents`
- `GET /v1/documents/search`
- `GET /v1/documents/archived`
- `GET /v1/documents/archived/search`
- `POST /v1/discussion-threads`
- `GET /v1/discussion-threads`
- `GET /v1/discussion-threads/{thread_id}`
- `PATCH /v1/discussion-threads/{thread_id}`
- `POST /v1/discussion-threads/{thread_id}/comments`

## MCP Mapping Guidance

These tools can route to `documents` after import/parity verification:

- `store_document`
- `get_document`
- `list_documents`
- `search_documents`
- `delete_document`
- `update_document_visibility`
- `archive_document_preflight`
- `query_archived_documents`
- `get_document_discussion`
- `comment_on_document`
- `list_discussion_threads`
- `get_discussion_thread`
- `create_discussion_comment`
- `update_discussion_thread`

Do not route knowledge, agent-guidance ownership, or librarian tools to this
service. `den_knowledge_*`, `query_librarian`, `get_agent_guidance`,
`add_agent_guidance_entry`, and `delete_agent_guidance_entry` stay with their
own domains.

## Operational Notes

Project write policy is validated through `projects`. Archive preflight can
call an optional agent-guidance reference endpoint when configured; when it is
not configured the response sets `guidance_reference_check_ready=false`, so
callers know the guidance-reference portion was not authoritative.

No production MCP route flip or deployment has been performed.
