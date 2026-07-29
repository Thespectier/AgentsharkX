# Architecture

Status: Phase 13 persistent Audit foundation over the Phase 12 management surface, verified 2026-07-29.

## Context

AgentsharkX is a management-plane application above two independent upstream
processes. It presents a shared console while preserving the ownership and
semantics of every upstream capability.

```text
Browser ──HTTPS──> AgentsharkX Web + Go BFF
                         │
                         ├──SQL──────────────> PostgreSQL
                         │                    Audit / checkpoints / outbox
                         │
                         ├──management HTTP──> agentgateway
                         │                       │
                         │                  LLM / MCP / A2A
                         │
                         └──management HTTP──> AgentGuard
                                                 ▲
Agent runtime ───────────── AgentGuard client ────┘
Agent runtime ───────────── business traffic ─────────> agentgateway
```

AgentsharkX never sits in the agent business-data path. The browser never
receives an upstream API key. The BFF keeps an independent client, timeout,
capability registry, and error state for each source.

## Ownership

| Component    | Owns                                                                                                                                           | Does not own                                                               |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| agentgateway | LLM/MCP/A2A/HTTP proxying, routes, providers, policies, guardrails, request logs, cost and latency telemetry                                   | AgentGuard runtime identities, reviews, or rules                           |
| AgentGuard   | Runtime interception, resources, labels, runtime rules, approvals, traffic, sessions, and security audit                                       | Gateway routing or transport policy                                        |
| AgentsharkX  | Console navigation, admin authentication, source adapters, capability detection, normalization, aggregation, persistent Audit projections/checkpoints/outbox, SSE, and high-frequency workflows | Proxying, task inference, a rules engine, business-traffic replay, or upstream source-of-truth storage |

## BFF boundaries

The Go BFF is organized into the following packages:

- `config`: validated environment configuration with secret-safe diagnostics.
- `auth`: one-admin session, strict cookie, CSRF, and write protection.
- `gateway`: agentgateway management adapter.
- `connect`: filtering, cursor pagination, details, setup verification,
  short-lived configuration revision tokens, one shared serialized coordinator
  for typed LLM/MCP/Traffic/Policy/Guardrail mutations, and native-console links over
  verified gateway resources.
- `guard`: AgentGuard management adapter.
- `trust`: explicit AgentGuard identity/resource aggregation, filtering,
  pagination, label writes, and bounded scan-job orchestration.
- `protect`: authenticated complete gateway-policy and global-guardrail configuration workflows,
  independent AgentGuard rule/plugin aggregation, short-lived rule-check tokens,
  guarded mutations, approval pagination, and duplicate-operation locks.
- `audit`: independent polling, source-scoped failures, normalized summaries,
  authenticated complete source detail, exact-ID session counts, metrics,
  trends, persistent event writes, source checkpoints, and a bounded current
  activity snapshot.
- `aggregate`: source-preserving view models and partial-result handling.
- `storage`: small Audit event, payload, checkpoint, outbox, readiness, migration,
  and retention interfaces with PostgreSQL production and memory test adapters.
- `stream`: commit notification only; PostgreSQL outbox sequences are the sole
  SSE IDs and replay source.
- `api`: OpenAPI-backed HTTP handlers and standard errors.
- `model`: source-preserving health, capability, Connect/Trust/Protect/Audit
  resource, overview, event, and error contracts.
- `upstream`: bounded retries and response-size limits shared by the two
  adapters without sharing their source state.

Adapters expose upstream facts. Aggregation may reduce display differences but
must retain `source`, upstream object ID, original-detail reference, scope, and
phase. A correlation flag is allowed only when both sources provide the same
identifier and the adapter explicitly verifies its meaning.

## Frontend boundary

The React/Vite console remains independently reviewable with Mock data. Its
TanStack Router paths and TanStack Query requests target only the
AgentsharkX-owned paths in `api/openapi.yaml`. MSW intercepts those paths in the
browser and supplies source-labelled REST envelopes plus a bounded Mock SSE
stream. With `VITE_ENABLE_MOCKS=false`, an OpenAPI-generated client uses the
same paths through the Go BFF. The real mode exchanges the admin token for a
strict session and keeps the CSRF token only in module memory; after a hard
reload, `GET /api/v1/auth/session` validates the HttpOnly cookie and reissues
that session's CSRF value without persisting the administrator token. No frontend
module imports upstream code or receives an upstream credential.

