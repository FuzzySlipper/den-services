# den-services PostgreSQL deployment notes

Source of operational truth: Den document
`den-network/den-services-postgresql-2026-06-18`.

The dedicated den-services PostgreSQL cluster on `den-srv` is native PostgreSQL,
not one of the rootless Docker PostgreSQL containers. Docker already owns port
`5432`; den-services uses loopback-only port `5433`.

## Cluster

- Host: `den-srv` (`192.168.1.10`)
- PostgreSQL: `17/denservices`
- systemd unit: `postgresql@17-denservices.service`
- Database: `denservices`
- Listener: `127.0.0.1:5433`
- Secret env file: `/etc/den-services/postgresql.env`

## Migration connection

The migration runner uses the migration role only:

```text
postgres://den_migration:<password>@127.0.0.1:5433/denservices
```

The actual URL/password are stored only in:

```text
/etc/den-services/postgresql.env
```

Runtime services must not use the migration role.

## Runtime role connection format

After app roles are created:

```text
postgres://den_delivery_app:<password>@127.0.0.1:5433/denservices
postgres://den_runtime_app:<password>@127.0.0.1:5433/denservices
postgres://den_observation_app:<password>@127.0.0.1:5433/denservices
postgres://den_channels_app:<password>@127.0.0.1:5433/denservices
```

Store-level integration tests should use a separate test database on the same
dedicated cluster, not the Docker listener on `5432`.

## App-role bootstrap

App-role passwords should be supplied through environment variables sourced from
`/etc/den-services/postgresql.env`. Do not write them into the repository.

Required variables for [postgresql-app-roles.psql](postgresql-app-roles.psql):

- `DEN_DELIVERY_APP_PASSWORD`
- `DEN_RUNTIME_APP_PASSWORD`
- `DEN_OBSERVATION_APP_PASSWORD`
- `DEN_CHANNELS_APP_PASSWORD`

Run on `den-srv`:

```sh
set -a
. /etc/den-services/postgresql.env
set +a

psql "$DEN_SERVICES_MIGRATION_DATABASE_URL" \
  -v DEN_DELIVERY_APP_PASSWORD="$DEN_DELIVERY_APP_PASSWORD" \
  -v DEN_RUNTIME_APP_PASSWORD="$DEN_RUNTIME_APP_PASSWORD" \
  -v DEN_OBSERVATION_APP_PASSWORD="$DEN_OBSERVATION_APP_PASSWORD" \
  -v DEN_CHANNELS_APP_PASSWORD="$DEN_CHANNELS_APP_PASSWORD" \
  -f deployment/postgresql-app-roles.psql
```

The script creates the four runtime app roles and a `denservices_test` database
for store-level tests. It does not grant cross-schema access; schema grants are
owned by the versioned migration files.

## Applying migrations

On `den-srv`, load the secret env file and run:

```sh
set -a
. /etc/den-services/postgresql.env
set +a

den-services-migrate -database-url "$DEN_SERVICES_MIGRATION_DATABASE_URL" status
den-services-migrate -database-url "$DEN_SERVICES_MIGRATION_DATABASE_URL" up
den-services-migrate -database-url "$DEN_SERVICES_MIGRATION_DATABASE_URL" status
```

The runner creates successor schemas if missing and then records migration
versions in each schema's own `schema_migrations` table.
