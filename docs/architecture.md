# Architecture

Status: Phase 4 Trust integration, verified 2026-07-22.

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
- `aggregate`: source-preserving view models and partial-result handling.
- `stream`: non-blocking Phase 2 health-event fan-out; bounded business event
  buffers and resume semantics remain Phase 6 work.
- `api`: OpenAPI-backed HTTP handlers and standard errors.
- `model`: source-preserving health, capability, Connect/Trust resource,
  overview, event, and error contracts.
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
strict session and keeps the CSRF token only in module memory. No frontend
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

## Phase 4 runtime data and storage

The BFF has no database. A background monitor polls only the two health
contracts and publishes a normalized SSE event when status or version changes.
New SSE connections first receive the current source-scoped health snapshot.
Connect reads a bounded `/api/config` snapshot per request and never returns raw
config, params, policies, credentials, or prompt payloads. Trust reads the four
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

The background monitor still polls only health. `/overview` remains
`mode=health-only`, and no business-event ring buffer, traffic correlation, or
durable storage is added. Long-term logs remain in their upstream systems.

## Security baseline

- `AGENTSHARK_ADMIN_TOKEN` is mandatory outside explicitly loopback-only
  development mode.
- Successful login creates an `HttpOnly`, `SameSite=Strict`, `Secure` session
  cookie; write requests also require a CSRF token.
- A non-Secure cookie or disabled authentication is accepted only when both the
  environment is explicitly local/development and the listener is loopback.
- Upstream keys are server-only and never appear in a frontend bundle, API
  response, structured log, or error message.
- Full prompts, authorization headers, and raw sensitive payloads are denied by
  default. Raw event views use a redacted copy.
- The Phase 0 Compose baseline publishes upstream management ports on loopback.
  It is a development topology, not an internet-facing deployment.

## Phase 0 deployment decisions

- agentgateway is pulled by tag plus digest.
- AgentGuard publishes a release tag but no official prebuilt container image;
  Compose builds directly from that release's full Git revision and assigns a
  local image name.
- No submodules and no upstream source are committed here.
- agentgateway needs explicit wildcard management bindings inside a container.
- AgentGuard `v2.1` needs a corrected Compose healthcheck path. Details and
  evidence are in [upstream compatibility](upstream-compatibility.md).
