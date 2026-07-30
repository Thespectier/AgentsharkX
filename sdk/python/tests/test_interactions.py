from __future__ import annotations

import asyncio
import importlib.util

import pytest
from helpers import config, runtime
from openinference.instrumentation.mcp import MCPInstrumentor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from agentshark.integrations import mcp as mcp_integration
from agentshark.tracing import TelemetryManager


def spans(exporter: object):
    assert isinstance(exporter, InMemorySpanExporter)
    return exporter.get_finished_spans()


def test_mcp_protocol_and_tool_classification() -> None:
    sdk, exporter, _ = runtime()

    def call_tool(value: str) -> str:
        return value.upper()

    wrapped = sdk.wrap_mcp_tool(call_tool, server_name="mail", tool_name="mail.send")
    with sdk.task(task_id="task-a"):
        with sdk.mcp_call(server_name="mail", method="initialize"):
            pass
        with sdk.mcp_call(server_name="mail", method="tools/list"):
            pass
        assert wrapped("hello") == "HELLO"
    sdk.flush()

    mcp_spans = [span for span in spans(exporter) if "agentshark.mcp.method" in span.attributes]
    assert [span.attributes["agentshark.mcp.method"] for span in mcp_spans] == [
        "initialize",
        "tools/list",
        "tools/call",
    ]
    assert [span.attributes["agentshark.countable"] for span in mcp_spans] == [
        False,
        False,
        True,
    ]
    assert mcp_spans[-1].attributes["agentshark.tool.kind"] == "mcp"
    sdk.close()


def test_mark_mcp_tool_wraps_object_entrypoint_once() -> None:
    sdk, exporter, _ = runtime()

    class Tool:
        name = "lookup"

        @staticmethod
        def func(value: str) -> str:
            return value

    tool = Tool()
    assert sdk.mark_mcp_tool(tool, server_name="research") is tool
    assert sdk.mark_mcp_tool(tool, server_name="research") is tool
    with sdk.task(task_id="task-a"):
        assert tool.func("value") == "value"
    sdk.flush()
    calls = [span for span in spans(exporter) if span.attributes.get("agentshark.mcp.method")]
    assert len(calls) == 1
    sdk.close()


