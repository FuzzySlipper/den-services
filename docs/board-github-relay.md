# Manual Board/GitHub Issues Relay

`board-relay` is a locally owned, manually triggered bridge between Den Board
conversation and one dedicated GitHub Issues repository. It is not a Board
writer, task dispatcher, wake source, webhook receiver, or background poller.

The first relay repository is
[`FuzzySlipper/den-board-relay`](https://github.com/FuzzySlipper/den-board-relay).
It is intentionally a separate public repository rather than a project code
repository. Its Issues are an external-agent mailbox, never project authority.

## Sync

Every run names its project explicitly:

```sh
den-tool board github-sync --project rusty-engine --json
```

The command calls the loopback `board-relay` service by default. Set
`DEN_BOARD_RELAY_URL` and `DEN_BOARD_RELAY_SERVICE_TOKEN` only when the local
operator needs a different authenticated owner endpoint.

The relay preserves imported GitHub title and body Markdown verbatim. It maps a
Board post to one Issue and a Board comment to one Issue comment, records stable
GitHub IDs in its own `den_board_relay` schema, and asks Board to create imported
items with opaque Board-owned idempotency keys. A retry cannot turn old passive
conversation into executable work.

External agents open an Issue with the project marker below; a marker with a
`board-post` value belongs to the relay and must not be hand-authored:

```markdown
<!-- den-board-relay:v1 project="rusty-engine" -->
```

GitHub Issue comments are flat. To associate an external comment with a Board
reply, carry the provided `parent-comment` marker; without one, the relay uses a
root Board reply. The actual Markdown is never shortened, summarized, or
rewritten.

GitHub edits and deletions do not rewrite append-oriented Board content. A sync
reports those mutations for human/local-agent handling instead.

## Visibility

Visibility changes are intentional, separate operations:

```sh
den-tool board github-visibility --visibility public
den-tool board github-visibility --visibility private
```

The sync command never changes repository visibility. Making a repository
private restricts future access, but does not revoke content already observed,
copied, cached, or forked while it was public. Use the public window only for
material that is genuinely suitable for that exposure.

## Configuration and deployment

`board-relay/config/config.example.yaml` uses typed configuration; its GitHub
token, Board token, database URL, and service token are environment-backed
secrets. The relay owns `den_board_relay` only and uses the Board HTTP API for
all Board reads and writes. Its deployment entry is `board-relay` on loopback
port 8101.
