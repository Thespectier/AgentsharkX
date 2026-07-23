# AgentsharkX

AgentsharkX is a lightweight management console above
[agentgateway](https://github.com/agentgateway/agentgateway) and
[AgentGuard](https://github.com/WhitzardAgent/AgentGuard). It provides one
information architecture for connection management, trusted runtime context,
protection workflows, and audit views without entering the agent data plane or
reimplementing either upstream.

The repository is at the **0.7.0 Phase 7 preview**. Connect reads explicit agentgateway
providers, models, MCP targets, and routes. Trust now reads AgentGuard sessions,
tools, skills, and MCP resources, builds Agents only from explicit AgentGuard
identity fields, and supports tool-label updates plus polled Skill/MCP detection
jobs. Protect now displays read-only agentgateway policy/guardrail summaries,
AgentGuard runtime rules and per-agent plugin phases, and supports syntax-gated
rule publication/deletion plus guarded approval decisions. Every dangerous
write requires a note, explicit confirmation, CSRF, a request ID, and a result
receipt. Audit now polls redacted agentgateway request logs/Analytics and
AgentGuard Traffic/Audit/Sessions independently, retains a bounded 1000-event
window, and streams normalized events with SSE resume and client-side dedupe.
The preview adds a reproducible non-root production image with the real Web
bundle embedded in the Go BFF, source-specific System diagnostics, a full-path
release E2E, supply-chain artifacts, and six screenshot baselines.

## Product boundary

- `Connect` and gateway-side audit data come from agentgateway.
- `Trust`, runtime protection, approvals, and security events come from
  AgentGuard.
- AgentsharkX owns authentication isolation, capability detection,
  normalization, aggregation, navigation, and high-frequency workflows.
- AgentsharkX does not infer tasks, correlate events by time proximity, proxy
  agent traffic, or add a new rules engine, database, replay system, or traffic
  collector.

See [architecture](docs/architecture.md), the
[capability matrix](docs/capability-matrix.md), and
[upstream compatibility notes](docs/upstream-compatibility.md) before changing
an adapter contract.

## Prerequisites

- GNU Make
- Docker with Compose v2
- OpenSSL and Python 3.11+ for the first-event quickstart
- Node.js 24 and npm
- Go 1.26.5 when developing the server locally (the Makefile can use the pinned
  Go container if Go is not installed)

## See the first real event in 10 minutes

```bash
make preview-bootstrap
make preview-up
```

Open <http://localhost:8080>, log in with the generated
`AGENTSHARK_ADMIN_TOKEN` from the ignored `.env`, then follow
[the 10-minute quickstart](docs/quickstart.md) to run the pinned minimal
AgentGuard client. Its real tool event appears under **Audit → Security events**
within three seconds. The example does not require an LLM or provider key.

For a complete Chinese walkthrough covering startup, Agent integration,
operations, development, release verification, and troubleshooting, see the
[中文使用指南](docs/usage-guide.zh-CN.md).

The bootstrap command refuses to overwrite `.env`, generates random
non-placeholder credentials with mode `0600`, and leaves every published port
on loopback. An unchanged `deploy/example.env` is intentionally rejected by the
BFF; there is no default password or token.

## Verify the repository

```bash
npm ci --prefix apps/web
make verify
```

This checks Go formatting/tests, the frontend format/type/unit/build suite,
repository invariants, the OpenAPI contract, and the fully rendered Compose
model.

## Review the Mock console

```bash
npm --prefix apps/web run dev
```

Open <http://127.0.0.1:5173>. The top-bar demo selector exposes the normal,
empty, loading, partial-failure, and total-failure states. Browser acceptance
requires Playwright Chromium; see [the web README](apps/web/README.md) for host
and container commands. The checked-in 1440 px and 1280 px baselines are
indexed under [docs/screenshots](docs/screenshots/README.md).

## Run the BFF locally for development

Start the pinned upstreams, then provide non-placeholder secrets and host-side
URLs. Plain HTTP cookies are permitted only with an explicit local environment
and loopback listener:

```bash
export AGENTSHARK_LISTEN_ADDR=127.0.0.1:8080
export AGENTSHARK_ENVIRONMENT=local
export AGENTSHARK_ADMIN_TOKEN='replace-with-at-least-16-characters'
export AGENTSHARK_COOKIE_SECURE=false
export AGENTGATEWAY_BASE_URL=http://127.0.0.1:15000
export AGENTGATEWAY_CONSOLE_URL=http://127.0.0.1:15000/ui
export AGENTGUARD_BASE_URL=http://127.0.0.1:38080
export AGENTGUARD_ADMIN_TOKEN='replace-with-the-agentguard-api-key'
export AGENTGUARD_VERSION=main-4b755fb
export AGENTSHARK_SCAN_TIMEOUT=90s
export AGENTSHARK_POLL_INTERVAL=2s

cd apps/server
go run ./cmd/agentshark
```

In another terminal, run the frontend against the BFF through Vite's same-origin
API proxy:

```bash
VITE_ENABLE_MOCKS=false npm --prefix apps/web run dev
```

The browser exchanges the admin token for an `HttpOnly`, `SameSite=Strict`
session cookie. The token is not persisted in browser storage. After a reload,
the authenticated session endpoint restores only the in-memory CSRF value.
Production
deployments must keep `AGENTSHARK_COOKIE_SECURE=true` and terminate HTTPS before
the BFF. Trust and Protect write requests additionally require the session CSRF
token. Rule check tokens, scan jobs, and the Audit event window are bounded in
memory and are lost when the BFF restarts. AgentGuard mutations are never
automatically retried. Request-log payloads and attributes are never requested
by the Audit poller; event detail is an allow-listed redacted projection.

## Compose and pinned upstreams

AgentGuard does not publish an upstream image. Its official `scripts/start.sh`
builds the server and console from the current checkout. Compose mirrors that
model but pins the verified main revision
`4b755fb4a4a2763b7e817b3d0220fe5c22187b59` as the local image
`agentsharkx/agentguard:main-4b755fb`; no source is vendored into this
repository and no floating tag is used.

```bash
make preview-bootstrap
make preview-up
```

Default local endpoints:

- AgentsharkX preview: <http://localhost:8080>
- agentgateway console/admin: <http://127.0.0.1:15000/ui>
- agentgateway readiness: <http://127.0.0.1:15021/healthz/ready>
- AgentGuard server: <http://127.0.0.1:38080/v1/backend/health>
- AgentGuard console: <http://127.0.0.1:38008/>

Run the read-only compatibility smoke test after startup:

```bash
set -a
. ./.env
set +a
make upstream-smoke
make gateway-config-write-smoke
```

The second smoke test reads the active agentgateway configuration and submits
the same JSON through the native `POST /api/config` save path. It keeps that
potentially sensitive payload in mode-0600 temporary files and never prints it.
Preview Compose runs only the agentgateway service as the owner of
`deploy/agentgateway/config.yaml`, so the upstream Raw Configuration editor can
write the bind-mounted file without making it world-writable or running as
root.

`make preview-down` stops the stack. The BFF starts even if one source is down,
and `/system` provides source-specific recovery checks. `/healthz` reports only
that the AgentsharkX process is serving; it does not hide upstream degradation.

## Release gates and evidence

```bash
make release-gate
```

The release gate runs Go/Web/contract tests, tracked-file and browser-bundle
secret scans, SPDX/license generation, the production dependency audit, the
multi-stage image build, and the full real-BFF browser flow: start → login →
connect → emit gateway and guard events → view Audit → approve. Supply-chain
evidence is indexed under [docs/release](docs/release/README.md), screenshots
under [docs/screenshots](docs/screenshots/README.md), and operational guidance
under [Troubleshooting](docs/troubleshooting.md).

## Repository layout

```text
apps/server/              Secure Go BFF, source adapters, aggregation, and SSE
apps/web/                 React console, generated API client, MSW, and browser tests
api/openapi.yaml          AgentsharkX-owned API contract
api/upstream-contracts/   Sanitized, versioned upstream response samples
deploy/                   Pinned Compose baseline and environment template
docs/                     Architecture, capability, and compatibility records
examples/                 Minimal pinned AgentGuard client event example
scripts/                  Repository and live-upstream verification helpers
```

The staged implementation plan is
[Agentshark_New_Repository_Codex_Execution_Plan.md](Agentshark_New_Repository_Codex_Execution_Plan.md).

## License

AgentsharkX is licensed under Apache-2.0. Upstream components remain separate
processes under their own licenses; AgentGuard is GPL-3.0 and agentgateway is
Apache-2.0. See [upstream compatibility](docs/upstream-compatibility.md) for the
integration boundary and release-review requirement.
