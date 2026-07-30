# Agent integration

AgentsharkX does not sit in the agent data plane. Agents send model or MCP
traffic through agentgateway, attach AgentGuard locally, and export standard
OTLP telemetry directly to the independent AgentsharkX Collector. The BFF still
reads only management state and never proxies agent requests.

## Local Agentshark SDK

The Phase 14 Python package is repository-local and is not published. The
repository forbids submodules, so the bootstrap helper clones the already
server-verified immutable AgentGuard revision into an ignored directory and
validates its revision and packaging files:

```bash
make sdk-bootstrap
python -m pip install -c ./sdk/python/constraints.txt \
  -e ./third_party/AgentGuard -e ./sdk/python
```

Required production settings are `AGENTSHARK_TRACE_ENDPOINT` (ending in
`/v1/traces`), `AGENTSHARK_TRACE_INGEST_TOKEN`, `AGENTGUARD_SERVER_URL`, and
`AGENTGUARD_API_KEY`. `AGENTSHARK_GUARD_FAILURE_MODE=closed` is the default.
Set `open` only for an explicitly accepted unprotected fallback; Trace export
failure is always non-blocking and produces one content-free warning.

```python
from agentshark import AgentShark

runtime = AgentShark.from_env(
    agent_id="research-agent",
    session_id="session-42",
)
protected_agent = runtime.attach_langchain(agent)

with runtime.task(task_id="task-7", goal=prompt):
    result = protected_agent.invoke({"messages": messages})

runtime.close()
```

`task()` supports both `with` and `async with`; `AgentShark` itself supports
sync/async context managers plus explicit `flush()` and idempotent `close()`.
Task context uses `ContextVar`, but AgentGuard's facade has mutable context, so
one runtime/Guard/Agent permits only one active task. A concurrent second task
raises `ConcurrentTaskError`; use one runtime and Agent instance per worker or
session. LangChain instrumentation is process-wide and initialized once, while
attaching the same object twice to one runtime is idempotent and cross-runtime
ownership is rejected.

`AGENTSHARK_TRACE_CONTENT_MODE=metadata` is the default. `none` retains minimal
classification/state, and `full` opts prompt, completion, task goal, tool body,
status, and exception content into the Collector's bounded separate Payload
table. `AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES` controls each item. Authorization,
cookies, API keys, passwords, and credential-shaped values are redacted in the
SDK and Collector in all modes. Full content is never placed in normal logs or
Span metadata.

MCP and A2A classification is explicit:

```python
mcp_tool = runtime.mark_mcp_tool(
    mcp_tool,
    server_name="research-mcp",
    method="tools/call",
)

with runtime.invoke_agent(peer_agent_id="planner") as invocation:
    headers = {}
    invocation.inject(headers)
    response = call_planner(headers=headers)
```

Only MCP `tools/call` is countable; `initialize` and `tools/list` remain visible
protocol spans. Only `gen_ai.operation.name=invoke_agent` with a non-empty
explicit peer ID is A2A. Use `remote_context()` to continue a W3C parent context
or `link_from_carrier()` plus `detached=True` for asynchronous relationships;
the SDK never fabricates a peer's internal spans or a parent from timing.

Run `make sdk-test` for unit/framework checks and
`make sdk-agentguard-contract` for the exact upstream editable-install/public
API validation. The authenticated Phase 15 BFF lists those records under
**Audit / Traces**. Trace Detail is payload-safe; select one Span to fetch its
complete retained attributes, Resource, Events, and payload through Span Detail.

## Minimal AgentGuard event

[`examples/agentguard_minimal.py`](../examples/agentguard_minimal.py) uses the
verified AgentGuard main revision
`4b755fb4a4a2763b7e817b3d0220fe5c22187b59` constructor and `wrap_tool`
contract. Configure:

- `AGENTGUARD_SERVER_URL`: the AgentGuard API reachable by the agent process;
- `AGENTGUARD_API_KEY`: the AgentGuard server API key, never an AgentsharkX
  browser token.

The example supplies explicit `agent_id`, `session_id`, and `user_id` values.
AgentsharkX preserves those upstream identities; it never constructs an Agent
from timing, names, or gateway logs.

## Framework adapters

The pinned AgentGuard snapshot exposes `Guard`/`Principal` plus adapters
including `attach_langchain`, `attach_langgraph`, `attach_autogen`,
`attach_openai_agents`, and `attach_llamaindex`. Use the exact
pinned-revision documentation and keep the AgentGuard API key in the agent's
server-side secret store. Adapter event phases remain `llm_before`,
`llm_after`, `tool_before`, or `tool_after`; AgentsharkX does not rename them
into a synthetic policy model.

For gateway traffic, point the agent's OpenAI-compatible or MCP client at an
explicit listener configured in agentgateway. The repository's default
`deploy/agentgateway/config.yaml` intentionally has no business routes or
provider credentials. Configure those in the upstream console, enable its
request-log database when traffic history is required, and never place provider
keys in AgentsharkX.

## Verification

After an agent run:

1. **System** should show the relevant source healthy.
2. **Trust** should contain only identities/resources explicitly reported by
   AgentGuard.
3. **Audit → Traffic** shows gateway log records only when upstream storage is
   configured.
4. **Audit → Security events** shows AgentGuard events with their upstream ID,
   source, phase, action, and complete upstream detail for an authenticated
   administrator.
5. A cross-source event is marked correlated only when both sources provide the
   same verified non-empty trace or session identifier.
