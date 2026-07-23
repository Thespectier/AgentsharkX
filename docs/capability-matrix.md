# Capability matrix

Verified against agentgateway `v1.3.1` and AgentGuard main revision
`4b755fb4a4a2763b7e817b3d0220fe5c22187b59` (package `2.1`) on 2026-07-23.

Phase 7 adds reproducible deployment, session/CSRF reload recovery,
source-specific diagnostics, release E2E, and supply-chain gates above the
source-preserving Connect, Trust, Protect, and Audit integrations. Mock
fixtures remain UI evidence only and do not upgrade an upstream capability
status.

## Status vocabulary

| Status | Meaning |
|---|---|
| `supported` | A stable upstream route was invoked successfully or its write contract is present in the pinned upstream OpenAPI and implementation. |
| `partial` | The source exposes some required facts, but configuration, derivation from explicit fields, or a missing dedicated endpoint limits the experience. |
| `link-out` | The upstream console owns the safe or complete workflow; AgentsharkX should deep-link instead of duplicating it. |
| `unavailable` | The pinned upstream has no verified management-plane capability for this feature. |

`runtime` evidence means a real request was made to a running pinned upstream
artifact (verified native binary or immutable container).
`schema` means the route and payload were confirmed in the pinned source or
generated OpenAPI but a mutating request was intentionally not executed.

## System and Connect

| AgentsharkX capability | Source | Status | Evidence | Adapter rule |
|---|---|---|---|---|
| Readiness | agentgateway | supported | runtime `GET :15021/healthz/ready` | Treat only HTTP 200 plus `ready` as healthy. |
| Version/build | agentgateway | supported | runtime `GET /api/runtime` | Preserve version and Git revision; do not infer API presence from version. |
| Capability/config discovery | agentgateway | supported | runtime `GET /api/config`, `GET /config_dump` | Probe route availability and required fields independently. |
| Providers and models | agentgateway | partial | Phase 3 adapter over runtime `/api/config`; no dedicated read API | Normalize explicit `llm` config only; preserve direct/virtual kind, source, fetched time, and raw reference. |
| MCP servers | agentgateway | partial | Phase 3 adapter over runtime `/api/config`; no dedicated read API | Normalize top-level and inline explicit MCP targets only and preserve transport and scope. |
| Listeners, routes, backends | agentgateway | partial | Phase 3 adapter over runtime `/api/config` | Apply only verified HTTP defaults; provide filter, cursor pagination, and detail without claiming backend health. |
| Cost catalog | agentgateway | supported | runtime `GET /api/costs/models` | `loaded:false` is an empty state, not fabricated providers. |
| Request logs | agentgateway | supported | runtime `POST /api/logs/search` returned 200 from the bundled SQLite store and returned the real LLM request | Probe the redacted search contract; surface an explicit unavailable status if an external deployment omits storage. |
| Analytics | agentgateway | supported | runtime bounded `POST /api/logs/analytics/summary` returned the real request/token aggregate | Sum non-overlapping returned buckets; return explicit `unavailable` and null metrics when storage is missing. |
| Metrics | agentgateway | supported | runtime `GET :15020/metrics` | Metrics are diagnostics, not a substitute for request-log records. |
| Raw config editor/save | agentgateway | link-out | pinned UI `/raw-config`; live unchanged `POST /api/config` returned 200 from the host-native process | Keep editing upstream-owned; the default process runs as the checkout user against the explicit config file, while the container fallback aligns its non-root UID/GID. |
| CEL editor/evaluator | agentgateway | link-out | pinned UI `/cel`; evaluation API remains upstream-owned | BFF creates a validated deep link only. |
| Playground | agentgateway | link-out | pinned UI `/llm/playground`, `/mcp/playground` | Never send provider keys through AgentsharkX frontend. |
| Admin API authentication | agentgateway | unavailable | pinned admin routes have no native auth middleware | Keep the admin listener private; BFF supplies browser authentication isolation. |
| Source-specific diagnostics | AgentsharkX | supported | Phase 7 aggregate/API/UI tests for each disconnected source | Derive advice only from independent health probes; never return configured URLs, tokens, or raw responses. |
| Process/container readiness | AgentsharkX | supported | Phase 7 `/healthz`, embedded SPA, and image healthcheck verification | Readiness means the process serves; upstream degradation remains visible in System. |

