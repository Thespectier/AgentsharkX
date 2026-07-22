# Agent integration

AgentsharkX does not sit in the agent data plane. Agents send model or MCP
traffic through agentgateway and attach the AgentGuard client to their runtime;
AgentsharkX reads only the two management APIs.

## Minimal AgentGuard event

[`examples/agentguard_minimal.py`](../examples/agentguard_minimal.py) uses the
verified AgentGuard `v2.1` constructor and `wrap_tool` contract. Configure:

- `AGENTGUARD_SERVER_URL`: the AgentGuard API reachable by the agent process;
- `AGENTGUARD_API_KEY`: the AgentGuard server API key, never an AgentsharkX
  browser token.

The example supplies explicit `agent_id`, `session_id`, and `user_id` values.
AgentsharkX preserves those upstream identities; it never constructs an Agent
from timing, names, or gateway logs.

## Framework adapters

The pinned AgentGuard release exposes `Guard`/`Principal` plus adapters including
`attach_langchain`, `attach_langgraph`, `attach_autogen`,
`attach_openai_agents`, and `attach_llamaindex`. Use the exact release
documentation and keep the AgentGuard API key in the agent's server-side secret
store. Adapter event phases remain `llm_before`, `llm_after`, `tool_before`, or
`tool_after`; AgentsharkX does not rename them into a synthetic policy model.

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
   source, phase, action, and redacted detail reference.
5. A cross-source event is marked correlated only when both sources provide the
   same verified non-empty trace or session identifier.