The application shell owns one SSE connection, deduplicates deliveries by
outbox ID, upserts events by normalized `(source, id)`, and invalidates
source-scoped query families when a verified event arrives. A `reset` event
clears the live cache and refetches REST state. Active queries also refresh on a
bounded interval, on navigation, and when the window regains focus. Successful
AgentGuard mutations invalidate Trust, Protect, Audit, and Overview together so
cross-page counts and rows converge without a hard browser reload.
The Home activity flow derives its gateway request/error and AgentGuard
decision/explicit-deny counts from the same Overview metrics and trend snapshot.
It selects event paths only from the normalized source and explicit decision or
error classification; it does not display fixed topology counts or infer LLM,
MCP, A2A, or cross-source relationships. Narrow viewports render the same data
as readable source rows instead of shrinking the SVG labels.

The five primary views are Home, Connect, Trust, Protect, and Audit. System is
a supporting diagnostics page, not another product capability. URL search
state preserves demo failure scenarios and Audit event selection so a detail
drawer can be restored after refresh. The application shell renders the active
workspace's section routes as nested sidebar links; page headers do not duplicate
that navigation. The URL remains the source of truth for the selected section,
including after reload and while the desktop sidebar is collapsed.

The application shell also owns presentation locale and time-zone policy. The
English/Chinese selection is persisted only as a non-sensitive browser preference
and updates the document language, navigation, shared controls, and primary
business surfaces without a reload. API timestamps remain ISO 8601 instants from
the BFF; the browser formats user-facing timestamps and trend buckets in
`Asia/Shanghai` and labels them `UTC+8`. Audit event and session rows include the
full Beijing calendar date plus time so records from different days are never
presented as same-day events. The Home greeting selects its day period from the
current Beijing hour, so browser or host time-zone settings cannot change its
meaning.

## Availability model

Health and capability state are source-scoped:

```text
healthy     request and capability probes succeeded
degraded    source is reachable but one or more capabilities failed
down        health request failed or timed out
unknown     probing has not completed
```

A gateway failure cannot suppress AgentGuard data, and an AgentGuard failure
cannot suppress gateway data. Aggregated responses carry per-source metadata
and stale markers rather than collapsing partial failures into a global 500.

## Phase 13 runtime data and storage

PostgreSQL is required production state for Audit. Background monitors poll the
two health contracts and, every two seconds by default, the verified Audit read
contracts. Each source commits independently: normalized event upserts, an
optional payload row, one outbox row for each real event change, and that
source's checkpoint share one transaction. `(source, upstream_id)` provides
idempotency, and the in-memory stream Hub is notified only after commit. One
upstream or database write failure cannot be presented as successful data from
the other upstream. An event's source-owned occurrence time becomes immutable
at first insert; later updates can change its normalized content or retained
payload without moving it across a keyset pagination boundary.

Audit event metadata defaults to 30-day retention. The current analytics
snapshot remains bounded to 1000 rows, while `/api/v1/audit/events` uses a
stable keyset cursor pinned to the first page's database watermark. Outbox rows
default to 24-hour retention. Their `bigserial` sequence is the sole SSE `id`:
reconnecting clients send `Last-Event-ID`, receive retained outbox rows first,
and then wait on the commit notifier. A cursor ahead of the database or older
than the retained window produces `event: reset`; the browser discards its live
assumption and refetches REST state. A singleton outbox-state row advances in
the same transaction and tracks only the latest committed sequence, so an
uncommitted or rolled-back PostgreSQL sequence value can never move a reset
cursor past an event that later becomes visible. Every outbox writer locks that
row before allocating a sequence and retains the lock through commit, which
also makes sequence order equal commit visibility order across BFF instances.

`ingest_checkpoints` records each adapter's last attempt, success, error, and
last observed event watermark. The verified agentgateway and AgentGuard reads
do not expose a fully verified request-side historical cursor across all Phase
13 feeds. The checkpoint therefore preserves observed polling state for
operations diagnostics, but is not sent back as an unverified upstream cursor.
After restart the adapters repeat their bounded reads and PostgreSQL uniqueness
deduplicates them; records that aged out of an upstream window during downtime
cannot be recovered.

Polling may still return records older than the configured event retention.
When such an identity is no longer present, the store advances the source
checkpoint but does not recreate the event or an outbox message. An existing
identity remains updateable until the scheduled retention prune removes it.