The live registry uses `gateway.runtime`, `gateway.configuration`,
`gateway.cost-catalog`, `gateway.request-logs`, and `gateway.admin-auth` IDs.
`gateway.request-logs` is now determined by a live redacted search probe rather
than a static `partial` label. Phase 6 sends the verified log-search body with
`includeAttributes=false`; a missing database in an external deployment becomes
a source-scoped Audit failure rather than an empty traffic claim.

## Trust and AgentGuard resources

| AgentsharkX capability | Source | Status | Evidence | Adapter rule |
|---|---|---|---|---|
| Health/version | AgentGuard | supported | runtime `GET /v1/backend/health` | Requires `X-Api-Key`; report both release pin and returned service version. |
| Agent list | AgentGuard | partial | Phase 4 aggregation tests; no dedicated upstream `/agents` route | Build only from explicit resource/session `agent_id` or `owner_agent_id`; keep absent identity fields unknown. |
| Sessions | AgentGuard | supported | Phase 4 disposable runtime plus contract tests for `GET /v1/backend/sessions` | Preserve explicit session/agent IDs; omit keys, client URLs, principal, and arbitrary metadata. |
| Tools | AgentGuard | supported | Phase 4 disposable runtime plus contract tests for `GET /v1/backend/tools` | Preserve owner, name, labels, and raw reference; never infer tools from gateway traffic. |
| Skills | AgentGuard | supported | Phase 4 pinned-source contract tests for global/agent routes | Preserve `skill_unique_id` and safe detector summary; omit descriptor/code/path data. |
| MCP resources | AgentGuard | supported | Phase 4 pinned-source contract tests for global/agent routes | Keep distinct from agentgateway MCP targets and omit upstream URL/descriptor data. |
| Tool label update | AgentGuard | supported | Phase 4 disposable runtime and adapter tests for `PATCH .../labels` | UI marks the mutation pending optimistically, then replaces it with the exact server response; no retry. |
| Skill detection | AgentGuard | supported | Phase 4 disposable runtime and adapter tests for `POST .../skills/detect` | BFF wraps the synchronous call in a bounded job; poll actual state without synthetic percentage progress. |
| MCP detection | AgentGuard | supported | Phase 4 adapter tests for `POST .../mcps/detect` | Same bounded job contract; expose safe result fields and recoverable errors only. |
| Remote attestation | AgentGuard | unavailable | no verified route or field | Do not use cryptographic or remote-attestation claims in UI copy. |
| Native configuration console | AgentGuard | link-out | configured `AGENTGUARD_CONSOLE_URL`; no new upstream mutation contract | Expose the validated native-console URL from Protect for settings not covered by verified AgentsharkX writes. |
| Thought-Aligner plugin | AgentGuard | link-out | pinned main source, example config, and Compose environment render check | The code and dedicated server-only environment inputs are present but opt-in and upstream-owned; configure it through AgentGuard, never infer an AgentsharkX mutation API. |

The AgentGuard registry probes the verified global routes independently and
publishes `guard.health`, `guard.sessions`, `guard.tools`, `guard.skills`,
`guard.mcps`, `guard.rules`, `guard.traffic`, `guard.audit`, `guard.approvals`,
and `guard.auditors`. Sessions/tools/skills/MCPs feed Trust; rules, plugins, and
approvals feed Protect. Traffic and audit now feed Phase 6 Audit; auditors remain
probe-only because this phase does not add an auditor-management surface.

## Protect

