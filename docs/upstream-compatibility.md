# Upstream compatibility

Last verified: 2026-07-27.

Phase 9 still prevents direct browser contact with either upstream. The
agentgateway adapter reads request-log summary, complete detail, and Analytics contracts and now
performs the typed Provider/direct-Model main-form configuration workflow. The
AgentGuard adapter reads Trust, Protect, and Audit
resources and invokes
verified label, detection, runtime-rule, and approval mutations with
`X-Api-Key`, source-scoped errors, strict response bounds, and no automatic
write retries. The pinned agentgateway admin token setting is not transmitted
because the selected upstream exposes no verified native admin-auth header.

## Pinned baseline

| Upstream     | Selected release              | Immutable revision                         | Runtime artifact                                                                                                                                                                        |
| ------------ | ----------------------------- | ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| agentgateway | `v1.3.1`                      | `dbaaf7ed73671e7aec9195e35e7f726c0b14b84a` | Default: official host binary verified by platform SHA-256; fallback: `cr.agentgateway.dev/agentgateway:v1.3.1@sha256:c3ce7b75da90fef70239befcc1c3adc05152d7b9dd21fcb8351178026a2c4381` |
| AgentGuard   | main snapshot (package `2.1`) | `4b755fb4a4a2763b7e817b3d0220fe5c22187b59` | Built from `https://github.com/WhitzardAgent/AgentGuard.git#4b755fb4a4a2763b7e817b3d0220fe5c22187b59` as local image `agentsharkx/agentguard:main-4b755fb`                              |

The agentgateway GitHub release API reported `v1.4.0-alpha.2` as
`prerelease=false`, even though the tag is explicitly an alpha. Phase 0
therefore selected the latest release whose semantic tag has no pre-release
suffix: `v1.3.1`. AgentGuard `v2.1` remains its latest stable tag, but the
official repository's startup script builds the current checkout and main now
contains newer opt-in Thought-Aligner support. The preview therefore uses the
exact verified main revision rather than the older release commit or a floating
tag.

`deploy/versions.env` is the machine-readable source of these pins. It records
the Linux amd64, Linux arm64, and Darwin arm64 binary checksums plus the
container digest. Default configuration contains no floating tag.

## Runtime verification record

Both pinned revisions were run as containers again on 2026-07-23, and the
official agentgateway Linux amd64 binary was independently checksum/version
verified. Sanitized management responses are stored under
`api/upstream-contracts/`.

### agentgateway v1.3.1

- The official Linux amd64 binary matched its pinned SHA-256, reported version
  `1.3.1` and the selected Git revision prefix, and remained active under the
  user-level systemd manager after the launch command exited.
- `GET :15021/healthz/ready` returned `200 ready`.
- `GET /api/runtime` returned version `1.3.1`, the pinned Git revision, and
  `gatewayMode=standalone`.
- `GET /api/config` and `GET /config_dump` returned the loaded empty Phase 0
  configuration and normalized stores.
- A populated `GET /api/config` was rechecked on 2026-07-27 and returned the
  accepted object discriminator `provider.custom` alongside model
  `provider.reference`. Phase 8 exposes the verified custom format names/paths
  and allow-listed base params but never returns credential values or unowned
  provider fields.
- `GET /api/costs/models` returned `loaded=false` and an empty provider list.
- `GET :15020/metrics` returned Prometheus metrics.
- The configured host-native LLM listener returned the explicit model from
  `GET :4000/v1/models`; a minimal chat completion reached the configured
  DeepSeek provider and returned HTTP 200 with a valid chat-completion shape.
- The exact v1.3.1 schema and source confirm `config.database.url` selects the
  request-log backend; a non-PostgreSQL URL uses SQLite, creates the schema, and
  enables WAL automatically.
- The bundled preview SQLite store made `POST /api/logs/search` and
  `POST /api/logs/analytics/summary` return HTTP 200. After one real DeepSeek
  proxy request, summary search returned one HTTP-200 record and Analytics
  returned one request with its token count.

The default Linux preview starts the official binary with an explicit file
source and loopback admin, metrics, and readiness addresses. LLM and MCP
listeners from that file bind directly on the host, which removes the need to
predict and publish business ports through Compose. The binary is stored only
under ignored `.cache/` after its checksum and embedded release/revision match
the pinned metadata.

