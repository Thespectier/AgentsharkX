# AgentsharkX deterministic demo agent

This package provides the Phase 16A demo workflow, private Runner, and fixed LLM/MCP
fixtures. It is intentionally narrow: `happy`, `approval`, and `failure` are the only
scenarios, and `delayMs` is limited to `0..2000` milliseconds.

Every target, prompt, fixture response, and workflow edge is fixed in source. The
`send_http` tool returns a simulated receipt and never opens a network connection. The
failure scenario raises a fixture-owned MCP timeout, records one error span, and finishes
the Task Root with `outcome=degraded`.

## Local development

Use the checked-in lock file and Python 3.11 through 3.14:

```bash
uv sync --project examples/demo-agent --frozen
uv run --project examples/demo-agent pytest examples/demo-agent/tests
uv run --project examples/demo-agent ruff check --config examples/demo-agent/pyproject.toml examples/demo-agent
uv run --project examples/demo-agent mypy --config-file examples/demo-agent/pyproject.toml
```

Start the combined fixture service with:

```bash
uv run --project examples/demo-agent agentshark-demo-fixtures
```

The fixture exposes `GET /healthz`, `GET /v1/models`, `POST /v1/chat/completions`, and
Streamable HTTP MCP at `/mcp`. The LLM client is expected to reach it through the
dedicated agentgateway Demo route; it does not bypass agentgateway.

With agentgateway, AgentGuard, the collector, and the fixtures running, execute one smoke
scenario:

```bash
uv run --project examples/demo-agent agentshark-demo-run --scenario happy --delay-ms 0
```

The CLI accepts no prompt, URL, command, model override, or target override.

## Runner

`agentshark-demo-runner` listens on the configured internal address and exposes:

```text
GET  /healthz
POST /internal/v1/runs
GET  /internal/v1/runs/{runId}
POST /internal/v1/runs/{runId}/cancel
```

All `/internal/v1/*` requests require
`Authorization: Bearer $AGENTSHARK_DEMO_RUNNER_TOKEN`; the token must contain at least 32
bytes. The Runner has a fixed concurrency of one, treats an identical `runId` request as
idempotent, rejects changed reuse of that ID, and retains only bounded in-memory status.
The BFF remains the durable owner of Demo runs.

AgentGuard owns the Human Check wait. While the `approval` tool is blocked waiting for an
operator, the Runner remains `running` with `currentStep=guarded_action`. The BFF derives
`waiting_approval` only by finding a pending AgentGuard ticket with the exact Run
`sessionId`; timestamps or proximity are never used for correlation. Approve resumes the
simulated action, while Deny returns `outcome=denied` without executing it.

Required production settings are supplied by the deployment layer:

```text
AGENTSHARK_DEMO_RUNNER_TOKEN
AGENTSHARK_TRACE_ENDPOINT
AGENTSHARK_TRACE_INGEST_TOKEN
AGENTGUARD_SERVER_URL
AGENTGUARD_API_KEY
AGENTSHARK_DEMO_LLM_BASE_URL
AGENTSHARK_DEMO_LLM_MODEL
AGENTSHARK_DEMO_MCP_URL
```

Do not publish the Runner port on the host. The Runner token and upstream credentials are
never accepted in a Run request, returned in a snapshot, or attached to a trace.
