# Deploying a new single-machine den-services instance

Status: proven on a clean Fedora 43 machine on 2026-07-16, including Den Web as
the LAN-facing operator UI. The Debian-family path uses the distribution's
current PostgreSQL major rather than assuming a fixed version; record the exact
major and unit name in each deployment's evidence.

This guide installs a new, empty den-services instance. It assumes PostgreSQL,
the den-services checkout, every deployed service, and internal service traffic
all live on one systemd machine. PostgreSQL and service backends stay on
loopback. Only an intentional front door should be exposed to the LAN. The
proven local-agent profile keeps MCP and Gateway on loopback; Den Web is the
only LAN listener and does not require a browser access token.

An all-in-one trusted agentbox may intentionally expose MCP on
`0.0.0.0:5199` so local and LAN agent runtimes share one stable endpoint. In
that profile, MCP is a second intentional front door alongside Den Web. Keep
Gateway, PostgreSQL, and owner services on loopback. Do not use this
unauthenticated profile outside a trusted agent network; use the hardened token
profile when the network boundary is broader.

This is not a data migration guide. Do not run the legacy import or backfill
tools named in [No data import](#no-data-import).

## Current clean-instance caveats

The complete core-service procedure has been exercised on a second machine.
These limitations remain:

- Some service environment examples still contain older config-path variable
  names or `127.0.0.1:5432/den` database URLs. Use the canonical values in this
  guide and treat the examples as variable inventories, not copy-and-run files.
- `gateway/config/routes.example.yaml` defaults to legacy den-channels on
  `127.0.0.1:5080`. A den-services-only instance needs a reviewed
  successor-only route table before Gateway is useful.
- `mcp/config/config.example.yaml` and `mcp/routes.example.yaml` still include a
  legacy den-core backend on `127.0.0.1:5299`. Remove legacy-only routes or
  deliberately provide that backend. A fully successor-only MCP route table is
  a first-deployment follow-up.
- Optional integrations need their own setup: Review's GitHub integration,
  Doc Publish's writable Git checkout and push credentials, and Visual
  Inspect's model endpoint. The proven local profile disables Review's GitHub
  polling and omits those optional services.
- Node 22.22.2 can build and run Den Web, but the checked-in Nx/Playwright
  configuration fails while loading its browser-test config with
  `TypeError: Cannot convert undefined or null to object`. Upgrade to a newer
  patch release in the same maintained Node channel; Node 22.23.1 is proven.
  The fallback below is only for distributions whose maintained channel cannot
  yet provide the corrected runtime.

Record each correction in [First-deployment evidence](#first-deployment-evidence)
and update this guide in the same change.

## Target layout

The guide uses the existing deployment contract:

```text
/data/services/den-services/         repository checkout
/data/dev/den-web/                   Den Web repository checkout
/var/lib/pgsql/data/                 PostgreSQL data on Fedora packages
/data/services/postgresql/data/      PostgreSQL data in the Debian example
/data/services/<service>/            deployed service root
/data/services/den-web/              Den Web releases and stable symlinks
/etc/den-services/postgresql.env     migration and app-role settings/secrets
/etc/den-services/<service>.env      service settings/secrets
/etc/systemd/system/den-go@.service  shared service unit template
/data/services/web-edge/            Go static/API edge service
```

Default local endpoints are registered in
[`services.yaml`](./services.yaml). PostgreSQL uses
`127.0.0.1:5433/denservices`; port 5433 preserves compatibility with the first
deployment and avoids assuming that port 5432 is free.

## Deployment profiles

Choose one posture before configuring PostgreSQL or service environment files:

- **Local-trust profile (proven):** every backend listener, including MCP and
  Gateway, binds only to `127.0.0.1`; PostgreSQL trusts local Unix-socket and
  IPv4 loopback connections; MCP permits unauthenticated local calls; and the
  constant `local-den` is supplied only where current service code still
  requires an internal bearer value. It is not a secret or a security
  boundary. When Den Web is installed, only its `0.0.0.0:18080` listener is
  opened to the trusted LAN, with no browser access token.
- **Hardened profile:** PostgreSQL uses SCRAM passwords, protected services use
  distinct bearer tokens, and any LAN front door is deliberately firewalled.

The local-trust profile gives local agents a token-free MCP endpoint while
keeping every backend closed to network clients. Den Web is the deliberate UI
exception. Some direct REST services still require the internal compatibility
bearer because their current API middleware does not have a local-dev bypass;
normal local agents should use MCP.

## Assumptions

- Fedora 43 or a Debian-family system with systemd. The proven host used
  PostgreSQL 18.3; PostgreSQL 17 remains the Debian example.
- The interactive deployment/service account can use non-interactive sudo and
  owns the service roots. The proven account was `lore:lore`.
- Go 1.26 or newer is installed; every module currently declares `go 1.26`.
- Node.js 22 or newer and npm are installed when Den Web is included. Node
  22.23.1 and npm 10.9.8 are proven for the complete build and Playwright
  suite; avoid Node 22.22.2 as described in the caveats.
- The machine can clone `https://github.com/FuzzySlipper/den-services.git` and
  `https://github.com/FuzzySlipper/den-web.git`, or equivalent repository URLs.
- All commands are run on the new machine.

If the OS, account names, paths, PostgreSQL major version, or port differ, adapt
the commands consistently and record the deviation.

## 1. Install host prerequisites

On Fedora, install the basic tools and PostgreSQL extension package:

```sh
sudo dnf install -y \
  ca-certificates \
  curl \
  git \
  nodejs \
  npm \
  openssl \
  postgresql \
  postgresql-contrib \
  postgresql-server \
  python3 \
  tar
```

`postgresql-contrib` is required because the migrations create `pg_trgm`.

On Debian, install the basic tools first:

```sh
sudo apt-get update
sudo apt-get install -y \
  ca-certificates \
  curl \
  git \
  openssl \
  postgresql-common \
  python3
```

Install Node and npm only when they are absent:

```sh
command -v node >/dev/null || sudo apt-get install -y nodejs
command -v npm >/dev/null || sudo apt-get install -y npm
```

Keeping these as separate, conditional transactions matters when a host already
uses NodeSource or another maintained Node repository: those `nodejs` packages
bundle npm and conflict with Ubuntu's separate `npm` package. If the selected
Node package is older than Node 22, install a current Node release from one
maintained source before deploying Den Web. Verify both tools:

```sh
node --version
npm --version
```

Install Go 1.26 or newer. Fedora 43's packaged Go was 1.25 during the proven
deployment, so that host used the checksum-verified upstream Go 1.26.4 archive:

```sh
den_go_tmp="$(mktemp -d)"
curl -fL --retry 3 \
  -o "$den_go_tmp/go.tar.gz" \
  https://go.dev/dl/go1.26.4.linux-amd64.tar.gz
echo '1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f  '"$den_go_tmp/go.tar.gz" | \
  sha256sum -c -
test ! -e /usr/local/go
sudo tar -C /usr/local -xzf "$den_go_tmp/go.tar.gz"
sudo ln -s /usr/local/go/bin/go /usr/local/bin/go
sudo ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt
rm -rf -- "$den_go_tmp"
```

For another version or architecture, take the archive and checksum from the
official Go downloads page rather than copying these values. Verify:

```sh
go version
```

On the proven low-security Fedora host, SELinux was made permissive before
executing service binaries from `/data/services`:

```sh
sudo cp -a /etc/selinux/config /etc/selinux/config.pre-den-services
sudo setenforce 0
sudo sed -i 's/^SELINUX=.*/SELINUX=permissive/' /etc/selinux/config
getenforce
```

`getenforce` must print `Permissive`. A hardened Fedora deployment should keep
SELinux enforcing and add reviewed labels/policy instead. Firewalld needs no
Den port changes for this loopback-only topology.

The proven low-friction setup uses the interactive administrator as the service
owner. Capture that identity and confirm non-interactive sudo before
continuing:

```sh
DEN_SERVICE_USER="$(id -un)"
DEN_SERVICE_GROUP="$(id -gn)"
printf 'service owner: %s:%s\n' "$DEN_SERVICE_USER" "$DEN_SERVICE_GROUP"
sudo -n id
```

On Lore those values were `lore:lore`. A dedicated service account is also
valid, but log in as that account and reset both variables before cloning; the
deploy script requires its caller to own the service roots.

Create the shared roots:

```sh
sudo install -d -m 0755 -o root -g root /data/services
sudo install -d -m 0755 -o root -g root /etc/den-services
sudo install -d -m 0755 \
  -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" \
  /data/services/den-services
```

Clone the public repository as the service owner:

```sh
git clone \
  https://github.com/FuzzySlipper/den-services.git \
  /data/services/den-services
cd /data/services/den-services
```

Record the deployment commit:

```sh
git rev-parse HEAD
git status --short
```

The worktree must be clean because the deployment script refuses dirty
deployments.

Before installing anything, compare the registered endpoints with the host's
current listeners:

```sh
sudo ss -ltnp
awk -F'"' '/(health_url|version_url):/ { print $2 }' \
  deployment/services.yaml | sort -u
```

Resolve every collision deliberately. Move or reconfigure an existing service
only with its own rollback and smoke, or omit the colliding Den service and
record the omission. Do not let the deployment script smoke an unrelated
process that happens to own a registered port; a health response from the wrong
binary is not deployment evidence.

## 2. Install the dedicated PostgreSQL cluster

### 2.1 Fedora cluster (proven path)

Fedora uses one packaged cluster at `/var/lib/pgsql/data` and the unit
`postgresql.service`. Initialize it directly on the Den port:

```sh
test -f /var/lib/pgsql/data/PG_VERSION || \
  sudo postgresql-setup --initdb --port=5433
```

Back up and normalize the listener and host authentication files:

```sh
sudo cp -a /var/lib/pgsql/data/postgresql.conf \
  /var/lib/pgsql/data/postgresql.conf.pre-den-services
sudo cp -a /var/lib/pgsql/data/pg_hba.conf \
  /var/lib/pgsql/data/pg_hba.conf.pre-den-services

sudo sed -i \
  "s/^#listen_addresses = .*/listen_addresses = '127.0.0.1'/" \
  /var/lib/pgsql/data/postgresql.conf
sudo sed -i -E \
  's/^(local[[:space:]]+all[[:space:]]+all[[:space:]]+)peer$/\1trust/;
   s/^(host[[:space:]]+all[[:space:]]+all[[:space:]]+127\.0\.0\.1\/32[[:space:]]+)ident$/\1trust/' \
  /var/lib/pgsql/data/pg_hba.conf

sudo systemctl enable --now postgresql.service
```

Those `trust` rules are the local-trust profile. For the hardened profile, use
`scram-sha-256` for the two `all` application rules and provision passwords as
described below. Never add a LAN CIDR to `pg_hba.conf` for this topology.

Verify the effective settings rather than trusting the file edit:

```sh
sudo -u postgres psql -p 5433 -Atqc \
  'show listen_addresses; show port'
pg_isready -h 127.0.0.1 -p 5433
sudo ss -ltnp | grep ':5433'
```

The listener must be `127.0.0.1:5433`.

### 2.2 Prevent an unwanted default Debian cluster

On a fresh Debian-family host, configure `postgresql-common` before installing
the PostgreSQL server package:

```sh
if grep -q '^#\?create_main_cluster' \
  /etc/postgresql-common/createcluster.conf; then
  sudo sed -i \
    's/^#\?create_main_cluster.*/create_main_cluster = false/' \
    /etc/postgresql-common/createcluster.conf
else
  printf '%s\n' 'create_main_cluster = false' | \
    sudo tee -a /etc/postgresql-common/createcluster.conf >/dev/null
fi
grep '^create_main_cluster' /etc/postgresql-common/createcluster.conf
```

Install the distribution's current PostgreSQL server and client:

```sh
sudo apt-get install -y postgresql postgresql-client
DEN_POSTGRES_MAJOR="$(
  psql --version | awk '{ print $3 }' | cut -d. -f1
)"
test -n "$DEN_POSTGRES_MAJOR"
test -f \
  "/usr/share/postgresql/$DEN_POSTGRES_MAJOR/extension/pg_trgm.control"
printf 'PostgreSQL major: %s\n' "$DEN_POSTGRES_MAJOR"
```

The `pg_trgm` extension ships with PostgreSQL 18's Ubuntu package even though
Ubuntu 26.04 does not publish a separate `postgresql-contrib` metapackage. The
explicit control-file check above fails early when another Debian-family
distribution packages it separately.

Confirm that package installation did not create an unwanted
`$DEN_POSTGRES_MAJOR/main` cluster:

```sh
pg_lsclusters
```

If a `main` cluster exists for that major, stop and decide whether it belongs
to another application. Do not delete an existing cluster as part of this
guide.

### 2.3 Create the Debian denservices cluster

```sh
DEN_POSTGRES_MAJOR="$(
  psql --version | awk '{ print $3 }' | cut -d. -f1
)"
sudo install -d -m 0755 -o root -g root /data/services/postgresql
sudo pg_createcluster \
  --datadir=/data/services/postgresql/data \
  --port=5433 \
  --start \
  "$DEN_POSTGRES_MAJOR" denservices
```

Keep PostgreSQL loopback-only:

```sh
sudo -u postgres psql -p 5433 -d postgres \
  -c "alter system set listen_addresses = '127.0.0.1'"
```

Create a small, explicit host authentication file matching the selected
deployment profile. For the local-trust profile, keep peer access for the local
PostgreSQL administrator and trust only Unix-socket and IPv4 loopback
application connections:

```sh
sudo cp "/etc/postgresql/$DEN_POSTGRES_MAJOR/denservices/pg_hba.conf" \
  "/etc/postgresql/$DEN_POSTGRES_MAJOR/denservices/pg_hba.conf.pre-den-services"
sudo tee \
  "/etc/postgresql/$DEN_POSTGRES_MAJOR/denservices/pg_hba.conf" >/dev/null <<'EOF'
local   all   postgres                              peer
local   all   all                                   trust
host    all   all   127.0.0.1/32                    trust
EOF
sudo chown postgres:postgres \
  "/etc/postgresql/$DEN_POSTGRES_MAJOR/denservices/pg_hba.conf"
sudo chmod 0640 \
  "/etc/postgresql/$DEN_POSTGRES_MAJOR/denservices/pg_hba.conf"
sudo systemctl restart \
  "postgresql@$DEN_POSTGRES_MAJOR-denservices.service"
sudo systemctl enable \
  "postgresql@$DEN_POSTGRES_MAJOR-denservices.service"
```

For the hardened profile, use the same file and commands but replace both
`trust` application methods with `scram-sha-256`; then provision the distinct
passwords and password-bearing URLs described below. Do not combine SCRAM HBA
rules with the passwordless URLs from the local-trust profile.

Do not add a LAN `pg_hba.conf` entry. Every service in this deployment connects
over loopback.

### 2.4 Create the migration role and empty database

For the local-trust profile, create a passwordless loopback-only migration
identity and the empty database:

```sh
sudo -u postgres psql -p 5433 -Atqc \
  "select 1 from pg_roles where rolname='den_migration'" | grep -qx 1 || \
  sudo -u postgres createuser -p 5433 \
    --createdb --createrole --login den_migration
sudo -u postgres psql -p 5433 -Atqc \
  "select 1 from pg_database where datname='denservices'" | grep -qx 1 || \
  sudo -u postgres createdb -p 5433 \
    --owner=den_migration denservices
```

For the hardened profile, generate a URL-safe password. Keep it in the operator
shell only until the secret file is written:

```sh
DEN_NEW_MIGRATION_PASSWORD="$(openssl rand -hex 32)"
```

Create or normalize the privileged offline migration identity and make it owner
of the new empty database:

```sh
sudo -u postgres psql -p 5433 -d postgres \
  --set=migration_password="$DEN_NEW_MIGRATION_PASSWORD" <<'SQL'
select format(
  'create role den_migration login password %L createrole createdb',
  :'migration_password'
)
where not exists (
  select 1 from pg_roles where rolname = 'den_migration'
)
\gexec

alter role den_migration
  login createrole createdb password :'migration_password';

select 'create database denservices owner den_migration'
where not exists (
  select 1 from pg_database where datname = 'denservices'
)
\gexec

alter database denservices owner to den_migration;
SQL
```

Runtime services must never receive this migration role or its connection URL.

### 2.5 Create the PostgreSQL environment file

For the local-trust profile, `/etc/den-services/postgresql.env` contains no
secrets and may use mode `0644`. Use the passwordless migration URL and set
every `DEN_*_APP_PASSWORD` value in the inventory below to the compatibility
value `local-den`:

```sh
sudo install -m 0644 -o root -g root /dev/null \
  /etc/den-services/postgresql.env
sudoedit /etc/den-services/postgresql.env
```

```text
DEN_MIGRATION_DATABASE_URL=postgres://den_migration@127.0.0.1:5433/denservices?sslmode=disable
DEN_DELIVERY_APP_PASSWORD=local-den
DEN_RUNTIME_APP_PASSWORD=local-den
DEN_OBSERVATION_APP_PASSWORD=local-den
DEN_CHANNELS_APP_PASSWORD=local-den
DEN_TIMELINE_APP_PASSWORD=local-den
DEN_ARTIFACTS_APP_PASSWORD=local-den
DEN_PROJECTS_APP_PASSWORD=local-den
DEN_TASKS_APP_PASSWORD=local-den
DEN_MESSAGES_APP_PASSWORD=local-den
DEN_REVIEW_APP_PASSWORD=local-den
DEN_DOCUMENTS_APP_PASSWORD=local-den
DEN_KNOWLEDGE_APP_PASSWORD=local-den
DEN_GUIDANCE_APP_PASSWORD=local-den
```

The role bootstrap currently requires password variables even when
`pg_hba.conf` trusts loopback. The fixed value only satisfies that interface;
it is not presented by the passwordless database URLs used by services.

For the hardened profile, generate a distinct URL-safe password for every app
role:

```sh
openssl rand -hex 32
```

Create `/etc/den-services/postgresql.env` with mode `0600`. Replace every
placeholder; the migration password must be the value generated above.

```sh
sudo install -m 0600 -o root -g root /dev/null \
  /etc/den-services/postgresql.env
sudoedit /etc/den-services/postgresql.env
```

Use this inventory:

```text
DEN_POSTGRES_HOST=127.0.0.1
DEN_POSTGRES_PORT=5433
DEN_POSTGRES_DB=denservices
DEN_MIGRATION_USER=den_migration
DEN_MIGRATION_PASSWORD=<migration-password>
DEN_MIGRATION_DATABASE_URL=postgres://den_migration:<migration-password>@127.0.0.1:5433/denservices?sslmode=disable

DEN_DELIVERY_APP_PASSWORD=<unique-password>
DEN_RUNTIME_APP_PASSWORD=<unique-password>
DEN_OBSERVATION_APP_PASSWORD=<unique-password>
DEN_CHANNELS_APP_PASSWORD=<unique-password>
DEN_TIMELINE_APP_PASSWORD=<unique-password>
DEN_ARTIFACTS_APP_PASSWORD=<unique-password>
DEN_PROJECTS_APP_PASSWORD=<unique-password>
DEN_TASKS_APP_PASSWORD=<unique-password>
DEN_MESSAGES_APP_PASSWORD=<unique-password>
DEN_REVIEW_APP_PASSWORD=<unique-password>
DEN_DOCUMENTS_APP_PASSWORD=<unique-password>
DEN_KNOWLEDGE_APP_PASSWORD=<unique-password>
DEN_GUIDANCE_APP_PASSWORD=<unique-password>
```

Hex-generated passwords need no URL escaping. If a different password format
is used, percent-encode it in PostgreSQL URLs.

Clear the temporary shell variable after the file is complete:

```sh
unset DEN_NEW_MIGRATION_PASSWORD
```

### 2.6 Create application roles before applying migrations

This order is required. Migrations grant schema access only to roles that
already exist.

Run the checked-in role bootstrap as root so it can read the secret file:

```sh
sudo bash -c '
  set -euo pipefail
  set -a
  . /etc/den-services/postgresql.env
  set +a
  cd /data/services/den-services
  psql "$DEN_MIGRATION_DATABASE_URL" \
    -v DEN_DELIVERY_APP_PASSWORD="$DEN_DELIVERY_APP_PASSWORD" \
    -v DEN_RUNTIME_APP_PASSWORD="$DEN_RUNTIME_APP_PASSWORD" \
    -v DEN_OBSERVATION_APP_PASSWORD="$DEN_OBSERVATION_APP_PASSWORD" \
    -v DEN_CHANNELS_APP_PASSWORD="$DEN_CHANNELS_APP_PASSWORD" \
    -v DEN_TIMELINE_APP_PASSWORD="$DEN_TIMELINE_APP_PASSWORD" \
    -v DEN_ARTIFACTS_APP_PASSWORD="$DEN_ARTIFACTS_APP_PASSWORD" \
    -v DEN_PROJECTS_APP_PASSWORD="$DEN_PROJECTS_APP_PASSWORD" \
    -v DEN_TASKS_APP_PASSWORD="$DEN_TASKS_APP_PASSWORD" \
    -v DEN_MESSAGES_APP_PASSWORD="$DEN_MESSAGES_APP_PASSWORD" \
    -v DEN_REVIEW_APP_PASSWORD="$DEN_REVIEW_APP_PASSWORD" \
    -v DEN_DOCUMENTS_APP_PASSWORD="$DEN_DOCUMENTS_APP_PASSWORD" \
    -v DEN_KNOWLEDGE_APP_PASSWORD="$DEN_KNOWLEDGE_APP_PASSWORD" \
    -v DEN_GUIDANCE_APP_PASSWORD="$DEN_GUIDANCE_APP_PASSWORD" \
    -f deployment/postgresql-app-roles.psql
'
```

### 2.7 Build and run the offline migration runner

Build the migration binary from the same commit that will be deployed:

```sh
cd /data/services/den-services
install -d -m 0755 bin
go build -trimpath -o bin/den-services-migrate ./migration/cmd/migrate
```

Apply every embedded migration:

```sh
sudo bash -c '
  set -euo pipefail
  set -a
  . /etc/den-services/postgresql.env
  set +a
  /data/services/den-services/bin/den-services-migrate status
  /data/services/den-services/bin/den-services-migrate up
  /data/services/den-services/bin/den-services-migrate status
'
```

The final status must report `pending=0` for every schema. The current runner
also creates the temporary `den_core` schema from its versioned migrations; on
a clean instance those tables remain empty because no import command is run.

Verify the database and listener:

```sh
pg_isready -h 127.0.0.1 -p 5433 -d denservices
sudo ss -ltnp | grep ':5433'
sudo systemctl --no-pager --full status \
  postgresql.service
```

Use `postgresql@<major>-denservices.service` for the Debian-family cluster in
section 2.3, substituting the detected major (for example,
`postgresql@18-denservices.service` on Ubuntu 26.04).

The listener check should show `127.0.0.1:5433`, not `0.0.0.0:5433` or a LAN
address.

## 3. Install the shared systemd service template

The deployment uses a shared unit for every registered Go service. Start with
this Fedora form; replace `lore` with the service-root owner. On a
Debian-family host, replace `postgresql.service` in both dependency lines with
the detected cluster unit, such as `postgresql@18-denservices.service`.

```ini
[Unit]
Description=Den Go service %i
After=network-online.target postgresql.service
Wants=network-online.target postgresql.service

[Service]
Type=simple
User=lore
Group=lore
WorkingDirectory=/data/services/%i
EnvironmentFile=-/etc/den-services/%i.env
Environment=SERVICE_NAME=%i
Environment=SERVICE_ROOT=/data/services/%i
ExecStart=/data/services/%i/bin/%i
Restart=on-failure
RestartSec=2s
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/data/services/%i

[Install]
WantedBy=multi-user.target
```

Install it:

```sh
sudoedit /etc/systemd/system/den-go@.service
sudo systemctl daemon-reload
sudo systemctl cat den-go@.service
```

Do not pass the bare template to `systemd-analyze verify` before a service is
installed. On Fedora 43 it synthesizes `den-go@test_instance.service` and fails
because `/data/services/test_instance/bin/test_instance` correctly does not
exist. The deployment script's real service start and health checks provide the
useful validation.

`ProtectHome=true` means optional services must not depend on files under a home
directory. Put Doc Publish repositories and any service-owned credentials under
the service root or add a deliberately reviewed systemd exception.

## 4. Prepare service roots and configuration

### 4.1 Create only the selected service roots

Keep the machine tidy by creating roots only for the proven core deployment.
Add an optional service to this array when its configuration is ready:

```sh
cd /data/services/den-services
DEN_DEPLOY_SERVICES=(
  projects knowledge artifacts runtime conversation
  tasks documents delivery messages guidance observation timeline
  review librarian mcp
)
for service in "${DEN_DEPLOY_SERVICES[@]}"; do
  sudo install -d -m 0755 \
    -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" \
    "/data/services/$service"
  sudo install -d -m 0755 \
    -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" \
    "/data/services/$service/config"
done

# Artifacts writes content-addressed blobs below its service root. Create each
# path component explicitly so an intermediate directory is not left root-owned
# by a recursive sudo install.
sudo install -d -m 0755 \
  -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" \
  /data/services/artifacts/data
sudo install -d -m 0755 \
  -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" \
  /data/services/artifacts/data/blobs
```

The deployment script refuses to create these top-level roots itself.

### 4.2 Stage and review YAML configuration

For each service, copy its registered `config_example` to
`/data/services/<service>/config/config.yaml` before the first deployment. The
deploy script only copies the example when the destination is absent, so a
reviewed file will be preserved.

Seed those files for the selected services:

```sh
for service in "${DEN_DEPLOY_SERVICES[@]}"; do
  install -m 0644 "$service/config/config.example.yaml" \
    "/data/services/$service/config/config.yaml"
done
```

At minimum, make these same-machine changes:

- All internal base URLs use `http://127.0.0.1:<registered-port>`.
- Observation uses `chat_source.mode: "postgres_view"`; do not leave the clean
  deployment pointed at legacy HTTP on port 18081.
- Artifacts uses a writable storage root such as
  `/data/services/artifacts/data/blobs`, not `/var/lib/den/artifacts`, because
  the shared systemd unit only grants writes under its service root.
- Gateway gets a reviewed successor-only `config/routes.yaml`; do not use the
  legacy catch-all default on a machine with no legacy den-channels.
- MCP gets a route table and backend list that match the services actually
  installed. Any operation still assigned to `den-core` will fail unless a
  den-core backend is deliberately provided.
- In the local-trust profile, MCP uses `listen_addr: "127.0.0.1:5199"` and
  `security.allow_unauthenticated_local_dev: true`.
- In the trusted agentbox LAN profile, MCP instead uses
  `listen_addr: "0.0.0.0:5199"`. The same unauthenticated setting then applies
  to LAN callers as well as local callers; this is an explicit mutation-capable
  trusted-network choice, not an authentication boundary.
- In the local-trust core deployment, set `review.github.enabled: false` and
  omit Gateway, Doc Publish, Visual Inspect, and Visual Contract until those
  capabilities are actually needed.
- When Visual Contract is enabled, keep it bound to loopback but set
  `artifacts.public_base_path: "/api/v1/visual-contracts"`. Emitted artifact
  refs are browser-visible same-origin paths served through web-edge and
  Gateway; never configure this field with the private `127.0.0.1:8086`
  service-owner URL.

### 4.3 Create service environment files

For the local-trust profile, use passwordless loopback database URLs:

```text
postgres://den_<domain>_app@127.0.0.1:5433/denservices?sslmode=disable
```

Set `DEN_MCP_SERVICE_TOKEN` empty and set required service/backend token values
to `local-den`. MCP accepts the user's local call without a token and attaches
the compatibility value when it calls current protected REST backends. The env
files contain no secrets in this profile and may use mode `0644`.

For the hardened profile, generate one distinct bearer token per protected
service and store the token in the service's env file plus the env files of
authorized callers. Use `openssl rand -hex 32` for URL- and shell-safe values.

Every hardened-profile database URL on this machine has this form:

```text
postgres://den_<domain>_app:<matching-app-password>@127.0.0.1:5433/denservices?sslmode=disable
```

Create `/etc/den-services/<service>.env` as `root:root` mode `0600`. Use the
checked-in env example as a variable inventory, then normalize its config path
and database URL using this table:

Seed the files for the selected services before editing them:

```sh
for service in "${DEN_DEPLOY_SERVICES[@]}"; do
  sudo install -m 0600 -o root -g root \
    "$service/config/$service.env.example" \
    "/etc/den-services/$service.env"
done
```

| Service | Canonical config-path variable | Database role | Local dependencies |
|---|---|---|---|
| web-edge | `WEB_EDGE_CONFIG_PATH` | none | Den Web asset release and Gateway |
| gateway | `GATEWAY_CONFIG_PATH` | none | configured route targets |
| runtime | `RUNTIME_CONFIG_PATH` | `den_runtime_app` | none |
| delivery | `DELIVERY_CONFIG_PATH` | `den_delivery_app` | runtime |
| observation | `OBSERVATION_CONFIG_PATH` | `den_observation_app` | cross-schema views |
| conversation | `CONVERSATION_CONFIG_PATH` | `den_channels_app` | runtime only if wake-target lookup is enabled |
| timeline | `TIMELINE_CONFIG_PATH` | `den_timeline_app` | cross-schema views |
| visual-contract | `VISUAL_CONTRACT_CONFIG_PATH` | none | filesystem |
| doc-publish | `DOC_PUBLISH_CONFIG_PATH` | none | documents and an external Git repository |
| visual-inspect | `VISUAL_INSPECT_CONFIG_PATH` | none | artifacts and a model endpoint |
| artifacts | `ARTIFACTS_CONFIG_PATH` | `den_artifacts_app` | writable blob storage |
| projects | `PROJECTS_CONFIG_PATH` | `den_projects_app` | none |
| tasks | `TASKS_CONFIG_PATH` | `den_tasks_app` | projects |
| messages | `MESSAGES_CONFIG_PATH` | `den_messages_app` | projects and tasks |
| documents | `DOCUMENTS_CONFIG_PATH` | `den_documents_app` | projects; guidance callback optional |
| knowledge | `KNOWLEDGE_CONFIG_PATH` | `den_knowledge_app` | none |
| review | `REVIEW_CONFIG_PATH` | `den_review_app` | projects, tasks, messages, and optionally GitHub |
| guidance | `GUIDANCE_CONFIG_PATH` | `den_guidance_app` | projects and documents |
| librarian | `LIBRARIAN_CONFIG_PATH` | none | projects, tasks, messages, documents, knowledge |
| mcp | `MCP_CONFIG_PATH` | none | configured MCP backends |

For example, `/etc/den-services/projects.env` should resemble:

```text
PROJECTS_CONFIG_PATH=/data/services/projects/config/config.yaml
DEN_PROJECTS_BASE_URL=http://127.0.0.1:8091
DEN_PROJECTS_DATABASE_URL=postgres://den_projects_app:<projects-password>@127.0.0.1:5433/denservices?sslmode=disable
DEN_PROJECTS_SERVICE_TOKEN=<projects-token>
```

The same service token must be used consistently by callers. For example,
`DEN_PROJECTS_SERVICE_TOKEN` in the Tasks, Messages, Documents, Review,
Guidance, Librarian, and MCP env files must match the Projects service token.
The MCP env file similarly needs `DEN_MCP_SERVICE_TOKEN` plus the matching
service token for every enabled backend in the hardened profile. Gateway needs
separate caller tokens and the matching upstream service tokens for every
enabled route.

Check that placeholders and old database endpoints are gone:

```sh
sudo grep -R -n -E \
  'change-me|replace-with|<[^>]+>|127\.0\.0\.1:5432/den([?[:space:]]|$)' \
  /etc/den-services/*.env
```

The command should produce no output. Empty optional values, such as an LLM API
key for a trusted local endpoint, should be reviewed rather than blindly
replaced.

## 5. Verify the checkout before deployment

Test every main module in read-only module mode, including modules not listed
in the older Makefile service loop:

```sh
cd /data/services/den-services
while read -r module_dir; do
  test -n "$module_dir" || continue
  (cd "$module_dir" && go test -mod=readonly ./...)
done < <(go list -m -f '{{if .Main}}{{.Dir}}{{end}}' all)
git diff --check
git diff --exit-code
git status --short
```

The final status must remain clean. Do not run `go work sync` as a deployment
step: on the proven Go 1.26.4 host it rewrote many committed module files and
created two new sums. `-mod=readonly` proves the checked-in dependency state is
sufficient without mutating the deployment checkout.

## 6. Deploy services in dependency order

The standard command builds a registered binary with version metadata, installs
it under `/data/services/<service>`, restarts its systemd unit, and verifies
`/health` and `/version`:

```sh
scripts/den-services-deploy.sh <service> --no-pull
```

The script uses `sudo -n` for systemd. Refresh the operator's sudo credential
before each deployment batch:

```sh
sudo -v
```

Use these batches. Stop at the first failed health or version smoke and update
the guide with the diagnosis.

1. Independent database/service foundations:

   ```sh
   for service in projects knowledge artifacts runtime conversation; do
     scripts/den-services-deploy.sh "$service" --no-pull
   done
   ```

2. Services with one foundation dependency:

   ```sh
   for service in tasks documents delivery; do
     scripts/den-services-deploy.sh "$service" --no-pull
   done
   ```

3. Composite database-backed services:

   ```sh
   for service in messages guidance observation timeline; do
     scripts/den-services-deploy.sh "$service" --no-pull
   done
   ```

4. Higher-level composition:

   ```sh
   for service in review librarian; do
     scripts/den-services-deploy.sh "$service" --no-pull
   done
   ```

5. Optional services, only after their configuration is real:

   ```sh
   for service in visual-contract visual-inspect doc-publish; do
     scripts/den-services-deploy.sh "$service" --no-pull
   done
   ```

6. Deploy the MCP front door after its standalone route configuration is
   reviewed:

   ```sh
   scripts/den-services-deploy.sh mcp --no-pull
   ```

   Keep MCP on `127.0.0.1:5199` for the local-agent profile, or deliberately
   bind `0.0.0.0:5199` for the trusted agentbox LAN profile described above. A
   headless agent machine may omit Gateway. When Den Web is included, defer
   Gateway deployment to section 7, where its successor-only route table is
   installed first, and keep it on `127.0.0.1:8079`. Legacy-only MCP operations
   continue to report the absent `den-core` backend, while successor-backed
   operations work.

   If a legacy Den process already owns `5199`, first prove den-services MCP on
   an alternate loopback canary port. Then move the legacy process to a
   documented loopback-only rollback port such as `127.0.0.1:5200` before
   starting `den-go@mcp.service` on canonical `5199`. Do not leave two LAN MCP
   endpoints that both appear canonical. Existing clients already configured
   for `http://localhost:5199/mcp` or the machine's LAN address then cut over
   without configuration churn.

After each first successful deployment, enable the unit for boot:

```sh
sudo systemctl enable den-go@<service>.service
```

The deploy script restarts a service but does not currently enable it.

## 7. Deploy Den Web as the LAN front door

Den Web lives in a separate repository, but the single-machine deployment
includes it because it is the browser UI for these services. The Go `web-edge`
service serves its assets and same-origin API on `0.0.0.0:18080`. Every
`/api/v1/*` request goes through the successor-only Gateway on
`127.0.0.1:8079`; browser code never selects or addresses an owner service.

The browser does not send a Den access token. The edge replaces any inbound
Authorization header with a dedicated Gateway web caller token, and Gateway
replaces that with the owning service token.

### 7.1 Configure and deploy the loopback Gateway

Create the Gateway root:

```sh
sudo install -d -m 0755 \
  -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" \
  /data/services/gateway /data/services/gateway/config
```

Create `/data/services/gateway/config/config.yaml`:

```yaml
bind_addr: "127.0.0.1:8079"
routing_config_path: "config/routes.yaml"

http:
  read_header_timeout: "5s"
```

Install the complete checked-in route table. Do not maintain a second inline
copy in this guide:

```sh
install -m 0644 gateway/config/routes.example.yaml \
  /data/services/gateway/config/routes.yaml
```

The route table covers all browser owners and the narrower runtime/lane
callers. The Gateway schema currently requires a legacy URL even for
`successor_mode: "always"`; the deployable table points both URL fields at the
same successor owner while ensuring the authenticated successor branch is used.

Create `/etc/den-services/gateway.env` with these local-trust values:

```sh
sudo install -m 0644 -o root -g root /dev/null \
  /etc/den-services/gateway.env
sudoedit /etc/den-services/gateway.env
```

```text
GATEWAY_CONFIG_PATH=/data/services/gateway/config/config.yaml
DEN_GATEWAY_SERVICE_TOKEN=local-den
DEN_GATEWAY_WEB_TOKEN=local-den-web
DEN_GATEWAY_PROJECTS_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_TASKS_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_MESSAGES_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_DOCUMENTS_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_GUIDANCE_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_REVIEW_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_ARTIFACTS_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_VISUAL_CONTRACT_UPSTREAM_TOKEN=<matching-DEN_VISUAL_CONTRACT_SERVICE_TOKEN>
DEN_GATEWAY_LIBRARIAN_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_DELIVERY_WRITE_TOKEN=local-den
DEN_GATEWAY_DELIVERY_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_OBSERVATION_READ_TOKEN=local-den
DEN_GATEWAY_OBSERVATION_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_CONVERSATION_READ_TOKEN=local-den
DEN_GATEWAY_CONVERSATION_WRITE_TOKEN=local-den
DEN_GATEWAY_CONVERSATION_UPSTREAM_TOKEN=local-den
DEN_GATEWAY_TIMELINE_READ_TOKEN=local-den
DEN_GATEWAY_TIMELINE_UPSTREAM_TOKEN=local-den
```

The visual-contract route is deliberately path-translated by Gateway:
browser `/api/v1/visual-contracts/*` becomes Gateway
`/v1/visual-contracts/*`, then visual-contract `/visual-contracts/*`.
The service emits artifact refs under the original browser prefix configured as
`artifacts.public_base_path: "/api/v1/visual-contracts"`; Gateway must not
rewrite response bodies.
Use the exact same secret for `DEN_GATEWAY_VISUAL_CONTRACT_UPSTREAM_TOKEN`
and the visual-contract service's `DEN_VISUAL_CONTRACT_SERVICE_TOKEN`.
Neither value belongs in `den-web-config.json` or any browser request.

Install and test Gateway:

```sh
cd /data/services/den-services
scripts/den-services-deploy.sh gateway --no-pull
sudo systemctl enable den-go@gateway.service
curl -fsS http://127.0.0.1:8079/health
curl -fsS \
  -H 'Authorization: Bearer local-den' \
  'http://127.0.0.1:8079/v1/observation/lane?limit=1'
```

### 7.2 Clone and prepare Den Web

Keep the source checkout separate from the release tree:

```sh
sudo install -d -m 0755 \
  -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" /data/dev
git clone https://github.com/FuzzySlipper/den-web.git /data/dev/den-web

sudo install -d -m 0755 \
  -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" \
  /data/services/den-web /data/services/den-web/releases
```

Install the pinned frontend dependencies and Playwright Chromium before the
first deployment. The deploy command runs the full browser suite, so a clean
machine must have both the browser binary and its OS libraries:

```sh
cd /data/dev/den-web
npm ci
npx playwright install --with-deps chromium
```

Stage the first asset release without a live smoke. This breaks the initial
asset/edge startup dependency; normal later deploys must leave smoke enabled:

```sh
cd /data/dev/den-web
env NX_DAEMON=false NX_TUI=false DEPLOY_SMOKE=0 ENVIRONMENT_NAME="$(hostname -s)" \
  npm run deploy:den-srv
```

The release contains only assets and public same-origin runtime config. It
contains no backend targets or tokens.

### 7.3 Deploy the Go web edge

Create the edge env file from its checked-in example. Use the same dedicated
caller token in both the edge and Gateway env files:

```sh
sudo install -m 0600 -o root -g root \
  web-edge/config/web-edge.env.example /etc/den-services/web-edge.env
sudoedit /etc/den-services/web-edge.env
```

```text
WEB_EDGE_CONFIG_PATH=/data/services/web-edge/config/config.yaml
DEN_WEB_EDGE_GATEWAY_TOKEN=local-den-web
```

Create the service root and deploy through the normal registry:

```sh
sudo install -d -m 0755 -o "$DEN_SERVICE_USER" -g "$DEN_SERVICE_GROUP" \
  /data/services/web-edge
cd /data/services/den-services
scripts/den-services-deploy.sh web-edge --no-pull
sudo systemctl enable den-go@web-edge.service
```

### 7.4 Build, deploy, and expose Den Web

The normal path runs the complete frontend gates, creates an atomic asset
release, flips the stable symlink without restarting the edge, and runs the
static/API smoke:

```sh
cd /data/dev/den-web
env \
  NX_DAEMON=false \
  NX_TUI=false \
  DEN_WEB_URL='http://127.0.0.1:18080' \
  ENVIRONMENT_NAME="$(hostname -s)" \
  npm run deploy:den-srv
```

If Node 22.22.2 produces the Nx native-loader error recorded in the caveats,
upgrade to a newer patch release from the same maintained channel first. If the
operating system cannot yet provide one, the deploy stops before activating a
release. Preserve the other gates and use the deploy script only for staging,
restart, and smoke:

```sh
cd /data/dev/den-web
npm ci
npm run check:pattern
npm run check:docs
npm run check:connectivity
npm run lint
npm run typecheck
npm test
npm run build

env \
  NX_DAEMON=false \
  NX_TUI=false \
  SKIP_INSTALL=1 \
  SKIP_CHECKS=1 \
  DEN_WEB_URL='http://127.0.0.1:18080' \
  ENVIRONMENT_NAME="$(hostname -s)" \
  npm run deploy:den-srv
```

This fallback is acceptable only when `npm run e2e` passes for the same commit
on another build host and the live-browser check below passes against the new
machine. Do not use it to ignore an application test failure.

Allow only port 18080 on the trusted LAN. On Fedora with firewalld, inspect the
active zone first:

```sh
sudo firewall-cmd --get-active-zones
sudo firewall-cmd --list-ports
```

Lore's `FedoraWorkstation` zone already allowed `1025-65535/tcp`, so no firewall
change was needed. On a host that does not already allow 18080, add only that
port to the active trusted-LAN zone and reload firewalld:

```sh
DEN_FIREWALL_ZONE="replace-with-active-trusted-lan-zone"
sudo firewall-cmd --permanent \
  --zone="$DEN_FIREWALL_ZONE" --add-port=18080/tcp
sudo firewall-cmd --reload
```

Do not open 8079, 5433, or any owner-service port. Keep 5199 closed for the
local-agent profile. For the trusted agentbox LAN profile, allow 5199 only on
the intended trusted-LAN interface or zone and record that exception in the
deployment evidence.

### 7.5 Validate Den Web from the machine and a LAN browser

Identify the target unambiguously before collecting receipts. Private LAN
addresses commonly repeat across sites, so an IP alone can accidentally prove
another deployment:

```sh
hostname
cat /etc/machine-id
cat /proc/sys/kernel/random/boot_id
hostname -I
```

Record the SSH host/profile or other access route alongside these values.

On the new machine, run the checked-in smoke with the deployed commit:

```sh
cd /data/dev/den-web
DEN_WEB_COMMIT="$(git rev-parse HEAD)"
env \
  DEN_WEB_URL='http://127.0.0.1:18080' \
  EXPECTED_BUILD_COMMIT="$DEN_WEB_COMMIT" \
  EXPECTED_ENV_NAME="$(hostname -s)" \
  node tools/scripts/smoke-den-web.mjs
```

Confirm the listener split and the LAN URL:

```sh
ss -ltn | grep -E ':(18080|8079|5433)[[:space:]]'
curl -fsS "http://$(hostname -I | awk '{print $1}'):18080/"
curl -fsS "http://$(hostname -I | awk '{print $1}'):18080/den-web-build.json"
curl -fsS "http://$(hostname -I | awk '{print $1}'):18080/api/v1/projects"
```

The listener check must show Den Web on `0.0.0.0:18080`, Gateway on
`127.0.0.1:8079`, and PostgreSQL on `127.0.0.1:5433`.

From a Den Web checkout on a machine where its Playwright suite passes, run the
rendered live scenarios against the new instance and inspect the screenshots:

```sh
DEN_WEB_LAN_URL=http://replace-with-new-machine-ip:18080
LIVE_RUN=1 BASE_URL="$DEN_WEB_LAN_URL" \
  npx playwright test \
  --config=apps/den-web-e2e/playwright.config.mts \
  src/live/boot.live.spec.ts \
  src/live/inherited-features.live.spec.ts \
  --workers=2
```

Finally, restart Gateway and the edge, wait for the build sentinel rather than
assuming `systemctl is-active` means the socket is ready, and repeat the smoke:

```sh
sudo systemctl restart den-go@gateway.service den-go@web-edge.service
for attempt in $(seq 1 40); do
  curl -fsS http://127.0.0.1:18080/den-web-build.json && break
  sleep 0.25
done
```

## 8. Validate the complete instance

### 8.1 systemd and HTTP contract

```sh
systemctl --failed --no-pager
systemctl list-units 'den-go@*.service' --no-pager
```

Smoke every registered health and version endpoint that was intentionally
deployed. The unfiltered commands below should pass when all registered
services are installed; otherwise filter the URL list to the recorded omission
set:

```sh
awk -F'"' '/health_url:/ { print $2 }' deployment/services.yaml |
while read -r url; do
  printf '%s ' "$url"
  curl -fsS "$url"
  printf '\n'
done

awk -F'"' '/version_url:/ { print $2 }' deployment/services.yaml |
while read -r url; do
  printf '%s ' "$url"
  curl -fsS "$url"
  printf '\n'
done
```

Each `/version` response must report the recorded deployment commit.

### 8.2 PostgreSQL contract

Re-run migration status:

```sh
sudo bash -c '
  set -a
  . /etc/den-services/postgresql.env
  set +a
  /data/services/den-services/bin/den-services-migrate status
'
```

Confirm that PostgreSQL is not LAN-exposed:

```sh
sudo ss -ltnp | grep ':5433'
```

### 8.3 MCP contract

Initialize MCP without an `Authorization` header in the local-trust or trusted
agentbox LAN profile:

```sh
curl -fsS -X POST http://127.0.0.1:5199/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"deployment-smoke","version":"1.0"}}}'
```

Then prove an unauthenticated local call crosses MCP's internal authenticated
backend boundary:

```sh
curl -fsS -X POST http://127.0.0.1:5199/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}'
```

On a clean instance, the result should contain `"isError":false` and an empty
project list. For the trusted agentbox LAN profile, repeat both calls through
the machine's LAN address from a second host and confirm `tools/list` exposes
the same registry as loopback. For the hardened profile, include the configured
bearer token instead of performing token-free calls.

### 8.4 Reboot persistence

After all intended services are enabled, perform one controlled reboot and
repeat the systemd, health, version, PostgreSQL, MCP, and Den Web checks. When
Den Web is installed, repeat at least its LAN browser boot scenario too. A
deployment is not proven until the database and services recover without an
operator shell.

## No data import

For a new empty instance, run only the versioned migration runner. Do not run:

- `migration/cmd/den-core-import-parity`;
- `migration/cmd/lifeboat-backfill`;
- `conversation/cmd/import-legacy`;
- `scripts/conversation-pilot-canary-2917.sh` against a legacy source;
- any ad hoc `pg_dump` restore from another Den instance.

Schema migrations may create bookkeeping rows or schema placeholders. That is
not a legacy data migration.

## First-deployment evidence

### 2026-07-16 — Lore clean Fedora deployment

- Machine: `lore`, Fedora Linux 43 KDE, x86_64.
- Deployment commit: `fe744194bd89528c610e16c954696af600831a0f` from
  public HTTPS clone at `/data/services/den-services`.
- Toolchain: checksum-verified upstream Go 1.26.4. Fedora's available Go 1.25
  package did not satisfy the workspace's `go 1.26` declarations.
- PostgreSQL: Fedora 18.3 packages, `/var/lib/pgsql/data`,
  `postgresql.service`, `127.0.0.1:5433`, local-trust HBA rules.
- Host posture: SELinux permissive; firewalld unchanged; no Den port opened;
  all Den and PostgreSQL listeners verified on `127.0.0.1`.
- Deployed and enabled: Projects, Knowledge, Artifacts, Runtime, Conversation,
  Tasks, Documents, Delivery, Messages, Guidance, Observation, Timeline,
  Review with GitHub disabled, Librarian, and MCP.
- Intentionally omitted: Gateway, Doc Publish, Visual Inspect, Visual Contract,
  and other broker/development binaries. MCP retained its den-core route entries
  for discovery compatibility, so those legacy-only operations fail until they
  receive successor routes; successor-backed operations work.
- Migration result: 14 schemas reported `pending=0`; no legacy import or
  backfill ran.
- Runtime result: all 15 deployed `/health` and `/version` endpoints reported
  healthy at commit `fe744194bd89`. Token-free MCP initialization and
  `list_projects` succeeded, with the latter returning the expected empty list.
  MCP exposed 69 tools at this commit.
- Verification: every main Go module passed `go test -mod=readonly ./...`; the
  checkout remained clean. A controlled PostgreSQL plus all-service restart
  recovered all health endpoints and the MCP backend call. A full OS reboot
  changed the boot ID to `c79f5ea7-ee6c-4e8a-983b-8005589f4400`; PostgreSQL
  and all 15 units returned active and enabled, every migration remained at
  `pending=0`, and the token-free MCP backend call passed again.
- Package-manager note: unrelated stale PlasmaZones and Zoom repositories
  emitted DNF errors, but official Fedora package transactions completed. They
  were deliberately left outside this deployment's scope.

Corrections found during the test:

1. `postgresql-contrib` was required for `pg_trgm`; adding the package allowed
   migration to continue after `extension "pg_trgm" is not available`.
2. `.gitignore`'s broad `migrate` rule hid `migration/cmd/migrate/main.go` from
   clean clones. The rule now explicitly includes the runner so the documented
   build command works in future clones.
3. `den_guidance/001_initial.sql` compared existing timestamp columns with an
   untyped empty string, which PostgreSQL 18 rejected. Casting the legacy value
   to text before `nullif` made the migration work for both timestamp and
   legacy-text inputs; a regression test covers the SQL contract.
4. `go work sync` rewrote committed module files under Go 1.26.4. The guide now
   uses `go test -mod=readonly` and an explicit clean-diff check.

### 2026-07-16 — Lore Den Web extension

- Den Web commit: `92fe1111009324cde250f796e08f137373218dcc` from a
  clean public HTTPS clone at `/data/dev/den-web`.
- Frontend build toolchain: Fedora Node 22.22.2 and npm 10.9.7. The active atomic release was
  `20260717T045540Z-92fe11110093` under `/data/services/den-web`.
- Gateway: deployed and enabled at den-services commit `fe744194bd89`, with
  only the reviewed Conversation, Observation, Delivery, and Timeline successor
  route families. It remained loopback-only on `127.0.0.1:8079`.
- Network posture: Den Web was the only LAN listener at `0.0.0.0:18080`.
  PostgreSQL, Gateway, MCP, and owner services remained on loopback. Lore's
  existing `FedoraWorkstation` firewalld zone already allowed high TCP ports, so
  no firewall rule was added. Browser requests required no access token.
- Deployment gates: pattern, documentation, connectivity, lint, typecheck, all
  54 unit tests, and the production build passed on Lore. The same commit's
  full Playwright suite passed on the build workstation with 13 passing and 11
  intentionally skipped tests.
- Runtime proof: the checked-in Den Web smoke passed all 29 assertions against
  the empty instance, including Projects, Tasks, Notifications, Conversation,
  Observation, and Timeline through the same-origin proxy. Nine live Chromium
  scenarios passed against `http://192.168.1.25:18080`, and the boot,
  Conversation, and Agents screenshots were inspected. Empty-state content
  rendered without a degraded-service error.
- Restart proof: a controlled Gateway plus Den Web restart recovered both
  units and all 29 smoke assertions after waiting for the build sentinel.
- Reboot proof: the post-extension reboot changed the boot ID to
  `509fd433-7c1a-4267-b3da-e94ce56a72a6`. PostgreSQL, Den Web, and all 16 Go
  services were active and enabled; all 16 health/version pairs reported
  commit `fe744194bd89`; every schema remained at `pending=0`; the 29-assertion
  Den Web smoke and token-free MCP `list_projects` call passed; and all nine
  Chromium live scenarios passed again over the LAN URL, with the post-reboot
  Conversation screenshot inspected.

Corrections found during the Den Web extension:

1. Bare `systemd-analyze verify` of `den-go@.service` synthesizes a nonexistent
   `test_instance` and exits nonzero. The guide now reloads and inspects the
   template, while concrete deployments prove it through real unit and health
   checks.
2. Fedora's Node 22.22.2 hit an Nx native-loader error only when Playwright
   loaded the Nx ESM configuration. No release had been activated when the
   normal deploy stopped. All other gates were rerun explicitly, the atomic
   deploy used `SKIP_CHECKS=1`, and browser coverage was supplied by the same
   commit on the build workstation plus live tests against Lore.
3. `systemctl is-active` returned before Node had rebound port 18080 during a
   controlled restart. The guide now waits for `/den-web-build.json` before
   running the post-restart smoke.

Use this template for the next clean-machine deployment and record facts, not
only "worked" or "failed":

```text
Date:
Machine/OS:
CPU architecture:
Deployment commit:
Go version:
PostgreSQL package version:
PostgreSQL cluster and port:
Guide steps changed:
Services intentionally omitted:
Migration status result:
Health/version result:
Reboot result:
Known follow-ups:
```

For every correction, include the failing command or symptom, the cause, the
guide change, and the command that then passed. Never paste credentials into
this repository.

## Related references

- [`postgresql.md`](./postgresql.md): first-host PostgreSQL operational notes.
- [`postgresql-app-roles.psql`](./postgresql-app-roles.psql): runtime role
  bootstrap.
- [`service-contract.md`](./service-contract.md): deployable service contract.
- [`services.yaml`](./services.yaml): deployable service registry and ports.
- [`../docs/lifeboat-service-substrate.md`](../docs/lifeboat-service-substrate.md):
  module, configuration, and systemd conventions.
- [`../scripts/den-services-deploy.sh`](../scripts/den-services-deploy.sh):
  standard build/install/restart/smoke path.
- [Den Web standalone deploy guide](https://github.com/FuzzySlipper/den-web/blob/main/docs/den-web-standalone-deploy.md):
  Den Web's atomic asset-release contract and the den-services Go web edge.
