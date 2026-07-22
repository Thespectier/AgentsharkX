# Server package map

Phase 6 package responsibilities:

- `api`: OpenAPI-owned handlers, request IDs, structured access logs, standard
  errors, authentication enforcement, Audit APIs, and resumable SSE.
- `auth`: bounded single-admin session, strict cookie, and CSRF validation.
- `config`: environment parsing, deployment safety checks, and redacted
  summaries.
- `gateway` and `guard`: independent management clients over verified upstream
  routes, with non-retried AgentGuard writes and operation deadlines.
- `upstream`: bounded retry transport and secret-safe adapter errors.
- `aggregate`: source-scoped health, capability, partial-result, and operational
  overview models.
- `connect`: source-preserving agentgateway summaries, filtering, cursor
  pagination, details, Setup verification, and validated console links.
- `trust`: explicit AgentGuard identity/resource aggregation, filtering,
  pagination, label writes, and bounded in-memory detection jobs.
- `protect`: gateway/AgentGuard policy views and guarded mutations.
- `audit`: independent upstream polling, normalized metrics/events/sessions,
  exact-ID correlation, redacted detail, and bounded snapshots.
- `stream`: bounded event ring, dedupe, monotonic sequences, replay, and fan-out.
- `model`: the shared source-preserving response model.

Phase 6 intentionally has no database, durable event buffer, task model, or
payload replay. Audit polls only verified management reads; its 1000-event ring,
scan jobs, and check tokens are ephemeral and disappear on restart. SSE replay
means retained delivery after a sequence ID, not business-traffic replay.
