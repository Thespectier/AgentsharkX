# Upstream compatibility

Last verified: 2026-07-22.

Phase 7 still prevents direct browser contact with either upstream. The
agentgateway adapter remains read-only and now also reads redacted request-log
and Analytics contracts. The AgentGuard adapter reads Trust, Protect, and Audit
resources and invokes
verified label, detection, runtime-rule, and approval mutations with
`X-Api-Key`, source-scoped errors, strict response bounds, and no automatic
write retries. The pinned agentgateway admin token setting is not transmitted
because the selected upstream exposes no verified native admin-auth header.

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

The pinned native UI writes configuration through `POST /api/config`. Its
implementation accepts only a file-backed `ConfigSource`, validates the proposed
configuration, and writes the active YAML file. The preview therefore mounts
`deploy/agentgateway/config.yaml` read-write so **Configure agentgateway** can
save through the upstream-owned editor; the admin port remains bound to
loopback. AgentsharkX still does not accept, inspect, or relay raw configuration
or provider credentials.

For Phase 3, the populated config shape and UI routes were also checked against
the exact pinned source revision. The sanitized
`config-populated.response.json` freezes providers, direct and virtual models,
top-level MCP targets, HTTP/TCP routes, and sanitized route/backend policy
placement while excluding secret params, policy bodies, API keys, and other
sensitive values. Contract tests fail
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

For Phase 4, populated reads plus tool-label, Skill detection, and MCP detection
shapes were cross-checked against the exact pinned source revision and its HTTP
tests. Sanitized fixtures freeze the fields used by the adapter. Contract tests
fail with a field-scoped error when required arrays, IDs, names, label objects,
or detector result shapes change.

A disposable pinned container was also populated on 2026-07-22 with one
session, tool, and Skill. The authenticated Phase 4 BFF returned the explicit
Agent with `principal=null` and `status=unknown`, applied a CSRF-protected tool
label update using the server-confirmed response, and polled the Skill detection
wrapper to `succeeded`. No disposable session credentials or detector detail
payloads were retained in repository fixtures.

The upstream detection endpoints are synchronous and do not expose a remote job
ID or percentage progress. AgentsharkX therefore owns only a bounded in-memory
wrapper job, polls that wrapper state, applies a configurable deadline, and
forwards no fabricated progress. The adapter deliberately drops session keys,
client URLs, arbitrary metadata/principal objects, descriptors, source/code
paths, detector metadata/reasons, MCP URLs, and LLM configuration.

For Phase 5, the rule list/check/publish/delete, pending approval/resolve, and
per-agent plugin config/available shapes were cross-checked against the exact
`v2.1` source and captured as sanitized fixtures. The BFF deliberately omits
rule source and prompt fields, approval tool arguments and targets, plugin
parameters, and arbitrary event bodies. Publish requires exactly one successful
current syntax check; its token is short-lived, source-bound, one-use, and held
only in bounded memory.

Publish, delete, approve, and deny use a dedicated operation client with zero
automatic retries. Fake-upstream BFF tests confirm success, upstream 404, and a
client timeout followed by a distinct manual retry. Successful operations emit
only request ID, operation, target, status, and `note_present=true` to the
structured audit log; rule source and operator note are never logged.

The upstream `v2.1` Dockerfile healthcheck calls `/health`, which returns 404;
the real protected route is `/v1/backend/health`. Compose overrides the
server healthcheck and supplies `X-Api-Key`; the console service receives its
own port-38008 root-page check. This is why an unmodified upstream image can
appear `unhealthy` even when its API or console is serving successfully.

For Phase 6, request-log search, Analytics, AgentGuard Traffic/Audit/Sessions,
and their exact populated shapes were cross-checked against the pinned source.
The gateway request always sets `includeAttributes=false` and never calls
payload detail. The AgentGuard audit projection does not decode runtime state,
arguments/results, plugin results, or free-form reasons. Contract tests include
sentinel secrets in those omitted fields and fail if they reach normalized
JSON. AgentGuard Traffic supplies aggregate scalars only because its records do
not contain a stable upstream event ID; normalized security events come from
Audit's explicit `event_id` instead.

The BFF polls every two seconds by default, keeps at most 1000 normalized events
in memory, and uses independent source failures. SSE sequence IDs are owned by
AgentsharkX solely for bounded delivery/resume and are not presented as
upstream identity or cross-source correlation. Correlation remains false unless
the same explicit non-empty identifier appears in both sources.

AgentGuard does not publish a prebuilt container image for this release. The
Compose build context points at the release's full Git revision instead of
copying or vendoring GPL source into AgentsharkX.

For Phase 7, the production image embeds only the AgentsharkX Web build and Go
BFF. agentgateway and AgentGuard remain separate Compose services and SPDX
packages connected over HTTP. The AgentGuard quickstart client is installed
from the exact pinned Git revision in a disposable virtual environment; it is
not copied into or linked with the AgentsharkX image.

The post-preview usability pass exposes the validated AgentGuard native-console
URL beside the verified AgentsharkX rule, label, scan, and approval mutations.
This is a link-out only: no unverified AgentGuard configuration endpoint or
field was added to the adapter.

The release E2E fixtures implement only already-frozen contract shapes and run
outside default Compose. They prove BFF/browser orchestration, including a hard
navigation followed by CSRF recovery and one approval, but do not replace live
upstream compatibility verification. The image and Compose build passed on
2026-07-22 with the pinned Node 24.13.0, Go 1.26.5, and Alpine 3.23.3 digests.

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
