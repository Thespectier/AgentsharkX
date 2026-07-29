# Database operations

Phase 13 requires PostgreSQL for production Audit storage. The database owns
normalized Audit events, optional detail payloads, per-source ingest
checkpoints, and the SSE outbox. It does not store agentgateway configuration,
provider credentials, AgentGuard configuration, or agent business traffic.

The preview uses the Compose service `postgres` and named volume
`agentsharkx_agentshark-postgres-data`. Its published port defaults to
`127.0.0.1:55432`; do not change `AGENTSHARK_DATABASE_BIND` to a public address.
Agentgateway's SQLite request-log database is separate upstream-owned state.

## Configuration

`make preview-bootstrap` creates a random PostgreSQL password and matching URL
in the ignored `.env`. It preserves existing values and adds missing Phase 13
keys. The BFF requires an absolute `postgres://` or `postgresql://` URL that
names a database.

| Variable | Default | Validation and effect |
| --- | --- | --- |
| `AGENTSHARK_DATABASE_URL` | required | PostgreSQL connection URL; treat it as a secret because it normally contains the password. |
| `AGENTSHARK_DATABASE_PASSWORD` | generated | Compose password for role `agentshark`; it must match the password in the connection URL. |
| `AGENTSHARK_DATABASE_BIND` | `127.0.0.1` | Host address used only for the preview's published PostgreSQL port. |
| `AGENTSHARK_DATABASE_PORT` | `55432` | Host port used by the Linux host-network BFF and local development. |
| `AGENTSHARK_DATABASE_MAX_CONNS` | `10` | Integer from 1 through 100. |
| `AGENTSHARK_DATABASE_MIN_CONNS` | `1` | Integer from 0 through the configured maximum. |
| `AGENTSHARK_DATABASE_CONNECT_TIMEOUT` | `5s` | Duration from 100 ms through 30 seconds. |
| `AGENTSHARK_EVENT_RETENTION` | `720h` (30 days) | Normalized event metadata retention; at least 1 hour. |
| `AGENTSHARK_PAYLOAD_RETENTION` | `0s` | `0` disables durable payloads; otherwise 1 hour through event retention. |
| `AGENTSHARK_OUTBOX_RETENTION` | `24h` | SSE delivery retention; 1 minute through event retention. |

The maintenance loop prunes expired rows at startup and hourly. Reducing a
duration takes effect at the next successful prune. Increasing payload
retention does not backfill details for events already stored as metadata-only.

## Startup and migrations

Migrations are embedded in the BFF and run under a PostgreSQL advisory lock at
startup. Migration files are append-only. The database records a SHA-256
checksum for every applied file; changing an applied migration makes readiness
fail instead of silently accepting schema drift.

Use the probes independently:

```bash
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
```

`/healthz` reports process liveness only. `/readyz` verifies database
connectivity, all embedded migrations, and successful restoration of persisted
Audit history into the running service. While storage is unavailable, Audit
REST and SSE return 503 `DATABASE_UNAVAILABLE`; production does not fall back to
the memory test store. The process retries initialization so readiness can
recover after PostgreSQL returns.

## Backup

Create a PostgreSQL custom-format backup while the service is running:

```bash
mkdir -p .cache/database-backups
./scripts/standalone-compose.sh exec -T postgres \
  pg_dump --format=custom --no-owner --no-privileges \
  --username=agentshark --dbname=agentshark \
  > .cache/database-backups/agentsharkx-$(date +%Y%m%d-%H%M%S).dump
```

The ignored backup can contain complete payload rows when payload retention is
enabled. Restrict its filesystem access, storage location, and lifetime as you
would the source management records. Verify that the output file is non-empty
before relying on it.

## Restore

Restoring replaces current AgentsharkX Audit state. Confirm the backup path and
take a fresh backup first. Stop only the BFF so no application write races the
restore, then restore in one database transaction:

```bash
./scripts/standalone-compose.sh stop agentshark
./scripts/standalone-compose.sh exec -T postgres \
  pg_restore --clean --if-exists --no-owner --no-privileges \
  --single-transaction --username=agentshark --dbname=agentshark \
  < .cache/database-backups/agentsharkx-YYYYMMDD-HHMMSS.dump
./scripts/standalone-compose.sh start agentshark
curl -fsS http://127.0.0.1:8080/readyz
```

The restored migration ledger must match the running binary. If readiness
reports a migration checksum mismatch, run the binary matching the backup and
upgrade only through new append-only migration files.

## Intentional destructive reset

This operation permanently deletes all AgentsharkX Audit events, payloads,
checkpoints, and outbox history unless a backup exists. It does not delete the
separate agentgateway SQLite log database. Normal startup and shutdown never
perform this operation.

After making and verifying a backup, stop the stack, inspect the exact volume,
then remove only that named volume:

```bash
make preview-down
docker volume inspect agentsharkx_agentshark-postgres-data
docker volume rm agentsharkx_agentshark-postgres-data
make preview-up
curl -fsS http://127.0.0.1:8080/readyz
```

Do not substitute a glob, workspace root, or unresolved environment variable
for the explicit volume name. `preview-up` creates a fresh volume and reapplies
the embedded migrations.

## Checkpoint limitation

Each source checkpoint records the last observed event ID/time plus attempt,
success, and error timestamps. The verified Phase 13 upstream contracts do not
provide complete request-side historical cursors for every feed: AgentGuard
recent Audit is bounded by `n`, and the gateway request cursor is not frozen in
the checked contract. The checkpoint is diagnostic state, not an unverified
request input. On restart the adapters repeat bounded reads and unique event
identities provide idempotency, but records that aged out of an upstream window
cannot be recovered. AgentsharkX never fills such gaps by time proximity or
synthetic identity.
