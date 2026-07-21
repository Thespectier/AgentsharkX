# agentgateway v1.3.1 contract notes

Container:
`cr.agentgateway.dev/agentgateway:v1.3.1@sha256:c3ce7b75da90fef70239befcc1c3adc05152d7b9dd21fcb8351178026a2c4381`.

The container was started with an empty static configuration plus
`ADMIN_ADDR=0.0.0.0:15000` and `STATS_ADDR=0.0.0.0:15020`. This verifies the
management surface without configuring or sending business traffic.

| Sample | Request | Result |
|---|---|---|
| `readiness.response.txt` | `GET :15021/healthz/ready` | 200 |
| `runtime.response.json` | `GET :15000/api/runtime` | 200 |
| `config.response.json` | `GET :15000/api/config` | 200 |
| `config-dump.response.json` | `GET :15000/config_dump` | 200, selected stable top-level fields |
| `cost-models.response.json` | `GET :15000/api/costs/models` | 200 |
| `logs-unconfigured.response.json` | `POST :15000/api/logs/search` | 500, no request-log DB |
| `analytics-unconfigured.response.json` | `POST :15000/api/logs/analytics/summary` | 500, no request-log DB |
| `metrics.sample.txt` | `GET :15020/metrics` | 200, truncated non-sensitive sample |

Provider/model/MCP/route summaries must be derived only from explicit
config/config-dump fields. No dedicated resource-list API was found in this
pinned standalone management surface.
