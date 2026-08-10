# Den fleet update helper

[`../scripts/update-den-fleet.sh`](../scripts/update-den-fleet.sh) is the
single operator entry point for updating the Den Go services and the Rusty
Crew/View deployment on the external machines. It deliberately delegates to
the deployment scripts that own each stack:

- Den services: `scripts/den-services-deploy.sh`, once for each currently
  active `den-go@*.service` unit (including the public `web-edge`).
- Den Web: the separate `/data/dev/den-web` checkout is fast-forwarded and
  published with `npm run deploy:den-srv` into `/data/services/den-web`; the
  Go `web-edge` unit keeps serving the stable release symlink on port 18080.
- Native Rusty Crew/View: Rusty View's installed
  `/usr/local/sbin/update-rusty-stack`, which fetches both repositories,
  creates the paired release, restarts both native units, and runs its own
  health/SQLite/MCP checks.

The helper never resets a checkout, overwrites a dirty tree, or guesses a
remote deployment layout. For `--target all`, it preflights both machines
before changing either one.

Preflight also rejects route migrations whose owning successor is not enrolled.
Before a fleet update, both hosts must have:

- `DEN_GATEWAY_KNOWLEDGE_UPSTREAM_TOKEN` in `gateway.env`;
- an active registered `den-go@handoff.service` with its PostgreSQL role,
  schema migration, config, and service environment;
- `DEN_HANDOFF_SERVICE_TOKEN` in `mcp.env`; and
- a `handoff` backend entry in MCP `config.yaml`.

These are deployment prerequisites, not values for the updater to invent.
Provision them from the existing owning-service tokens and database role using
the token policy and [`postgresql.md`](postgresql.md), deploy Handoff, smoke
`:8099/health`, then run fleet preflight again. The per-service deployer takes
transaction-local config snapshots and restores them with the previous binary
after a failed activation; `systemctl reset-failed` is part of that recovery.

## Nephew agentbox / m5

The documented Cloudflare SSH route is already available to the current local
agent:

```sh
scripts/update-den-fleet.sh --target m5 --preflight
scripts/update-den-fleet.sh --target m5
```

The defaults use SSH config `~/.ssh/nephew-agentbox.conf`, host alias
`nephew-agentbox`, remote user `jb`, and these verified paths:

- Den checkout: `/data/services/den-services`
- Rusty updater: `/usr/local/sbin/update-rusty-stack`
- Rusty stack root: `/data/services/rusty-stack`
- Rusty View instances: LAN ports `9347` and `9348`
- Den MCP: LAN port `5199`
- Den Web edge: LAN port `18080`
- Den Web source: `/data/dev/den-web`
- Den Web releases: `/data/services/den-web`

The m5 host updater is a root-owned wrapper around the checked-out Rusty View
updater. It strips the retired `wakeTimeout` initializer from the temporary
copy before activation; the original host updater and native JSON configs were
backed up under timestamped `before-wake-timeout` names. This keeps the Rusty
View checkout clean while allowing the current Rusty Crew runtime to boot.

The update is intentionally source-first: the Den checkout is fetched from
`origin/main` and detached at that exact revision, then the active service
units are rebuilt and smoked. The Rusty updater then performs its paired
Crew/View update. A failed preflight makes no changes. Den service deployment
is still per-unit rather than a cross-service transaction; the existing
deploy script performs its own binary rollback if an individual smoke fails.

The frontend publish keeps the live asset smoke enabled. By default the
helper sets `SKIP_CHECKS=1` for den-web because the full Nx/Playwright gate
belongs to CI/review and is not reliable on every service host's Node native
loader; set the target's `DEN_FLEET_*_DEN_WEB_SKIP_CHECKS=0` to repeat those
local checks. `npm ci` remains enabled by default so the lockfile is honored.

## Lore / lore-desktop

The Den network inventory identifies Lore as `192.168.1.25`. A read-only LAN
probe sees Den Web at `:18080` and Rusty View at `:9347`. Lore uses the older
single-instance native layout rather than the m5 dual-stack layout:

- Den checkout: `/data/services/den-services`
- Den Web source: `/data/dev/den-web`
- Den Web releases: `/data/services/den-web`
- Rusty source checkouts: `/data/dev/rusty-crew` and `/data/dev/rusty-view`
- Rusty runtime/updater: `/data/services/rusty-crew`
- Rusty service: the `lore` user-level `rusty-crew.service`
- Den MCP: loopback `127.0.0.1:5199`

Lore was originally deployed with a standalone `den-web.service`. The one-time
cutover disables that unit and enables `den-go@web-edge.service`; after the
cutover both machines use the same asset-release layout and the fleet helper
publishes Den Web after rebuilding the Go edge. If preflight reports the old
unit, stop and complete that migration rather than running an update against
the two competing listeners.

The updater runs as `lore`; only Den's system-unit restarts require the
machine-local `/etc/sudoers.d/den-fleet-lore` rule. That rule permits
`systemctl` and the staged MCP routes replacement, not unrestricted sudo.

1. Create a key-only SSH route for a deliberate operator account. Do not use
   the `lore` service account merely because the deployment guide uses it as a
   file owner. A typical local config is:

   ```sshconfig
   Host lore-desktop
       HostName 192.168.1.25
       User <operator-account>
       IdentityFile ~/.ssh/lore-desktop_ed25519
       IdentitiesOnly yes
   ```

2. Copy `fleet-update.conf.example` to `~/.config/den-fleet.conf`. The verified
   Lore paths are already represented in that example; keep the SSH target and
   identity local to this machine.

3. Run the non-mutating discovery check:

   ```sh
   DEN_FLEET_CONFIG="$HOME/.config/den-fleet.conf" \
     scripts/update-den-fleet.sh --target lore --preflight
   ```

   If the host ever changes deployment shape, preflight reports that rather
   than treating it as the native m5 updater. Stop there and choose or install
   a reviewed Lore-specific updater.

4. Once preflight reports a clean checkout, active registered Den units,
   passwordless sudo, an active `web-edge`, and the intended Rusty updater, run:

   ```sh
   DEN_FLEET_CONFIG="$HOME/.config/den-fleet.conf" \
     scripts/update-den-fleet.sh --target lore
   ```

   If an enrollment requires root-owned `/etc/den-services/*.env` changes,
   perform that bounded operator step first. Do not broaden the persistent Lore
   sudoers rule merely to make routine fleet runs mutate credentials.

When both targets pass preflight, the future all-machine command is:

```sh
DEN_FLEET_CONFIG="$HOME/.config/den-fleet.conf" \
  scripts/update-den-fleet.sh --target all
```

This is a repository script rather than a Codex skill because it needs to be
runnable from an ordinary shell, cron/job wrapper, or a future deployment
agent. SSH config and the untracked local config file hold access details;
the repository only records non-secret defaults and the documented paths.