Payload storage is a separate table and is disabled by default
(`AGENTSHARK_PAYLOAD_RETENTION=0`). A positive duration, no longer than event
retention, opts AgentGuard/approval raw records into durable detail storage.
List, overview, and SSE projections always omit `raw`. Gateway detail continues
to call the verified upstream `/api/logs/get` contract on demand, so the
upstream-owned request-log retention remains authoritative.

Connect reads a bounded `/api/config` snapshot per request. It never returns LLM
credential values or prompt payloads. The LLM
management contract returns only verified provider/direct-model main-form fields,
allow-listed params, custom format names/paths, and credential configured/kind
state. Credential changes use typed, write-only inputs for native ambient,
environment-variable, file, literal, AWS static, GCP credential-file, and Azure
managed identity modes; values are never included in a read response. Direct
models expose the verified incoming, explicit, stripped-prefix, and custom CEL
outgoing-model mappings.
The authenticated MCP management contract returns complete verified top-level
listener/federation settings and target connection fields: Streamable HTTP/SSE
URL, host/port/path, or backend reference, plus stdio command, arguments,
environment map, and inherited-environment behavior. OpenAPI targets and
route-owned inline MCP targets retain their scope and remain read-only. The
authenticated Protect policy contract separately returns the complete
source-owned JSON value for every verified global MCP policy path. The
authenticated Traffic configuration contract returns complete source-owned
Listener and Route objects, including TLS and Listener/Route/Backend policy
objects, so administrators can
edit them without a second policy engine in AgentsharkX. Protect Policies is
split into **LLM / MODEL** and **MCP** views. It manages the exact verified
global LLM, direct-model, and native global MCP policy paths without interpreting
their JSON, while route/backend policies remain in Connect Traffic. Protect
Guardrails projects the two exact global paths
`/llm/policies/guardrails` and `/mcp/policies/mcpGuardrails` into separate LLM
and MCP workspaces. It retains complete ordered parent objects, provides typed
controls for every verified v1.3.1 variant, and keeps a complete JSON editor for
advanced source-owned fields. Direct-model guardrails remain in Protect
Policies; MCP-target and route/backend guardrails remain in their owning scopes.
Trust reads
the four AgentGuard session/resource routes independently, so one failed capability does
not erase successful siblings. It whitelists display fields and never returns
session keys, client URLs, arbitrary principal/metadata objects, descriptors,
file contents, MCP URLs, detector metadata, reasons, or LLM configuration.

The pinned agentgateway exposes only whole-document `GET` and `POST /api/config`
operations and no ETag. The BFF therefore issues bounded five-minute, one-use
revision tokens, serializes all in-process LLM, MCP, Traffic, and Protect policy/guardrail
mutations, rereads immediately before the change, preserves unowned fields and
opaque credential values, posts exactly once, then refetches and verifies the
requested result. A native-console or YAML
write can still race between the final read and whole-document POST; the BFF
rejects detectable stale revisions but cannot make that upstream gap atomic.
Provider type and model provider binding are immutable during Phase 8 edits.
An explicitly confirmed Provider deletion may remove its directly referenced
Models in the same whole-document write. If a Virtual Model targets any of
those Models, the BFF rejects the operation instead of mutating upstream-owned
advanced routing.

Connect MCP writes cover the verified global settings and top-level Streamable
HTTP, SSE, and stdio target forms. They preserve MCP policies, OpenAPI targets,
route-owned inline targets, non-MCP configuration, and unrecognized fields;
Phase 11/12 Protect writes may change only one verified global policy or
guardrail parent path.

Traffic writes cover bind CRUD; HTTP, HTTPS, HBONE, TCP, and TLS Listener CRUD;
and HTTP/TCP Route CRUD. Listener edits preserve compatible child routes and
require an explicit destructive choice when switching protocol families.
Route objects retain all verified match arrays, backend variants, weights, TLS,
and source-owned policy objects. Each mutation changes only the selected raw
configuration path, preserves the rest of the document, performs one upstream
POST, and verifies the complete canonical document by refetching.

Agent rows are an AgentsharkX view over explicit AgentGuard `agent_id` and
`owner_agent_id` fields. No gateway log, timing window, name similarity, or
other heuristic creates an identity. A session `user_id` remains session data
and is not promoted to Agent principal. Framework, principal, trust level, and
status remain nullable or `unknown` when AgentGuard does not provide them.

