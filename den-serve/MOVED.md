# den-serve moved to crew-services

`den-serve` is local-machine tooling, so it now lives in crew-services with the
other local agent services:

- command: `crew-services/cmd/den-serve` (binary name unchanged)
- packages: `crew-services/internal/serve`, `crew-services/internal/devserver`
- install: `crew-services/scripts/install-den-serve.sh` (also installs the
  `den-serve-page` user unit)
- docs: `crew-services/docs/den-serve.md`

The move carried this directory's working tree as it was on 2026-09-29,
including its then-uncommitted restart/status-page changes. Those changes are
still uncommitted here; this copy is no longer built or installed. Remove this
directory (and `devserver-broker`, plus their `go.work`/`Makefile` entries)
once whoever owns those changes has confirmed the crew-services copy.

Changes made after the move, in crew-services:

- `Instance` sessions: several separately owned hosts of one project checkout.
  The crew playtest service starts one per session, so each tester gets its
  own world. Instances never adopt the preferred port or another host.
- The default managed port range is now 30300-30450, below Linux's ephemeral
  range, where a probed port could be taken by an outgoing connection while a
  product built.
- The launcher process is reaped, so a long-running owner keeps no zombies.