def test_mcp_call_does_not_reclassify_a_langchain_chain_span(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    sdk, exporter, _ = runtime()

    with sdk.task(task_id="task-a"):
        with sdk._tracer.start_as_current_span(
            "langgraph-node",
            attributes={"openinference.span.kind": "CHAIN"},
        ) as chain_span:
            monkeypatch.setattr(
                mcp_integration,
                "_current_langchain_span",
                lambda: chain_span,
            )
            with sdk.mcp_call(
                server_name="research",
                tool_name="asset_lookup",
            ):
                pass
    sdk.flush()

    finished = spans(exporter)
    chain = next(span for span in finished if span.name == "langgraph-node")
    mcp = next(span for span in finished if span.name == "asset_lookup")
    assert chain.attributes["openinference.span.kind"] == "CHAIN"
    assert "agentshark.mcp.method" not in chain.attributes
    assert mcp.attributes["agentshark.mcp.method"] == "tools/call"
    assert mcp.attributes["agentshark.countable"] is True
    sdk.close()


def test_mcp_context_instrumentor_does_not_receive_a_tracer_provider(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[dict[str, object]] = []
    manager = TelemetryManager(config())
    monkeypatch.setattr(importlib.util, "find_spec", lambda name: object())
    monkeypatch.setattr(
        MCPInstrumentor,
        "instrument",
        lambda self, **kwargs: calls.append(kwargs),
    )
    try:
        assert manager.ensure_mcp_instrumented()
    finally:
        manager.provider.shutdown()
    assert calls == [{}]


def test_a2a_span_injects_w3c_context_without_peer_internals() -> None:
    sdk, exporter, _ = runtime()
    carrier: dict[str, str] = {}
    with sdk.task(task_id="task-a"):
        with sdk.invoke_agent(peer_agent_id="planner") as invocation:
            invocation.inject(carrier)
    sdk.flush()
    assert carrier["traceparent"].startswith("00-")
    finished = spans(exporter)
    a2a = next(
        span for span in finished if span.attributes.get("gen_ai.operation.name") == "invoke_agent"
    )
    assert a2a.attributes["agentshark.peer_agent.id"] == "planner"
    assert a2a.attributes["agentshark.countable"] is True
    assert len(finished) == 2
    sdk.close()


def test_unentered_a2a_invocation_does_not_leak_an_unfinished_span() -> None:
    sdk, exporter, _ = runtime()
    with sdk.task(task_id="task-a"):
        invocation = sdk.invoke_agent(peer_agent_id="unused-peer")
        with pytest.raises(RuntimeError, match="must be entered"):
            invocation.inject({})
    sdk.flush()
    assert [span.name for span in spans(exporter)] == ["agentshark.task"]
    sdk.close()


def test_remote_context_continues_trace_and_detached_relationship_uses_link() -> None:
    caller, caller_exporter, _ = runtime()
    peer, peer_exporter, _ = runtime()
    carrier: dict[str, str] = {}
    with caller.task(task_id="caller-task"):
        with caller.invoke_agent(peer_agent_id="peer") as invocation:
            invocation.inject(carrier)
    caller.flush()

    with peer.remote_context(carrier):
        with peer.task(task_id="peer-task"):
            pass
    link = peer.link_from_carrier(
        carrier,
        {
            "tool.arguments": "private link arguments",
            "http.request.header.authorization": "Bearer private-link-token",
        },
    )
    with peer.task(task_id="queue-task"):
        with peer.invoke_agent(peer_agent_id="worker", links=[link], detached=True):
            pass
    peer.flush()

    caller_a2a = next(
        span
        for span in spans(caller_exporter)
        if span.attributes.get("gen_ai.operation.name") == "invoke_agent"
    )
    peer_spans = spans(peer_exporter)
    peer_task = next(
        span for span in peer_spans if span.attributes.get("agentshark.task.id") == "peer-task"
    )
    linked = next(
        span for span in peer_spans if span.attributes.get("agentshark.peer_agent.id") == "worker"
    )
    assert peer_task.context.trace_id == caller_a2a.context.trace_id
    assert linked.parent is None
    assert linked.context.trace_id != caller_a2a.context.trace_id
    assert linked.links[0].context.trace_id == caller_a2a.context.trace_id
    assert "tool.arguments" not in linked.links[0].attributes
    assert linked.links[0].attributes["http.request.header.authorization"] == "__REDACTED__"
    assert "private-link-token" not in repr(linked.links[0].attributes)
    caller.close()
    peer.close()


def test_two_runtimes_can_continue_one_trace_while_both_tasks_are_active() -> None:
    caller, caller_exporter, _ = runtime()
    peer, peer_exporter, _ = runtime()
    carrier: dict[str, str] = {}

    with caller.task(task_id="caller-active") as caller_context:
        with caller.invoke_agent(peer_agent_id="peer") as invocation:
            invocation.inject(carrier)
        with peer.remote_context(carrier):
            with peer.task(task_id="peer-concurrent") as peer_context:
                assert peer_context.trace_id == caller_context.trace_id

    caller.flush()
    peer.flush()
    caller_spans = spans(caller_exporter)
    peer_spans = spans(peer_exporter)
    caller_task = next(
        span
        for span in caller_spans
        if span.attributes.get("agentshark.task.id") == "caller-active"
    )
    peer_task = next(
        span
        for span in peer_spans
        if span.attributes.get("agentshark.task.id") == "peer-concurrent"
    )
    assert caller_task.context.trace_id == peer_task.context.trace_id
    assert caller_task.attributes["agentshark.runtime.id"] == caller.runtime_id
    assert peer_task.attributes["agentshark.runtime.id"] == peer.runtime_id
    assert not any(
        span.attributes.get("agentshark.task.id") == "peer-concurrent" for span in caller_spans
    )
    caller.close()
    peer.close()


@pytest.mark.asyncio
async def test_async_a2a_wrapper_preserves_result_and_error() -> None:
    sdk, exporter, _ = runtime()

    async def success() -> str:
        await asyncio.sleep(0)
        return "ok"

    with sdk.task(task_id="task-a"):
        assert await sdk.ainvoke_agent(peer_agent_id="planner", call=success) == "ok"

    async def fail() -> None:
        raise RuntimeError("business error")

    with pytest.raises(RuntimeError, match="business error"):
        with sdk.task(task_id="task-b"):
            await sdk.ainvoke_agent(peer_agent_id="planner", call=fail)
    sdk.flush()
    assert len([span for span in spans(exporter) if span.name.startswith("invoke_agent")]) == 2
    sdk.close()
