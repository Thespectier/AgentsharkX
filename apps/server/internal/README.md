# Server package map

Phase 13 package responsibilities:

- `api`: OpenAPI-owned handlers, request IDs, structured access logs, standard
  errors, authentication enforcement, persistent Audit APIs, `/healthz`
  liveness, `/readyz` persistence initialization, and outbox-backed resumable
  SSE.
- `auth`: bounded single-admin session, strict cookie, and CSRF validation.
- `config`: environment parsing, deployment safety checks, database/retention
  validation, and redacted summaries.
- `gateway` and `guard`: independent management clients over verified upstream
  routes, with non-retried AgentGuard writes and operation deadlines.
- `upstream`: bounded retry transport and secret-safe adapter errors.
- `aggregate`: source-scoped health, capability, partial-result, and operational
  overview models.
- `connect`: source-preserving agentgateway summaries, filtering, cursor
  pagination, details, Setup verification, and validated console links.
- `trust`: explicit AgentGuard identity/resource aggregation, filtering,
  pagination, label writes, and bounded in-memory detection jobs.
- `protect`: gateway/AgentGuard policy views and guarded mutations; confirmed
  approval outcomes are persisted through Audit before being reported complete.
- `audit`: independent upstream polling, normalized metrics/events/sessions,
  exact-ID correlation, authenticated complete upstream detail, source
  checkpoints, and persistence orchestration.
- `storage`: small interfaces plus the PostgreSQL production store, append-only
  embedded migrations, stable event cursors, payload retention, outbox replay,
  pruning, and an explicit memory adapter for tests/Mock only.
- `stream`: coalesced post-commit notifications. It owns no event IDs or replay
  buffer; `stream_outbox.sequence` is the sole SSE cursor.
- `model`: the shared source-preserving response model.

PostgreSQL is required in production and there is no automatic memory fallback.
Normalized Audit metadata defaults to 30-day retention, outbox delivery rows to
24 hours, and payload storage to disabled. A positive payload retention opts
complete AgentGuard/approval records into a separate table; list, overview, and
SSE responses remain summary-only. Scan jobs and rule-check tokens are still
ephemeral. SSE replay is delivery recovery, not agent business-traffic replay.

The persisted checkpoint is an observed watermark. The verified upstream Audit
reads do not provide complete request-side historical cursors, so it cannot
guarantee recovery after data has aged out of an upstream's bounded window.