The explicit file uses the verified `config.database.url` field with the
repository-relative SQLite URL
`sqlite://.cache/agentgateway-standalone/data/request-logs.db`. Both the native
launcher and fallback container run from the repository root and keep that
directory persistent and owner-only. This store is owned by agentgateway, not
AgentsharkX. The pinned upstream retains LLM prompt/completion payload rows when
available. AgentsharkX periodic search uses `includeAttributes=false`; an
authenticated single-event detail request calls `/api/logs/get` with
`includePayload=true` and returns the complete source object without placing it
in the polling ring or SSE. The pinned v1.3.1 UI route
`/ui/llm/logs?log=<id>` remains an alternate view of that exact record. The
pinned v1.3.1 request types
also verify explicit `timeRange` on log search and
`timeRange` plus `bucketSeconds` on Analytics; AgentsharkX uses one exact
rolling 60-minute range and 300-second buckets for both Home and Audit.

Native Linux Docker connects the BFF through host networking. Docker Desktop
was separately verified to reach the same loopback-only readiness listener
through `host.docker.internal` while retaining the normal bridge publication
for AgentsharkX. Auto-detection selects between these two overlays; neither
requires binding the unauthenticated admin listener to all interfaces. In the
Docker Desktop topology, authenticated `GET /api/v1/system/health` reported
both agentgateway and AgentGuard healthy with `partial=false`.

The fallback image binds the admin listener to container loopback by default.
Port publishing alone therefore resets external connections. Compose sets
`ADMIN_ADDR=0.0.0.0:15000` and `STATS_ADDR=0.0.0.0:15020`; host publication
remains loopback by default.

The pinned standalone management surface exposes runtime information, config,
config dump, logs, analytics, costs, and UI routes. It does not expose dedicated
Provider, Model, MCP Server, Listener, Route, Policy, or Guardrail read APIs.
Adapters must use explicit fields from config/config-dump and treat missing
sections as unavailable. Advanced workflows remain in the native console.

The pinned native UI writes the entire configuration through `POST /api/config`.
Its implementation accepts only a file-backed `ConfigSource`, validates the
proposed configuration, and writes the active YAML file. It exposes no ETag,
revision, or conditional-update header. The default native process uses
the explicit checkout file as the checkout user, so **Configure agentgateway**
can save without container ownership translation; the admin port remains bound
to loopback. The container fallback still mounts the file read-write and aligns
only the gateway service with its non-root owner. A live
read-and-unchanged-write through `POST /api/config` returned 200; the same
request under the image's default UID `65532` returned 500 permission denied
against the checkout-owned mode-0644 file. The
`make gateway-config-write-smoke` check keeps the potentially sensitive config
in mode-0600 temporary files and never prints it.

Phase 8 does not accept raw configuration. The BFF necessarily parses a bounded
whole-document snapshot to preserve unowned fields and opaque existing
credential values, but the public read contract returns only allow-listed
Provider/direct-Model settings plus credential configured/kind state. The typed
write contract matches the pinned main forms: API keys can use ambient,
environment-variable, file, or write-only literal input; Bedrock accepts AWS
static credentials, Vertex accepts a GCP credential file, and Azure accepts
managed identity. Direct models support incoming, explicit, stripped-prefix,
and custom CEL outgoing-model mappings. Each write uses a bounded five-minute,
one-use revision token, an in-process mutation lock, a fresh revision check, one
upstream POST, and a refetch that verifies the requested result. Credential
values are not returned or logged. Provider type and Model provider binding are
immutable on update. A confirmed Provider deletion may remove its directly
referenced Models in the same whole-document write; deletion is rejected when
any affected Model is referenced by a Virtual Model. Direct Model deletion is
also rejected while a Virtual Model references it.

Because the upstream has no conditional update, a native-console or YAML write
can still occur between the BFF's final read and whole-document POST. The BFF
rejects detectable stale revisions but cannot make that external race atomic.
It never retries an ambiguous write automatically.

Phase 9 applies the same whole-document revision and single-POST discipline to
MCP configuration. The pinned `LocalSimpleMcpConfig` contract verifies `port`,
`statefulMode`, nullable `prefixMode`, nullable `failureMode`, `policies`, and
`targets`. The pinned `LocalMcpTarget` union verifies Streamable HTTP (`mcp`),
legacy SSE (`sse`), stdio, and OpenAPI shapes. AgentsharkX returns complete
typed network and stdio fields to the authenticated console and manages the
same three target kinds exposed by the native main editor. It preserves target
and global policies without returning their bodies. OpenAPI target editing and
route-owned inline targets remain advanced/read-only. MCP and LLM writes share
one process-local mutation lock because both replace the same `/api/config`
document.

For Phase 3 and the Phase 8/9 typed-write rounds, the populated config shape, UI
routes, client hook, and file-backed write implementation were checked against
the exact pinned source revision. The sanitized
`config-populated.response.json` freezes string, reference, and custom provider
shapes plus direct and virtual models,
top-level MCP targets, HTTP/TCP routes, and sanitized route/backend policy
placement while excluding secret params, policy bodies, API keys, and other
sensitive values. Contract tests fail
with a field-scoped error when required names, provider shapes, routing
strategies, or MCP transport shapes change.