AgentGuard detection calls are synchronous. The BFF exposes them as bounded,
in-memory jobs with `queued`, `running`, `succeeded`, and `failed` states. The UI
polls those real states and does not invent percentage progress. Jobs use the
configured `AGENTSHARK_SCAN_TIMEOUT`; completed state is not durable across a
BFF restart. Tool-label updates and scan starts require CSRF and are never
automatically retried.

The dedicated Guardrails workspace reuses that authenticated configuration and
mutation contract instead of introducing another endpoint or execution engine.
LLM saves replace the complete `PromptGuard` object, preserving request/response
order, optional streaming mode, every native guard variant, rejection response,
and advanced provider policy objects. MCP saves replace the complete
`McpGuardrails` parent and ordered `Processor` array, preserving targets, method
phases, CEL metadata, request-header filters, failure behavior, and backend
policy objects.
The upstream arrays have no stable child IDs, so child edits are submitted only
as one atomic parent-object write. Deleting either global guardrail follows the
native editor and removes its configuration key.

Protect reads AgentGuard rules and pending approvals, then fans out plugin
config/available reads only for explicit Trust agent IDs with fixed agent and
concurrency bounds. Gateway policy failures remain independent from AgentGuard
rule/plugin failures. Its authenticated gateway-policy configuration returns
complete JSON values for the verified agentgateway v1.3.1 catalog: twelve global
LLM policies, thirteen direct-model policy fields (including the compatible
`tls`/`backendTLS` alternatives), and eleven native global MCP policies. Each
upsert or delete addresses one stable raw path, shares the Connect configuration
revision and lock, performs exactly one whole-document POST, and verifies the
complete canonical refetch. Unknown policy keys are preserved and exposed only
as read-only facts; virtual-model, MCP-target, and route-owned policy scopes are
not edited from this page. Rule source is submitted only to AgentGuard
check/publish and is never returned by list APIs or logged. A publish token
stores only the source digest, expires after five minutes, is consumed once,
and is held in a 100-entry in-memory bound. Dangerous AgentGuard writes require
a non-empty operator note and explicit confirmation; approval/rule locks prevent
concurrent duplicate actions in one BFF process. Upstream mutations are not
automatically retried.

The Audit poller requests agentgateway summary logs with
`includeAttributes=false`; it does not pull payloads into periodic list or SSE
traffic. Log search and Analytics receive one shared,
rolling 60-minute window with twelve five-minute buckets. The BFF maps
explicit AgentGuard decisions into those same buckets and calculates
nearest-rank P95 latency from the bounded 500-record gateway summary
sample; every point exposes its sample count, and a bucket with no latency
samples remains null rather than becoming a fabricated zero.
AgentGuard records the initial `HUMAN_CHECK` in Traffic/Audit but its approval
mutation returns only an acknowledgement. After an approve/deny call is
confirmed, the Protect service therefore passes the already-normalized ticket
context to Audit, which persists a source-labelled approval-resolution event in
the same transaction model as polled events. A denied approval contributes to
the deny rate and deny trend immediately and survives a BFF restart; failed or
timed-out mutations do not create evidence.
AgentGuard normalization derives list fields from verified IDs and scalars.
When payload retention is positive, its complete source object and confirmed
approval context are stored separately from summary metadata until their own
expiry. With the default zero payload retention, complete AgentGuard detail is
available only while still held by the live process; normalized metadata
remains durable. List, overview, and SSE responses omit `raw` in every mode.
For agentgateway, an authenticated detail request uses the preserved upstream
ID with `POST /api/logs/get` and `includePayload=true`; its complete log object,
including arbitrary attributes, prompt, completion, error, and tool-call
content, crosses the BFF only in that response and is not copied to summary or
outbox rows. The native Logs deep link remains an alternate source view.

`/overview` is `mode=operational` when the Audit service is attached. Gateway
log/Analytics failures and AgentGuard Traffic/Audit/Sessions failures are
reported independently, so available peer data remains visible. AgentGuard
session event/deny counts use exact session-ID equality. Cross-source
correlation is marked verified only when both sources explicitly return the
same non-empty trace or session identifier; timestamps are never used.

Normalized Audit state, source checkpoints, and SSE delivery rows are durable
in the AgentsharkX PostgreSQL database. The bundled preview separately
configures agentgateway's upstream-owned SQLite request-log store under ignored
`.cache/agentgateway-standalone/data/`; neither database replaces the other's
source of truth. AgentsharkX still provides no task model, DAG, payload vault,
business replay engine, or traffic collector.

## Security baseline

