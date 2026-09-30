# Current Den service host

As of 2026-09-10, the 23 Go services from `deployment/services.yaml` and the
PostgreSQL `17/denservices` cluster formerly on den-srv run on Proxmox CT106:

- Hostname: `den-services`; address: `192.168.1.5`.
- Unprivileged Debian 13 LXC: 2 cores, 4 GiB RAM, 32 GiB local ZFS storage.
- Web: `https://den.dragonden.stream/` through Caddy at `.6`.
- MCP: `http://192.168.1.5:5199/mcp`.
- PostgreSQL: `127.0.0.1:5433` for services, `.5:5433` for existing LAN clients.
- Existing `/data/services/<service>`, `/etc/den-services`, and systemd unit
  conventions are preserved. Run deployment commands on this host.
- Dedicated admin access: `~/server-access/den-services/connect`.
- Daily 06:00 Pacific Proxmox backup includes CT106 and writes to den-srv.

Services on den-k8 remain there. Crew review uses its existing reverse SSH
forward to port 8413 on the new host; it is not a service moved into the LXC.
Den-srv's Go services and PostgreSQL cluster are stopped with autostart disabled.

Verification included all service health/version checks, matching database
counts, MCP reads, an artifact upload/read/delete, browser task navigation,
and isolated PostgreSQL recovery from the first full LXC backup.

The old `.10:5199` listener is a temporary compatibility forward for already
running clients, not a second Den instance. It expires 2026-09-11 at 17:50
Pacific and is not enabled at boot. Clients should reconnect using `.5`.

Source rollback data is retained at
`den-srv:/mnt/storage/backup/den-services-migration-20260910/`.
After target writes have resumed, rollback requires transferring current state
back consistently; restarting the old database would lose new writes.
