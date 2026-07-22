# Server package map

Phase 4 package responsibilities:

- `api`: OpenAPI-owned handlers, request IDs, structured access logs, standard
  errors, authentication enforcement, and health SSE.
- `auth`: bounded single-admin session, strict cookie, and CSRF validation.
- `config`: environment parsing, deployment safety checks, and redacted
  summaries.
- `gateway` and `guard`: independent management clients over verified upstream
  routes, with non-retried AgentGuard writes and operation deadlines.
- `upstream`: bounded retry transport and secret-safe adapter errors.
- `aggregate`: source-scoped health, capability, partial-result, and health-only
  overview models.
- `connect`: source-preserving agentgateway summaries, filtering, cursor
  pagination, details, Setup verification, and validated console links.
- `trust`: explicit AgentGuard identity/resource aggregation, filtering,
  pagination, label writes, and bounded in-memory detection jobs.
- `stream`: non-blocking in-memory health-event fan-out.
- `model`: the shared source-preserving response model.

Phase 4 intentionally has no database, business traffic polling, durable event
buffer, task correlation, or replay. Analytics is a bounded live read; scan
jobs are bounded ephemeral state and disappear on restart. Those boundaries are
not placeholders for unverified data.
