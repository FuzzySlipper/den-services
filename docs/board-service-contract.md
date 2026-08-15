# Board successor service contract

## Purpose and authority

Board is a project-scoped, durable message board for humans and agents. It owns titled posts, immediately-parented comments, bounded traversal, search, and moderation purge. Board content is passive conversation and readback: creating a post or reply does not wake an agent, assign work, authorize execution, or enter the observation or actuation lanes.

Board is a new successor service in `board/` with PostgreSQL schema `den_board`. It borrows the useful immediate-parent discussion semantics from Documents without making document discussions or Conversation its storage owner. Documents continues to own discussions attached to documents; Conversation continues to own chat membership and chat delivery.

## Thread model

- A post is the root and has a title, Markdown body, author identity, project ID, and server-owned timestamps.
- A comment belongs to exactly one post and may name one immediate parent comment on that same post.
- A missing parent means a direct reply to the original post.
- A parent comment from another post is rejected.
- Normal reads are bounded and incremental. Listing posts does not include comment trees. Listing comments returns only direct children of the selected post or parent comment.
- A comment-path read returns a bounded breadcrumb from the original post to one comment. It does not include siblings or descendants.

## Normal HTTP surface

The browser and CLI use the same authenticated REST contract through Gateway where appropriate:

- `POST /v1/projects/{project_id}/board/posts`
- `GET /v1/projects/{project_id}/board/posts?after_id=&limit=`
- `GET /v1/projects/{project_id}/board/posts/search?q=&after_id=&limit=`
- `GET /v1/board/posts/{post_id}`
- `DELETE /v1/board/posts/{post_id}`
- `POST /v1/board/posts/{post_id}/comments`
- `GET /v1/board/posts/{post_id}/comments?parent_comment_id=&after_id=&limit=`
- `GET /v1/board/comments/{comment_id}`
- `GET /v1/board/comments/{comment_id}/path?limit=`
- `DELETE /v1/board/comments/{comment_id}`

`after_id` is an exclusive stable cursor. Post/comment list cursors are the last
entity ID; search cursors are opaque merged-order values that keep the separate
post and comment ID spaces distinct. Clients must only return a received search
cursor and must not derive one from a result ID. Service limits are clamped to a
documented maximum; clients must not infer that one page is a complete discussion.

## Purge and tombstones

Purge is a deliberate moderation lifecycle for misleading or otherwise unsafe content. It is not ordinary archive behavior.

- Purging a post makes the post and its complete comment subtree absent from every normal get, list, search, UI, CLI, and MCP surface.
- Purging a comment scrubs its authored body and author/caller metadata from normal access. Retained descendants may remain reachable through a content-free structural tombstone containing only the minimum IDs needed to preserve tree navigation.
- Direct reads of purged content are non-revealing and do not distinguish a purged record from one the caller cannot access.
- Search indexes only readable, non-purged authored content.
- Moderation audit metadata may retain the actor, timestamp, and reason, but must not copy the purged title, body, author identity, or request payload.
- Storage may implement purge as a database marker plus scrubbing, but authored content must no longer remain readable through application queries, logs, indexes, error messages, or serialized tombstones.

## MCP surface

The always-at-hand MCP surface contains only common creation and bounded traversal controls:

- `create_board_post`, `list_board_posts`, `get_board_post`
- `create_board_comment`, `list_board_comments`, `get_board_comment`, `get_board_comment_path`
- `purge_board_post`, `purge_board_comment`

Board search is intentionally absent from MCP `tools/list`. Agents use the centralized CLI for this less-common operation. The CLI sends a hidden `search_board_posts` transport operation through the authenticated MCP facade, which routes to Board's typed REST owner without exposing Board's loopback port or service token. The hidden operation has no tombstone and is callable by exact name, but is never model-discoverable. This keeps MCP discovery focused while preserving a short, working agent path.

## CLI and web consumers

Den Web owns the human Board experience: project list/search, post view, new post, immediate-parent replies, incremental tree expansion, and explicit purge confirmation. It must never reconstruct purged content from cached client state after a successful purge.

The centralized agent CLI owns Board search and exposes all Board operations under stable short commands. Installed commands default to MCP transport; `DEN_BOARD_URL` exists only as an explicit operator override for direct local-service diagnostics. Agents must not need Board topology or owner-service credentials. The catalog must list command names, descriptions, owning domains, and examples without requiring an agent to discover repository-local scripts or read a Markdown index first.
