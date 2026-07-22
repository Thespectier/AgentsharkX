# Architecture

Status: Phase 6 Audit and live-data integration, verified 2026-07-22.

## Context

AgentsharkX is a management-plane application above two independent upstream
processes. It presents a shared console while preserving the ownership and
semantics of every upstream capability.

```text
Browser ──HTTPS──> AgentsharkX Web + Go BFF
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

| Component | Owns | Does not own |
|---|---|---|
| agentgateway | LLM/MCP/A2A/HTTP proxying, routes, providers, policies, guardrails, request logs, cost and latency telemetry | AgentGuard runtime identities, reviews, or rules |
| AgentGuard | Runtime interception, resources, labels, runtime rules, approvals, traffic, sessions, and security audit | Gateway routing or transport policy |
| AgentsharkX | Console navigation, admin authentication, source adapters, capability detection, normalization, aggregation, SSE, and high-frequency workflows | Proxying, task inference, a rules engine, replay, or durable event storage |

## BFF boundaries

The Go BFF is organized into the following packages:

- `config`: validated environment configuration with secret-safe diagnostics.
- `auth`: one-admin session, strict cookie, CSRF, and write protection.
- `gateway`: agentgateway management adapter.
- `connect`: filtering, cursor pagination, details, setup verification, and
  native-console links over sanitized gateway resources.
- `guard`: AgentGuard management adapter.
- `trust`: explicit AgentGuard identity/resource aggregation, filtering,
  pagination, label writes, and bounded scan-job orchestration.
- `protect`: independent gateway policy and AgentGuard rule/plugin aggregation,
  short-lived rule-check tokens, guarded mutations, approval pagination, and
  duplicate-operation locks.
- `audit`: independent polling, source-scoped failures, redacted normalization,
  exact-ID session counts, metrics, trends, and a bounded activity snapshot.
- `aggregate`: source-preserving view models and partial-result handling.
- `stream`: 1000-record in-memory ring, source/ID dedupe, monotonic SSE
  sequence IDs, replay after `Last-Event-ID`, and non-blocking fan-out.
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

The five primary views are Home, Connect, Trust, Protect, and Audit. System is
a supporting diagnostics page, not another product capability. URL search
state preserves demo failure scenarios and Audit event selection so a detail
drawer can be restored after refresh.

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

## Phase 7 runtime data and storage

The BFF has no database. Background monitors poll the two health contracts and,
every two seconds by default, the verified Audit read contracts. New normalized
events enter a 1000-record ring and are published with a monotonic SSE sequence.
Reconnecting clients send `Last-Event-ID` and receive only newer retained
records. Both the ring and browser list dedupe by normalized source/event ID.
Connect reads a bounded `/api/config` snapshot per request and never returns raw
config, params, policy bodies, credentials, or prompt payloads. Protect may
summarize explicit route/backend policy keys and raw config paths, while policy
editing stays in agentgateway. Trust reads the four
AgentGuard session/resource routes independently, so one failed capability does
not erase successful siblings. It whitelists display fields and never returns
session keys, client URLs, arbitrary principal/metadata objects, descriptors,
file contents, MCP URLs, detector metadata, reasons, or LLM configuration.

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

Protect reads AgentGuard rules and pending approvals, then fans out plugin
config/available reads only for explicit Trust agent IDs with fixed agent and
concurrency bounds. Gateway policy failures remain independent from AgentGuard
rule/plugin failures. Rule source is submitted only to AgentGuard check/publish
and is never returned by list APIs or logged. A publish token stores only the
source digest, expires after five minutes, is consumed once, and is held in a
100-entry in-memory bound. Dangerous writes require a non-empty operator note
and explicit confirmation; approval/rule locks prevent concurrent duplicate
actions in one BFF process. Upstream mutations are not automatically retried.

The Audit poller requests agentgateway logs with
`includeAttributes=false` and never requests payload detail. AgentGuard runtime
state, tool arguments/results, plugin results, and free-form reasons are not
decoded into the public model. Detail responses are built from an allow-listed
redacted projection; list, overview, and SSE events omit that projection.

`/overview` is `mode=operational` when the Audit service is attached. Gateway
log/Analytics failures and AgentGuard Traffic/Audit/Sessions failures are
reported independently, so available peer data remains visible. AgentGuard
session event/deny counts use exact session-ID equality. Cross-source
correlation is marked verified only when both sources explicitly return the
same non-empty trace or session identifier; timestamps are never used.

No Audit state is durable. SSE resume covers only the retained ring, and
long-term logs remain in their upstream systems. AgentsharkX still provides no
task model, DAG, payload vault, replay engine, or traffic collector.

## Security baseline

- `AGENTSHARK_ADMIN_TOKEN` is mandatory outside explicitly loopback-only
  development mode.
- Successful login creates an `HttpOnly`, `SameSite=Strict`, `Secure` session
  cookie; write requests also require a CSRF token.
- A non-Secure cookie or disabled authentication is accepted only when both the
  environment is explicitly local/development and the listener is loopback.
- Upstream keys are server-only and never appear in a frontend bundle, API
  response, structured log, or error message.
- Full prompts, rule source, operator notes, approval arguments, authorization
  headers, and raw sensitive payloads are denied by
  default. Raw event views use a redacted copy.
- The Phase 0 Compose baseline publishes upstream management ports on loopback.
  It is a development topology, not an internet-facing deployment.

## Phase 7 deployment boundary

The production Dockerfile has independent pinned Node and Go build stages. It
builds Web with `VITE_ENABLE_MOCKS=false`, replaces the development placeholder
assets before compiling, and embeds the resulting SPA into the Go binary. The
runtime stage contains only Alpine CA certificates and the static binary, runs
as UID/GID `65532:65532`, and exposes one same-origin port. `/healthz` is public
and reports process readiness only; authenticated `/system` diagnostics remain
the authority for independent upstream state.

Compose builds AgentGuard from its immutable revision, pulls agentgateway by
tag plus digest, and builds AgentsharkX locally. AgentsharkX has no dependency
condition on either upstream so it can start degraded and explain recovery.
Every published port remains loopback in the example environment. Missing or
placeholder AgentsharkX/AgentGuard credentials cause configuration validation
to fail before the HTTP listener opens.

The release E2E runs contract-shaped upstream fixtures as separate processes
and exercises the actual BFF session, Connect probe, Audit poll/SSE path, and
AgentGuard approval mutation. It does not promote fixture data to compatibility
evidence; pinned upstream samples and smoke checks remain authoritative.

## Phase 0 deployment decisions

- agentgateway is pulled by tag plus digest.
- AgentGuard publishes a release tag but no official prebuilt container image;
  Compose builds directly from that release's full Git revision and assigns a
  local image name.
- No submodules and no upstream source are committed here.
- agentgateway needs explicit wildcard management bindings inside a container.
- AgentGuard `v2.1` needs a corrected Compose healthcheck path. Details and
  evidence are in [upstream compatibility](upstream-compatibility.md).
