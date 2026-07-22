# AgentGuard v2.1 contract notes

The image was built from release tag `v2.1` at the pinned revision
`6f95deb9f405eca41efb6cc58ccee5b1791c7b03`. Requests used
`X-Api-Key: <redacted>`.

| Sample | Request | Result |
|---|---|---|
| `health.response.json` | `GET /v1/backend/health` | 200 |
| `stats.response.json` | `GET /v1/backend/stats` | 200 |
| `sessions.response.json` | `GET /v1/backend/sessions` | 200 |
| `tools.response.json` | `GET /v1/backend/tools` | 200 |
| `skills.response.json` | `GET /v1/backend/skills` | 200 |
| `mcps.response.json` | `GET /v1/backend/mcps` | 200 |
| `rules.response.json` | `GET /v1/backend/rules` | 200 |
| `traffic.response.json` | `GET /v1/backend/traffic` | 200 |
| `audit-recent.response.json` | `GET /v1/backend/audit/recent` | 200 |
| `approvals.response.json` | `GET /v1/backend/approvals` | 200 |
| `auditors.response.json` | `GET /v1/backend/auditors` | 200 |
| `unauthorized.response.json` | health without API key | 401 |
| `openapi-summary.response.json` | `GET /openapi.json`, summarized | 200 |
| `trust-populated.response.json` | Sessions/Tools/Skills/MCP populated shapes | Sanitized shape from pinned source tests |
| `tool-labels.response.json` | `PATCH .../tools/{tool_name}/labels` | Sanitized request and confirmed response shape |
| `skill-detect.response.json` | `POST .../skills/detect` | Sanitized request and detector result shape |
| `mcp-detect.response.json` | `POST .../mcps/detect` | Sanitized request and detector result shape |

`uptime_s` in the health/stats samples is capture-specific. Runtime OpenAPI
reports service version `0.3.0`, which differs from package/release version
`2.1`; both facts must remain visible in compatibility diagnostics.

Phase 4 populated and mutation shapes were cross-checked against the exact
`v2.1` revision and its upstream HTTP tests. AgentsharkX intentionally excludes
session keys, client URLs, arbitrary principal/metadata objects, descriptors,
file contents, detector metadata, MCP URLs, and LLM configuration from its
normalized responses.
