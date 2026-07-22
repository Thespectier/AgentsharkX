# Capability matrix

Verified against agentgateway `v1.3.1` and AgentGuard `v2.1` on 2026-07-22.

Phase 4 adds the Trust adapter above the existing Connect integration. It reads
verified AgentGuard resource contracts independently, exposes source-preserving
Agent and resource views, and implements guarded label/detection mutations.
Mock fixtures remain UI evidence only and do not upgrade an upstream capability
status.

## Status vocabulary

| Status | Meaning |
|---|---|
| `supported` | A stable upstream route was invoked successfully or its write contract is present in the pinned upstream OpenAPI and implementation. |
| `partial` | The source exposes some required facts, but configuration, derivation from explicit fields, or a missing dedicated endpoint limits the experience. |
| `link-out` | The upstream console owns the safe or complete workflow; AgentsharkX should deep-link instead of duplicating it. |
| `unavailable` | The pinned upstream has no verified management-plane capability for this feature. |

`runtime` evidence means a real request was made to a running pinned container.
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
| Request logs | agentgateway | partial | runtime `POST /api/logs/search`; 500 without request-log DB | Capability probe must surface `request log database is not configured`. |
| Analytics | agentgateway | partial | Phase 3 bounded `POST /api/logs/analytics/summary` with `bucketCount=12`; same DB dependency | Sum non-overlapping returned buckets; return explicit `unavailable` and null metrics when storage is missing. |
| Metrics | agentgateway | supported | runtime `GET :15020/metrics` | Metrics are diagnostics, not a substitute for request-log records. |
| Raw config editor | agentgateway | link-out | pinned UI `/raw-config` below its configured base path | Do not reproduce the editor in Phase 3. |
| CEL editor/evaluator | agentgateway | link-out | pinned UI `/cel`; evaluation API remains upstream-owned | BFF creates a validated deep link only. |
| Playground | agentgateway | link-out | pinned UI `/llm/playground`, `/mcp/playground` | Never send provider keys through AgentsharkX frontend. |
| Admin API authentication | agentgateway | unavailable | pinned admin routes have no native auth middleware | Keep the admin listener private; BFF supplies browser authentication isolation. |

The live registry uses `gateway.runtime`, `gateway.configuration`,
`gateway.cost-catalog`, `gateway.request-logs`, and `gateway.admin-auth` IDs.
`gateway.request-logs` remains `partial` from the verified database dependency;
Phase 3 still does not issue a log-search request; only the separately verified
analytics summary body is sent.

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

The AgentGuard registry probes the verified global routes independently and
publishes `guard.health`, `guard.sessions`, `guard.tools`, `guard.skills`,
`guard.mcps`, `guard.rules`, `guard.traffic`, `guard.audit`, `guard.approvals`,
and `guard.auditors`. Phase 4 promotes only sessions/tools/skills/MCPs into the
Trust view. Rules, approvals, traffic, audit, and auditors remain probe-only
until their owning integration phases.

## Protect

| AgentsharkX capability | Source | Status | Evidence | Adapter rule |
|---|---|---|---|---|
| Gateway policies | agentgateway | partial | config/config-dump | Read-only source-grouped summary; advanced editing links out. |
| Content guardrails | agentgateway | partial | config/config-dump | Preserve prompt/response scope; advanced editing links out. |
| Runtime rules list/check | AgentGuard | supported | runtime list; schema check | Rule source and check diagnostics remain AgentGuard-shaped. |
| Runtime rule generate/publish/delete | AgentGuard | supported | pinned schema and handlers | Phase 5 requires check, confirm, request ID, and mutation lock. |
| Plugins | AgentGuard | partial | agent available/config routes in schema; no global read route | Show only explicit per-agent phase configuration. |
| Approval queue | AgentGuard | supported | runtime `GET /v1/backend/approvals` | Empty array is a valid queue. |
| Approve/deny | AgentGuard | supported | pinned schema and handlers | Note is required by AgentsharkX even though upstream accepts an empty string. |
| Unified policy DSL | both | unavailable | sources use different policy models | Group by source; never translate into a fake common DSL. |

## Audit

| AgentsharkX capability | Source | Status | Evidence | Adapter rule |
|---|---|---|---|---|
| Gateway traffic detail | agentgateway | partial | request-log API requires configured DB | Include payload only after BFF redaction and explicit request. |
| Gateway analytics | agentgateway | partial | analytics API requires configured DB | Keep last known data with stale metadata during failures. |
| AgentGuard traffic | AgentGuard | supported | runtime `GET /v1/backend/traffic` | Preserve decision/action and upstream event ID. |
| AgentGuard recent audit | AgentGuard | supported | runtime `GET /v1/backend/audit/recent` | Do not promote an audit record into gateway traffic. |
| AgentGuard auditors | AgentGuard | supported | runtime `GET /v1/backend/auditors` | Display registered names/descriptions only. |
| Unified activity view | AgentsharkX | partial | normalization is planned for Phase 6 | Side-by-side source-preserving view, not a task timeline. |
| Verified cross-source correlation | both | partial | conditional on identical verified IDs | Default is uncorrelated; time windows are prohibited. |
| Task graph | neither | unavailable | outside product boundary | Must not be implemented. |
| Replay/payload vault | neither | unavailable | outside product boundary | Must not be implemented. |
