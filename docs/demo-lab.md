# Demo Lab

Demo Lab is an opt-in supporting tool for exercising the real AgentsharkX
management and telemetry paths with deterministic inputs. It is not a sixth
product capability and it is disabled in the normal preview.

## Start and stop

```bash
make demo-up
make demo-status
make demo-smoke
make demo-down
```

`demo-up` bootstraps a random Runner token, builds the pinned Demo image, and
starts the complete container topology. Open <http://127.0.0.1:8080/demo> and
log in with `AGENTSHARK_ADMIN_TOKEN` from the ignored `.env`.

`demo-down` removes only the three stateless Demo service containers and
returns the preview to its configured agentgateway runtime mode. It preserves
PostgreSQL, Audit history, Trace records, and the operator-owned gateway
configuration.

## Fixed scenarios

| Scenario | Result | Expected counts |
| --- | --- | --- |
| Happy | Normal deterministic completion | LLM 3, MCP 2, local tool 1, A2A 1, human checks 0, errors 0 |
| Approval | Wait for one AgentGuard approve or deny decision | LLM 3, MCP 2, local tools 2, A2A 1, human checks 1, errors 0 |
| Failure | One MCP call returns `DEMO_MCP_TIMEOUT`; the workflow uses existing evidence | LLM 3, MCP 2, local tool 1, A2A 1, human checks 0, errors at least 1 |

The only adjustable input is a node delay from `0` through `2000` milliseconds.
The API rejects arbitrary prompts, URLs, commands, identities, and targets.
`send_http` returns a simulated receipt and performs no network operation.

## Data path

```text
Browser -> authenticated AgentsharkX BFF -> private Demo Runner
Demo Runner -> agentshark-demo-gateway -> deterministic LLM fixture
Demo Runner -> deterministic Streamable HTTP MCP fixture
Demo Runner -> AgentGuard demo_tripwire -> existing Protect approval API
Demo Runner -> OTLP/HTTP Collector -> PostgreSQL -> Audit Trace APIs
```

The separate `deploy/agentgateway/demo-config.yaml` advertises only
`agentshark-demo-model-v1`. It is mounted into a distinct gateway process and
does not modify the operator's providers, models, listeners, routes, or logs.
The Runner and fixtures have no host-published ports. The Demo gateway's native
management console is published separately on loopback at
<http://127.0.0.1:15010>; it is never used for the operator gateway.

## Correlation and retention

Run, Task, Session, Trace, and approval IDs are generated before execution.
Trace evidence is verified only when the exact Trace record also contains the
expected Task and Session IDs. Approval evidence requires the exact Session ID.
Gateway log correlation is shown as unavailable unless the pinned upstream
returns exactly three unique request-log records carrying the exact Trace ID.
Only that complete set verifies all three expected LLM requests; duplicate,
missing, or extra records remain unavailable. Demo Lab then links one precise
upstream log ID in the Demo gateway console. Time proximity and request order
are never used.

Starting a Run while a required readiness probe is unhealthy returns
`DEMO_NOT_READY` with only the failed required checks in `error.failedChecks`.
Optional degraded components do not block Run creation.

`demo_runs` stores bounded control state and public progress only. Prompt,
completion, tool argument, result, authorization, and credential bodies do not
enter Demo rows, list responses, SSE events, or application logs. Authenticated
Trace Span and Audit detail retain their existing source-owned detail behavior.

## Recovery

The BFF persists run history and event cursors in PostgreSQL. Refreshing the
browser or restarting the BFF recovers the active or most recent run. The
Runner itself keeps only one active execution and a bounded in-memory history.
If it is lost, times out, or violates its internal response contract, the BFF
records a finite interrupted state and retains already collected Trace data.
