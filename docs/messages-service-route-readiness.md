# Messages Service Route Readiness

Task: 3694
Service: `den-services/messages`
Schema: `den_messages`
Role: `den_messages_app`

## Implemented Native Routes

- `POST /v1/projects/{project_id}/messages`
- `GET /v1/projects/{project_id}/messages`
- `GET /v1/messages/{message_id}`
- `GET /v1/projects/{project_id}/messages/wait`
- `GET /v1/projects/{project_id}/messages/threads/{thread_id}`
- `GET /v1/threads/{thread_id}`
- `POST /v1/messages/read`
- `POST /v1/projects/{project_id}/notifications`
- `GET /v1/user-notifications`
- `GET /v1/projects/{project_id}/user-notifications`
- `POST /v1/user-notifications/read`
- `POST /v1/projects/{project_id}/tasks/{task_id}/packets/context`
- `GET /v1/projects/{project_id}/tasks/{task_id}/packets/latest`
- `GET /v1/projects/{project_id}/packets/{message_id}/worker-prompt`
- `POST /v1/projects/{project_id}/tasks/{task_id}/completions`
- `GET /v1/projects/{project_id}/tasks/{task_id}/completions/latest`

## MCP Mapping Guidance

These Core-style message tools can route to `messages` after import/parity verification:

- `send_message`
- `get_messages`
- `wait_for_messages`
- `get_thread`
- `mark_read`
- `send_user_notification`
- `get_user_notifications`
- `mark_notifications_read`
- `get_latest_task_packet`
- `render_worker_prompt`
- `get_latest_worker_completion`

Context packet preparation can route here once callers accept the successor packet content format. Packet metadata preserves the contract keys: `schema`, `schema_version`, `type`, `packet_kind`, `role`, `task_id`, `reference_only_launch`, and `completion_reporting_mode`.

Do not route `post_worker_completion_packet` to this service as a full replacement. The implemented completion route is storage-only for durable packet records; it does not validate worker runs, transition assignments, wake gates, retry work, or complete executable state.

## Operational Notes

The service validates project writability through `projects` and task/project ownership through `tasks`; missing upstream URLs fail closed. Message replay remains passive: reading stored messages or packets must not create, claim, wake, retry, complete, or cancel executable work.