| AgentsharkX capability | Source | Status | Evidence | Adapter rule |
|---|---|---|---|---|
| Gateway policies | agentgateway | partial | Phase 5 adapter tests over route/backend `policies` keys | Return names and raw config paths only; never return policy bodies; advanced editing links out. |
| Content guardrails | agentgateway | partial | Phase 5 adapter tests for explicit `ai`/`llm` guardrail keys | Preserve request/response placement only when explicit; otherwise phase remains unknown. |
| Runtime rules list/check | AgentGuard | supported | pinned-source contract plus Phase 5 adapter/BFF integration tests | Omit rule source/prompt; a successful single-rule check issues a short-lived source-bound token. |
| Runtime rule publish/delete | AgentGuard | supported | Phase 5 fake-upstream success tests and non-retry transport tests | Require note, confirmation, CSRF, current check token for publish, mutation lock, request ID, and receipt. |
| Plugins | AgentGuard | partial | pinned per-agent available/config contracts; no global read route | Discover only explicit Trust agents with bounded fan-out; show plugin names and phase, never parameters. |
| Approval queue | AgentGuard | supported | Phase 5 sanitized contract and fake-upstream BFF test | Pending tickets omit tool arguments, target, obligations, and sensitive event bodies. Empty is valid. |
| Approve/deny | AgentGuard | supported | Phase 5 E2E and BFF tests for success, 404, timeout, and manual retry | Require note/confirmation; disable duplicates; never auto-retry; return request-ID receipt. |
| Unified policy DSL | both | unavailable | sources use different policy models | Group by source; never translate into a fake common DSL. |

## Audit

| AgentsharkX capability | Source | Status | Evidence | Adapter rule |
|---|---|---|---|---|
| Gateway traffic detail | agentgateway | supported | Phase 6 adapter tests plus bundled SQLite runtime request | Never request attributes or payload; return only allow-listed redacted detail. |
| Gateway analytics | agentgateway | supported | Phase 6 adapter tests plus bundled SQLite runtime aggregate | Surface explicit capability failure while peer-source data remains available. |
| AgentGuard traffic | AgentGuard | supported | Phase 6 contract tests for `GET /v1/backend/traffic?n=500` | Use scalar action/latency/risk fields for metrics only; the route has no event ID, so do not synthesize an event. |
| AgentGuard recent audit | AgentGuard | supported | Phase 6 redaction and BFF integration tests for `GET /v1/backend/audit/recent?n=500` | Preserve `event_id`, phase, action, subject, and safe tool identity; omit runtime state, args/results, plugins, and reason. |
| AgentGuard sessions in Audit | AgentGuard | supported | Phase 6 exact-ID count tests plus verified sessions contract | Preserve session/agent IDs; count events and denies only by exact AgentGuard session ID. |
| AgentGuard auditors | AgentGuard | supported | runtime `GET /v1/backend/auditors` | Display registered names/descriptions only. |
| Unified activity view | AgentsharkX | supported | Phase 6 BFF/API/UI tests plus 5000-event bounded-buffer tests | Side-by-side source-preserving view, not a task timeline; list/SSE omit raw detail. |
| SSE resume and dedupe | AgentsharkX | supported | Phase 6 ring and `Last-Event-ID` replay tests | Keep 1000 records, assign monotonic stream sequences, and dedupe source/event IDs on server and browser. |
| Verified cross-source correlation | both | partial | Phase 6 exact shared-ID and negative no-ID tests | Default is uncorrelated; time windows are prohibited. |
| Task graph | neither | unavailable | outside product boundary | Must not be implemented. |
| Replay/payload vault | neither | unavailable | outside product boundary | Must not be implemented. |

## Release evidence

| Gate | Status | Evidence |
|---|---|---|
| Go and Web | supported | `make verify`, race tests, generated-client check, browser suite |
| Contract | supported | OpenAPI `0.7.0-preview`, upstream samples, Compose render check |
| Full path | supported | real BFF fixture E2E: login → Connect → emit → Audit → approve |
| Native gateway | supported | pinned binary version/revision and per-platform SHA-256 verification; Linux integrated-preview smoke |
| Container | supported | pinned multi-stage build, embedded production Web, non-root runtime, healthcheck |
| Secret boundary | supported | tracked-file patterns plus production browser-bundle sentinel scan |
| Supply chain | supported | SPDX 2.3 SBOM, lockfile license inventory, Go vet and npm production audit record |
