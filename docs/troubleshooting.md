# Troubleshooting

Start with **System**. Diagnostics are derived from independent live probes and
never reveal configured URLs, API keys, authorization headers, or raw upstream
responses.

## Upstream connectivity

### agentgateway is down

1. Confirm `AGENTGATEWAY_BASE_URL` reaches the admin listener from the
   AgentsharkX container, not an agent traffic listener.
2. Run `docker compose --env-file deploy/versions.env --env-file .env -f deploy/compose.yaml logs agentgateway`.
3. Verify <http://127.0.0.1:15021/healthz/ready> returns `ready`.
4. Load `.env` and run `make upstream-smoke`.

The pinned standalone admin API has no verified native authentication. Keep its
published port on loopback or a private management network.

### agentgateway Raw Configuration cannot save

1. Start and manage the preview with `make preview-up`, `make preview-status`,
   and `make preview-down`; these targets use the UID/GID-aware Compose wrapper.
2. Load `.env` and run `make gateway-config-write-smoke`. It reads the current
   configuration and submits it unchanged without printing the payload.
3. Confirm `deploy/agentgateway/config.yaml` is owned by the checkout user and
   remains writable by that owner. Do not solve this by making it
   world-writable.
4. Recreate the service with `make preview-up` after changing file ownership.

Docker reporting a bind mount as read-write is insufficient: the pinned image
normally runs as UID `65532`, while a checkout-owned mode-0644 file rejects
writes from that identity. The preview wrapper runs only agentgateway as the
file owner's non-root UID/GID.

The smoke scripts intentionally use the published loopback ports even after
`.env` is loaded. Override `AGENTGATEWAY_SMOKE_BASE_URL` or
`AGENTGUARD_SMOKE_BASE_URL` only when testing a different host-side endpoint;
the Compose-internal `AGENTGATEWAY_BASE_URL` and `AGENTGUARD_BASE_URL` are not
host-resolvable.

### AgentGuard is down

1. Confirm `AGENTGUARD_BASE_URL` reaches port `38080` inside Compose.
2. Confirm `AGENTGUARD_ADMIN_TOKEN` is the same value used as
   `AGENTGUARD_API_KEY` by the AgentGuard service.
3. Inspect `docker compose --env-file deploy/versions.env --env-file .env -f deploy/compose.yaml logs agentguard`.
4. Run an authenticated `GET /v1/backend/health`; an unauthenticated request is
   expected to return 401.

`make preview-status` should show
`agentsharkx/agentguard:main-4b755fb`. The image is built from immutable main
revision `4b755fb4a4a2763b7e817b3d0220fe5c22187b59`, following the upstream
source-build startup model without using a floating `latest` tag.

AgentsharkX starts even when either upstream is unavailable so System can show
source-specific recovery guidance.

## Startup is rejected

- `change-me-*`, `replace-me-*`, empty, or shorter-than-16-character
  AgentsharkX/AgentGuard tokens are rejected before the listener opens.
- Disabled authentication and non-Secure cookies are accepted only for an
  explicit `local`/`development` environment bound to a loopback listener.
- `AGENTSHARK_REDACT_PAYLOADS=false` is always rejected.

Run `make preview-bootstrap` to create a safe local `.env`; it never overwrites
an existing file.

## Login or write fails

- A 401 means the browser session is missing/expired or the administrator token
  is wrong.
- A 403 `CSRF_REQUIRED` means the authenticated session could not recover its
  write token. Reload once; `GET /api/v1/auth/session` should return 204 and an
  `X-CSRF-Token` header.
- AgentGuard writes are never automatically retried. For a timeout, inspect the
  upstream ticket/rule state before using the explicit retry control.

## No gateway traffic events

The minimal agentgateway config contains no business listeners and no request-log
database. A missing database is shown as `partial`, never as an empty-success
claim. Configure routes/provider credentials and logging in agentgateway, then
use **Connect → Setup** and the upstream smoke test.

## Container is unhealthy

`GET /healthz` is an unauthenticated process-readiness check and must return
`{"status":"ok"}`. It does not claim that either upstream is healthy. Inspect
System for upstream state and `docker inspect` for the container health log.
