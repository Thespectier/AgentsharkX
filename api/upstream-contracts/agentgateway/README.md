# agentgateway v1.3.1 contract notes

Container:
`cr.agentgateway.dev/agentgateway:v1.3.1@sha256:c3ce7b75da90fef70239befcc1c3adc05152d7b9dd21fcb8351178026a2c4381`.

The container was started with an empty static configuration plus
`ADMIN_ADDR=0.0.0.0:15000` and `STATS_ADDR=0.0.0.0:15020`. This verifies the
management surface without configuring or sending business traffic.

| Sample                                 | Request                                  | Result                                                                    |
| -------------------------------------- | ---------------------------------------- | ------------------------------------------------------------------------- |
| `readiness.response.txt`               | `GET :15021/healthz/ready`               | 200                                                                       |
| `runtime.response.json`                | `GET :15000/api/runtime`                 | 200                                                                       |
| `config.response.json`                 | `GET :15000/api/config`                  | 200                                                                       |
| `config-populated.response.json`       | `GET :15000/api/config`                  | Sanitized populated shape from the pinned upstream UI fixture and runtime |
| `config-write.response.json`           | `POST :15000/api/config`                 | 200, sanitized success response from the pinned file-backed writer        |
| `config-dump.response.json`            | `GET :15000/config_dump`                 | 200, selected stable top-level fields                                     |
| `cost-models.response.json`            | `GET :15000/api/costs/models`            | 200                                                                       |
| `logs-unconfigured.response.json`      | `POST :15000/api/logs/search`            | 500, no request-log DB                                                    |
| `logs-populated.response.json`         | `POST :15000/api/logs/search`            | Sanitized populated shape from the pinned log-store source contract       |
| `log-detail.response.json`             | `POST :15000/api/logs/get`               | Synthetic complete shape verified from the pinned source and UI contract  |
| `analytics-unconfigured.response.json` | `POST :15000/api/logs/analytics/summary` | 500, no request-log DB                                                    |
| `metrics.sample.txt`                   | `GET :15020/metrics`                     | 200, truncated non-sensitive sample                                       |

Provider/model/MCP/route summaries must be derived only from explicit
config/config-dump fields. No dedicated resource-list API was found in this
pinned standalone management surface.

The populated shape and native console routes were cross-checked against
agentgateway tag `v1.3.1`, commit
`dbaaf7ed73671e7aec9195e35e7f726c0b14b84a`. Sensitive `params`, policy bodies,
and API-key fixture values are intentionally excluded from the frozen sample.
The `provider.custom` object discriminator was additionally verified from the
running pinned `/api/config` contract on 2026-07-27; only its non-sensitive
format names are retained.
The exact pinned UI/source confirms that configuration writes submit the whole
document, are accepted only for a file-backed source, are validated before the
active YAML file is replaced, and provide no ETag or conditional-write header.
Phase 8 therefore exposes typed provider/direct-model main-form fields without
accepting raw configuration. Typed write-only credential inputs match the
pinned literal API-key, AWS static, GCP credential-file, and Azure managed
identity shapes; no credential value is returned by the public read contract.
AgentsharkX may use that verified whole-document write to delete one Provider
and its directly referenced Models together after explicit confirmation. It
does not edit Virtual Models: any Virtual Model reference to an affected direct
Model blocks the deletion.
The Phase 9 MCP contract was checked against the same exact pinned source:
`schema/config.json` definitions `LocalSimpleMcpConfig`, `LocalMcpTarget`,
`McpStatefulMode`, `McpPrefixMode`, and `McpBackendFailureMode`, together with
`ui/src/pages/McpServers.tsx` and `ui/src/types.ts`. The verified main editor
manages global port/session/prefix/failure settings and top-level Streamable
HTTP, SSE, and stdio targets. Network targets support URL, host/port/path, or
backend/path shapes; stdio supports `cmd`, `args`, `env`, and `clear_env`.
OpenAPI targets and MCP policy bodies remain advanced/read-only. Phase 9 uses
the verified whole-document write, preserves those advanced/unowned fields,
shares the LLM mutation lock, and verifies the requested result by refetching.
The Phase 6 log and Analytics adapters send the same exact rolling 60-minute
`timeRange`. Search always sends `includeAttributes=false`; Analytics requests
`bucketSeconds=300`. Search rejects unexpected attributes or payload fields so
polling, lists, trends, and SSE remain bounded summaries. When an authenticated
administrator opens one event, the BFF calls `/api/logs/get` with the exact
`{ id, includePayload: true }` body and returns the complete source record,
including arbitrary attributes and payload values. The pinned UI route
`/ui/llm/logs` accepts the same exact upstream log ID in the `log` query
parameter.
