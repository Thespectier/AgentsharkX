# AgentsharkX

AgentsharkX is a lightweight management console above
[agentgateway](https://github.com/agentgateway/agentgateway) and
[AgentGuard](https://github.com/WhitzardAgent/AgentGuard). It will provide one
information architecture for connection management, trusted runtime context,
protection workflows, and audit views without entering the agent data plane or
reimplementing either upstream.

The repository is currently at **Phase 1**: the reproducible project skeleton,
pinned upstream contracts, and a complete reviewable web console are in place.
The console uses clearly labelled MSW fixtures until the Go BFF starts in
Phase 2; it does not send requests directly to either upstream.

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
- Node.js 24 and npm
- Go 1.26.5 when developing the server locally (the Makefile can use the pinned
  Go container if Go is not installed)

## Verify the repository

```bash
npm ci --prefix apps/web
make verify
```

This checks Go formatting/tests, the frontend format/type/unit/build suite,
repository invariants, the OpenAPI contract, and the fully rendered Compose
model.

## Review the Phase 1 console

```bash
npm --prefix apps/web run dev
```

Open <http://127.0.0.1:5173>. The top-bar demo selector exposes the normal,
empty, loading, partial-failure, and total-failure states. Browser acceptance
requires Playwright Chromium; see [the web README](apps/web/README.md) for host
and container commands. The checked-in 1440 px and 1280 px baselines are
indexed under [docs/screenshots](docs/screenshots/README.md).

## Start the pinned upstreams

The AgentGuard release does not publish an upstream image. Compose therefore
builds it directly from the verified `v2.1` commit and assigns a local image
name; no source is vendored into this repository.

```bash
cp deploy/example.env .env
# Replace every change-me value before exposing services beyond loopback.
docker compose --env-file deploy/versions.env --env-file .env \
  -f deploy/compose.yaml up --build
```

Default local endpoints:

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
```

## Repository layout

```text
apps/server/              Go BFF (implementation begins in Phase 2)
apps/web/                 React console, MSW fixtures, and browser tests
api/openapi.yaml          AgentsharkX-owned API contract
api/upstream-contracts/   Sanitized, versioned upstream response samples
deploy/                   Pinned Compose baseline and environment template
docs/                     Architecture, capability, and compatibility records
scripts/                  Repository and live-upstream verification helpers
```

The staged implementation plan is
[Agentshark_New_Repository_Codex_Execution_Plan.md](Agentshark_New_Repository_Codex_Execution_Plan.md).

## License

AgentsharkX is licensed under Apache-2.0. Upstream components remain separate
processes under their own licenses; AgentGuard is GPL-3.0 and agentgateway is
Apache-2.0. See [upstream compatibility](docs/upstream-compatibility.md) for the
integration boundary and release-review requirement.
