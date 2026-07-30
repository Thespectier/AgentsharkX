# Agentshark Python SDK

This package is a repository-local integration for AgentsharkX. It is not
published to PyPI. Install the pinned AgentGuard checkout first, then install
this directory in editable mode:

```bash
make sdk-bootstrap
python -m pip install -c ./sdk/python/constraints.txt \
  -e ./third_party/AgentGuard -e './sdk/python[dev]'
```

The verified AgentGuard revision is
`4b755fb4a4a2763b7e817b3d0220fe5c22187b59`. Its source stays outside this
package.

Required runtime configuration:

```text
AGENTSHARK_TRACE_ENDPOINT=http://127.0.0.1:4318/v1/traces
AGENTSHARK_TRACE_INGEST_TOKEN=<collector bearer token>
AGENTSHARK_TRACE_CONTENT_MODE=metadata
AGENTSHARK_GUARD_ENABLED=true
AGENTSHARK_GUARD_FAILURE_MODE=closed
AGENTGUARD_SERVER_URL=http://127.0.0.1:38080
AGENTGUARD_API_KEY=<agentguard api key>
```

`metadata` is the default content mode and excludes prompts, completions, tool
arguments, results, and task goals from export. `none` keeps only minimal
classification and state. `full` explicitly enables content collection, with
`AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES` applied to each content attribute.
Authorization, cookies, API keys, passwords, and credentials are redacted in
every mode.

Collector export failure never changes the agent's business result. AgentGuard
uses closed failure mode by default; `open` must be selected explicitly when an
unprotected fallback is acceptable. One runtime/Guard/Agent pair supports one
active task and rejects overlapping tasks because AgentGuard task context is
mutable. Use a separate runtime and agent per concurrent worker/session, call
`flush()` when delivery matters, and always call `close()` before process exit.

```python
from agentshark import AgentShark

runtime = AgentShark.from_env(
    agent_id="research-agent",
    session_id="session-42",
)
agent = runtime.attach_langchain(agent)

with runtime.task(task_id="task-7", goal="Summarize the report"):
    result = agent.invoke({"input": "Summarize the report"})

runtime.close()
```

Demo runtimes may add bounded `agentshark.demo` Task attributes. The immutable
context returned by `task()` exposes both the generated `trace_id` and
`root_span_id`; child spans in that Trace inherit the validated Demo attributes.
Explicit local tools can be routed through the same per-runtime AgentGuard
session without reading private SDK state:

```python
with runtime.task(
    task_id="demo-task",
    attributes={"agentshark.demo": True, "agentshark.demo.run_id": "run-42"},
) as task_context:
    guarded_action = runtime.guard_tool(send_http, name="send_http")
    result = guarded_action(url="https://quarantine.example.com/hosts/web-01")
    print(task_context.trace_id, task_context.root_span_id)
```

`guard_plugin_config` on `AgentShark`/`AgentShark.from_env` forwards a
caller-owned session plugin configuration to the pinned AgentGuard facade. The
SDK deep-copies it during construction so a later caller mutation cannot alter
the active Guard.

`guard_sandbox_profile` optionally forwards one pinned AgentGuard
`PermissionProfile` to that runtime's Guard. Omitting it preserves AgentGuard's
default local restricted profile. Callers must pass the source-owned profile
object, not an inferred mapping. The Demo creates a fresh profile per Run that
allows the fixed `quarantine.example.com` network capability while keeping
subprocess and file-write capabilities disabled; its action implementation still
performs no network I/O and rejects every other URL and body.

Phase 14 provides ingest and persistence only. BFF Trace query/detail routes
and the frontend Trace experience are deliberately deferred to Phase 15.

MCP calls must be marked explicitly. The wrapper reuses the active LangChain
Tool span when one exists, otherwise it creates one SDK Tool span:

```python
mcp_tool = runtime.mark_mcp_tool(
    mcp_tool,
    server_name="research-mcp",
    method="tools/call",
)
```

A2A calls also use an explicit wrapper. The yielded invocation injects W3C
`traceparent` and `tracestate` into a carrier:

```python
with runtime.invoke_agent(peer_agent_id="planner") as invocation:
    headers = {}
    invocation.inject(headers)
    response = call_planner(headers=headers)
```

For an asynchronous relationship that cannot be represented as a direct
parent, extract a link from the received carrier and create a detached linked
span. This never invents a `parent_span_id`:

```python
link = runtime.link_from_carrier(received_headers)
with runtime.invoke_agent(
    peer_agent_id="worker",
    links=[link],
    detached=True,
):
    enqueue_work()
```
