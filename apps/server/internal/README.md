# Server package map

Phase 3 package responsibilities:

- `api`: OpenAPI-owned handlers, request IDs, structured access logs, standard
  errors, authentication enforcement, and health SSE.
- `auth`: bounded single-admin session, strict cookie, and CSRF validation.
- `config`: environment parsing, deployment safety checks, and redacted
  summaries.
- `gateway` and `guard`: independent, timeout-bound management clients over
  verified upstream routes.
- `upstream`: bounded retry transport and secret-safe adapter errors.
- `aggregate`: source-scoped health, capability, partial-result, and health-only
  overview models.
- `connect`: source-preserving agentgateway summaries, filtering, cursor
  pagination, details, Setup verification, and validated console links.
- `stream`: non-blocking in-memory health-event fan-out.
- `model`: the shared source-preserving response model.

Phase 3 intentionally has no database, business traffic polling, durable event
buffer, task correlation, or replay. Analytics is a bounded live read and does
not create local storage. Those boundaries are not placeholders for unverified
data.