### AgentGuard main snapshot `4b755fb`

- Authenticated health, stats, tools, skills, MCPs, rules, traffic, audit,
  approvals, sessions, and auditors returned HTTP 200.
- An unauthenticated backend health request returned HTTP 401 with
  `missing backend API key`.
- Runtime OpenAPI reported 45 routes and OpenAPI info version `0.3.0`, while the
  package version is `2.1`. AgentsharkX records both and does not assume
  they are interchangeable.
- There is no dedicated agent-list route. Agent views may use only explicit
  AgentGuard `agent_id` fields from resources and sessions.
- The verified main snapshot contains the server-side Thought-Aligner plugin
  and example plugin configuration. It remains opt-in and upstream-owned;
  AgentsharkX does not invent a management route for it. Preview Compose
  forwards its three dedicated endpoint/model/key environment variables only
  to the AgentGuard server so the upstream example config can be selected
  without storing a key in JSON.
- Its 45 management OpenAPI paths are identical to the checked-in summary, and
  the read-only compatibility smoke passed against the rebuilt server.

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
per-agent plugin config/available shapes originated from the exact `v2.1`
source, were revalidated against the pinned main snapshot, and remain captured
as sanitized fixtures. The BFF deliberately omits
rule source and prompt fields, approval tool arguments and targets, plugin
parameters, and arbitrary event bodies. Publish requires exactly one successful
current syntax check; its token is short-lived, source-bound, one-use, and held
only in bounded memory.

Publish, delete, approve, and deny use a dedicated operation client with zero
automatic retries. Fake-upstream BFF tests confirm success, upstream 404, and a
client timeout followed by a distinct manual retry. Successful operations emit
only request ID, operation, target, status, and `note_present=true` to the
structured audit log; rule source and operator note are never logged.

The pinned upstream Dockerfile healthcheck calls `/health`, which returns 404;
the real protected route is `/v1/backend/health`. Compose overrides the server
healthcheck and supplies `X-Api-Key`; the console service receives its own
port-38008 root-page check. This is why an unmodified upstream image can appear
`unhealthy` even when its API or console is serving successfully.

For Phase 6, request-log search, Analytics, AgentGuard Traffic/Audit/Sessions,
and their exact populated shapes were cross-checked against the pinned source.
The gateway summary and Analytics requests share an explicit 60-minute
`timeRange`; search sets `includeAttributes=false` and Analytics sets
`bucketSeconds=300`. Authenticated detail calls the source-verified
`/api/logs/get` contract with `includePayload=true` and preserves arbitrary
attribute/payload JSON. AgentGuard normalized rows use verified typed fields,
while their authenticated detail retains the complete source object including
runtime state, arguments/results, plugin results, and free-form reasons.
AgentGuard Traffic supplies aggregate scalars only
because its records do not contain a stable upstream event ID; normalized
security events come from Audit's explicit `event_id` instead.

AgentGuard's approval mutation contract returns only `{"ok": true}` and does
not append the resolved decision to `/traffic` or `/audit/recent`. AgentsharkX
now retains the confirmed approve/deny transition as bounded, source-labelled
management evidence after a successful mutation. The exact source ticket JSON
is retained internally for this authenticated Audit detail, together with the
operator note and confirmed decision, while list and SSE projections omit it.
This uses the verified ticket and event IDs captured before resolution; it does
not infer an outcome from timing, and failures or timeouts are not recorded as
decisions. Denied approvals are included in Audit deny metrics and trend
buckets.

The BFF polls every two seconds by default, keeps at most 1000 normalized events
in memory, and uses independent source failures. SSE sequence IDs are owned by
AgentsharkX solely for bounded delivery/resume and are not presented as
upstream identity or cross-source correlation. Correlation remains false unless
the same explicit non-empty identifier appears in both sources.

AgentGuard does not publish a prebuilt container image for this snapshot. Its
official `scripts/start.sh` builds the current source tree and starts separate
server and frontend services. AgentsharkX Compose preserves that topology while
pointing the build context at the verified full Git revision instead of copying
or vendoring GPL source into AgentsharkX.

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
2026-07-23 with the pinned Node 24.13.0, Go 1.26.5, and Alpine 3.23.3 digests.

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

1. Inspect the candidate release or main snapshot and immutable revision;
   reject floating tags.
2. Start each upstream independently with the candidate pin.
3. Re-run `make upstream-smoke` and capture sanitized read responses.
4. Compare every adapter field with `api/upstream-contracts/`.
5. Exercise write contracts against a disposable environment.
6. Update `deploy/versions.env`, samples, this document, and the capability
   matrix in one commit.
7. A missing or changed route becomes `partial` or `unavailable`; do not add a
   guessed compatibility shim.
