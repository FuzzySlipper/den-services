# den-services deployable service contract

`deployment/services.yaml` is the registry of deployable Go services. It keeps
the service name, primary binary path, config examples, env examples, systemd
unit, and loopback health/version URLs in one reviewed file.

Every registered service must:

- build from its registered `binary_path`;
- report `service version commit built_at` from `<binary> --version`;
- expose public `GET /health` and `GET /version` if it is an HTTP service;
- keep secrets in `/etc/den-services/<service>.env`, with placeholders only in
  repo env examples;
- bind health/version URLs on loopback by default;
- deploy as `den-go@<service>.service`.

The root deployment contract test builds every registered primary binary and
runs `--version`. The den-srv deploy script should additionally smoke
`/health` and `/version` after restart and compare the reported commit to the
binary it built.

For new Den Core lifeboat services, use
[`docs/lifeboat-service-substrate.md`](../docs/lifeboat-service-substrate.md)
as the service substrate guide. It documents the existing den-srv
`den-go@<service>.service` template, module/config/migration conventions, and
the acceptance baseline for the next batch of services.
