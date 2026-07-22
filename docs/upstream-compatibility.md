# Upstream compatibility

Last verified: 2026-07-22.

Phase 3 still prevents direct browser contact with either upstream. The
agentgateway client now reads explicit resources from `/api/config` and invokes
the verified read-only analytics POST with independent timeout, bounded retry,
and source-scoped errors. AgentGuard requests use the verified `X-Api-Key`
header. The pinned agentgateway admin token setting is not transmitted because
the selected upstream exposes no verified native admin-auth header.

## Pinned baseline

| Upstream | Selected release | Immutable revision | Runtime artifact |
|---|---|---|---|
| agentgateway | `v1.3.1` | `dbaaf7ed73671e7aec9195e35e7f726c0b14b84a` | `cr.agentgateway.dev/agentgateway:v1.3.1@sha256:c3ce7b75da90fef70239befcc1c3adc05152d7b9dd21fcb8351178026a2c4381` |
| AgentGuard | `v2.1` | `6f95deb9f405eca41efb6cc58ccee5b1791c7b03` | Built from `https://github.com/WhitzardAgent/AgentGuard.git#6f95deb9f405eca41efb6cc58ccee5b1791c7b03` as local image `agentsharkx/agentguard:v2.1` |

The agentgateway GitHub release API reported `v1.4.0-alpha.2` as
`prerelease=false`, even though the tag is explicitly an alpha. Phase 0
therefore selected the latest release whose semantic tag has no pre-release
suffix: `v1.3.1`. AgentGuard `v2.1` is its latest stable tag.

`deploy/versions.env` is the machine-readable source of these pins. Default
Compose configuration contains no floating tag.

## Runtime verification record

Both pinned revisions were run as containers on 2026-07-21. Sanitized responses
are stored under `api/upstream-contracts/`.

### agentgateway v1.3.1

- `GET :15021/healthz/ready` returned `200 ready`.
- `GET /api/runtime` returned version `1.3.1`, the pinned Git revision, and
  `gatewayMode=standalone`.
- `GET /api/config` and `GET /config_dump` returned the loaded empty Phase 0
  configuration and normalized stores.
- `GET /api/costs/models` returned `loaded=false` and an empty provider list.
- `GET :15020/metrics` returned Prometheus metrics.
- Log search and analytics routes exist but returned HTTP 500 with
  `request log database is not configured` under the minimal config. These
  capabilities are `partial`, not silently empty.

The image binds the admin listener to container loopback by default. Port
publishing alone therefore resets external connections. Compose sets
`ADMIN_ADDR=0.0.0.0:15000` and `STATS_ADDR=0.0.0.0:15020`; host publication
remains loopback by default.

The pinned standalone management surface exposes runtime information, config,
config dump, logs, analytics, costs, and UI routes. It does not expose dedicated
Provider, Model, MCP Server, Listener, Route, Policy, or Guardrail read APIs.
Adapters must use explicit fields from config/config-dump and treat missing
sections as unavailable. Advanced workflows remain in the native console.

For Phase 3, the populated config shape and UI routes were also checked against
the exact pinned source revision. The sanitized
`config-populated.response.json` freezes providers, direct and virtual models,
top-level MCP targets, HTTP routes, and TCP routes while excluding `params`,
policies, API keys, and other sensitive fixture values. Contract tests fail
with a field-scoped error when required names, provider shapes, routing
strategies, or MCP transport shapes change.

### AgentGuard v2.1

- Authenticated health, stats, tools, skills, MCPs, rules, traffic, audit,
  approvals, sessions, and auditors returned HTTP 200.
- An unauthenticated backend health request returned HTTP 401 with
  `missing backend API key`.
- Runtime OpenAPI reported 45 routes and OpenAPI info version `0.3.0`, while the
  package/release version is `2.1`. AgentsharkX records both and does not assume
  they are interchangeable.
- There is no dedicated agent-list route. Agent views may use only explicit
  AgentGuard `agent_id` fields from resources and sessions.

The upstream `v2.1` Dockerfile healthcheck calls `/health`, which returns 404;
the real protected route is `/v1/backend/health`. Compose overrides the
server healthcheck and supplies `X-Api-Key`; the console service receives its
own port-38008 root-page check. This is why an unmodified upstream image can
appear `unhealthy` even when its API or console is serving successfully.

AgentGuard does not publish a prebuilt container image for this release. The
Compose build context points at the release's full Git revision instead of
copying or vendoring GPL source into AgentsharkX.

## Authentication and exposure

AgentGuard management routes require `X-Api-Key`. agentgateway standalone admin
routes have no verified native authentication. AgentsharkX must therefore keep
both upstream management planes off the public network and place its own
authenticated BFF in front of browser access. The development Compose file
publishes management ports to `127.0.0.1` unless explicitly changed.

## License boundary

agentgateway is Apache-2.0. AgentGuard is GPL-3.0. AgentsharkX integrates them as
separate processes over HTTP and does not copy, vendor, link, or subclass their
implementations. AgentGuard source fetched during a Compose build is not part
of this repository. This technical separation is not a legal opinion; before a
release, regenerate the dependency/license inventory and obtain a formal
license review.

## Upgrade protocol

1. Inspect the new release tag and immutable revision; reject floating tags.
2. Start each upstream independently with the candidate pin.
3. Re-run `make upstream-smoke` and capture sanitized read responses.
4. Compare every adapter field with `api/upstream-contracts/`.
5. Exercise write contracts against a disposable environment.
6. Update `deploy/versions.env`, samples, this document, and the capability
   matrix in one commit.
7. A missing or changed route becomes `partial` or `unavailable`; do not add a
   guessed compatibility shim.