- `AGENTSHARK_ADMIN_TOKEN` is mandatory outside explicitly loopback-only
  development mode.
- Successful login creates an `HttpOnly`, `SameSite=Strict`, `Secure` session
  cookie; write requests also require a CSRF token.
- A non-Secure cookie or disabled authentication is accepted only when both the
  environment is explicitly local/development and the listener is loopback.
- The BFF's configured upstream management credentials are server-only and
  never appear in a frontend bundle, API response, structured log, or error
  message.
- Authenticated Audit detail returns complete verified upstream event records.
  Application access logs record method/path/status only and do not copy event
  bodies; list, overview, and SSE responses omit raw detail.
- Authenticated Protect policy and guardrail configuration returns complete
  source-owned JSON. Application logs and mutation receipts contain only
  operation metadata; bodies are not copied into summary lists or SSE responses.
- The default Phase 13 preview runs the pinned agentgateway binary on the host
  and publishes the remaining management services on loopback. It is a
  development topology, not an internet-facing deployment.

## Phase 13 deployment boundary

The production Dockerfile has independent pinned Node and Go build stages. It
builds Web with `VITE_ENABLE_MOCKS=false`, replaces the development placeholder
assets before compiling, and embeds the resulting SPA into the Go binary. The
runtime stage contains only Alpine CA certificates and the static binary, runs
as UID/GID `65532:65532`, and exposes one same-origin port. `/healthz` is public
and reports process liveness only. `/readyz` is also unauthenticated and returns
success only when PostgreSQL is reachable, embedded migration checks pass, and
persisted Audit history has been restored into the current process;
authenticated `/system` diagnostics remain the authority for independent
upstream state.

The Linux preview runs agentgateway as an official host-native binary while
Compose runs a pinned PostgreSQL image, builds AgentGuard from its immutable
main revision, and builds AgentsharkX locally. The database port is published
only on the configured loopback address, and a named volume survives normal
`preview-down`/`preview-up` cycles. The binary release, Git revision, and
per-platform SHA-256 digests are fixed in `deploy/versions.env`; installation
is repository-local under ignored `.cache/` and refuses a checksum or
embedded-version mismatch.
It reads the same explicit `deploy/agentgateway/config.yaml` as the previous
container topology and runs as the checkout user, so native Raw Configuration
writes need no bind-mount UID workaround.
The explicit config enables the pinned upstream's SQLite log store at a
repository-relative path. Both the native launcher and container fallback use
the repository root as their working directory and share the same ignored data
directory, so its Logs and Analytics survive either runtime mode independently
of the AgentsharkX PostgreSQL service.

The integrated connector is environment-specific. Native Linux Docker gives
AgentsharkX host networking so the BFF reaches the gateway's loopback-only
management listener and AgentGuard's loopback-published API. Docker Desktop
keeps AgentsharkX on the Compose bridge and uses its verified
`host.docker.internal` forwarding path for the loopback gateway; AgentGuard
remains reachable by service name. In both cases newly configured LLM/MCP
listeners bind host ports directly, and the unauthenticated gateway admin
listener is not widened to `0.0.0.0`.

AgentGuard server and console remain separate Compose services. AgentsharkX has
no dependency condition on either upstream, so it can start degraded and
explain recovery. Missing or placeholder AgentsharkX/AgentGuard credentials
still fail validation before the HTTP listener opens.

The portable fallback keeps the original fully containerized topology and pins
agentgateway by tag plus image digest. In that mode the Compose wrapper resolves
the config file owner UID/GID and runs only the gateway container as that
non-root identity; extra business listeners require explicit published ports.

The release E2E runs contract-shaped upstream fixtures as separate processes
and exercises the actual BFF session, Connect probe, Audit poll/SSE path, and
AgentGuard approval mutation. It does not promote fixture data to compatibility
evidence; pinned upstream samples and smoke checks remain authoritative.

## Phase 0 deployment decisions

- agentgateway has both a host-native release with verified per-platform
  checksums and a container fallback pinned by tag plus digest.
- AgentGuard publishes no official prebuilt container image; Compose builds the
  server and console from the verified current-main revision and assigns a
  revision-qualified local image name.
- No submodules and no upstream source are committed here.
- agentgateway needs explicit wildcard management bindings inside its container
  fallback; the native default keeps management listeners on host loopback.
- The pinned AgentGuard main snapshot still needs the corrected Compose
  healthcheck path. Details and evidence are in
  [upstream compatibility](upstream-compatibility.md).
