# Architecture

Status: Phase 1 reviewable console, verified 2026-07-21.

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

The Go BFF will be organized into the following packages in Phase 2:

- `config`: validated environment configuration with secret-safe diagnostics.
- `auth`: one-admin session, strict cookie, CSRF, and write protection.
- `gateway`: agentgateway management adapter.
- `guard`: AgentGuard management adapter.
- `aggregate`: source-preserving view models and partial-result handling.
- `stream`: bounded polling, deduplication, ring buffers, heartbeat, and SSE
  resume.
- `api`: OpenAPI-backed HTTP handlers and standard errors.
- `web`: embedded frontend assets in production.

Adapters expose upstream facts. Aggregation may reduce display differences but
must retain `source`, upstream object ID, original-detail reference, scope, and
phase. A correlation flag is allowed only when both sources provide the same
identifier and the adapter explicitly verifies its meaning.

## Phase 1 frontend boundary

The React/Vite console is independently reviewable before the BFF exists. Its
TanStack Router paths and TanStack Query requests target only the
AgentsharkX-owned paths in `api/openapi.yaml`. MSW intercepts those paths in the
browser and supplies source-labelled REST envelopes plus a bounded Mock SSE
stream. No frontend module imports upstream code or receives an upstream
credential.

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

## Runtime data and storage

The BFF has no database. Pollers maintain a maximum of 500 normalized events
per source and deduplicate by `source + upstream ID`. Restarting the process may
discard this state. Payloads are redacted before they enter a ring buffer or
browser response. Long-term logs remain in their upstream systems.

## Security baseline

- `AGENTSHARK_ADMIN_TOKEN` is mandatory outside explicitly loopback-only
  development mode.
- Successful login creates an `HttpOnly`, `SameSite=Strict`, `Secure` session
  cookie; write requests also require a CSRF token.
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
