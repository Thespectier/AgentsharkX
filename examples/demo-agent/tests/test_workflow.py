from __future__ import annotations

import functools
from collections.abc import Callable, Sequence
from typing import Any
from uuid import uuid4

import pytest
from agentshark import AgentShark
from agentshark.config import SDKConfig
from agentshark.context import RuntimeContext
from agentshark_demo.config import DemoSettings
from agentshark_demo.errors import DemoMCPTimeout
from agentshark_demo.execution import execute_demo
from agentshark_demo.llm import StageLLMClient
from agentshark_demo.models import Outcome, RunnerStartRequest, Scenario
from agentshark_demo.scenarios import DEMO_HOST, DEMO_INDICATOR, scenario_spec
from langchain_core.callbacks.manager import CallbackManagerForLLMRun
from langchain_core.language_models.llms import LLM
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import StatusCode


class DeterministicTestLLM(LLM):
    @property
    def _llm_type(self) -> str:
        return "agentshark-demo-test"

    def _call(
        self,
        prompt: str,
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> str:
        _ = (stop, run_manager, kwargs)
        return f"fixture:{prompt.splitlines()[1]}"


class FixedMCP:
    def __init__(self, scenario: Scenario) -> None:
        self.scenario = scenario

    def asset_lookup(self, hostname: str) -> dict[str, Any]:
        assert hostname == DEMO_HOST
        return {"hostname": hostname, "simulated": True}

    def threat_intel_lookup(self, indicator: str, scenario: str) -> dict[str, Any]:
        assert indicator == DEMO_INDICATOR
        assert scenario == self.scenario.value
        if self.scenario is Scenario.FAILURE:
            raise DemoMCPTimeout(DemoMCPTimeout.code)
        return {
            "indicator": indicator,
            "malicious": self.scenario is Scenario.APPROVAL,
            "simulated": True,
        }


class FixedGuard:
    def __init__(self, *, deny_action: bool = False) -> None:
        self.deny_action = deny_action
        self.action_invocations = 0

    def attach_langchain(self, agent: Any) -> Any:
        return agent

    def guard_tool(
        self,
        call: Callable[..., Any],
        *,
        name: str,
        description: str,
        capabilities: Sequence[str],
    ) -> Callable[..., Any]:
        _ = (description, capabilities)
        if name != "send_http":
            return call

        @functools.wraps(call)
        def guarded(*args: Any, **kwargs: Any) -> Any:
            self.action_invocations += 1
            if self.deny_action:
                return {"agentguard": "blocked", "simulated": True}
            return call(*args, **kwargs)

        return guarded

    def bind_task(self, context: RuntimeContext, goal_metadata: dict[str, Any]) -> object:
        _ = (context, goal_metadata)
        return object()

    def restore_task(self, state: Any) -> None:
        _ = state

    def close(self) -> None:
        return None


@pytest.mark.parametrize("scenario", list(Scenario))
def test_fixed_workflow_counts_and_trace_contract(scenario: Scenario) -> None:
    request = _request(scenario)
    exporter = InMemorySpanExporter()
    guard = FixedGuard()
    runtime = _runtime(request, exporter, guard)

    result = execute_demo(
        request,
        settings=_settings(),
        runtime_factory=lambda ignored: runtime,
        llm_factory=lambda ignored_runtime, ignored_settings: StageLLMClient(
            DeterministicTestLLM()
        ),
        mcp_factory=lambda ignored_settings: FixedMCP(scenario),
    )

    expected = scenario_spec(scenario)
    assert result.outcome is expected.expected_outcome
    assert result.completed_steps == expected.steps
    assert result.metrics.llm_calls == expected.expected_metrics.llm_calls
    assert result.metrics.mcp_calls == expected.expected_metrics.mcp_calls
    assert result.metrics.local_tool_calls == expected.expected_metrics.local_tool_calls
    assert result.metrics.a2a_calls == expected.expected_metrics.a2a_calls
    assert result.metrics.error_count == (1 if scenario is Scenario.FAILURE else 0)
    assert guard.action_invocations == (1 if scenario is Scenario.APPROVAL else 0)

    spans = exporter.get_finished_spans()
    root = next(span for span in spans if span.name == "agentshark.task")
    assert root.status.status_code is StatusCode.OK
    assert result.trace_id == f"{root.context.trace_id:032x}"
    assert result.root_span_id == f"{root.context.span_id:016x}"
    assert all(span.attributes.get("agentshark.demo") is True for span in spans)
    assert all(
        span.attributes.get("agentshark.demo.run_id") == str(request.run_id)
        for span in spans
    )

    llm_spans = _countable(spans, "LLM")
    mcp_spans = [
        span
        for span in _countable(spans, "TOOL")
        if span.attributes.get("agentshark.tool.kind") == "mcp"
    ]
    a2a_spans = _countable(spans, "AGENT")
    local_spans = [
        span
        for span in _countable(spans, "TOOL")
        if span.attributes.get("agentshark.tool.kind") != "mcp"
    ]
    assert len(llm_spans) == expected.expected_metrics.llm_calls
    assert len(mcp_spans) == expected.expected_metrics.mcp_calls
    assert len(a2a_spans) == expected.expected_metrics.a2a_calls
    assert len(local_spans) == expected.expected_metrics.local_tool_calls
    if scenario is Scenario.FAILURE:
        failed = next(
            span
            for span in mcp_spans
            if span.attributes.get("tool.name") == "threat_intel_lookup"
        )
        assert failed.status.status_code is StatusCode.ERROR


def test_approval_denial_ends_safely_without_action_side_effect() -> None:
    request = _request(Scenario.APPROVAL)
    exporter = InMemorySpanExporter()
    guard = FixedGuard(deny_action=True)
    runtime = _runtime(request, exporter, guard)

    result = execute_demo(
        request,
        settings=_settings(),
        runtime_factory=lambda ignored: runtime,
        llm_factory=lambda ignored_runtime, ignored_settings: StageLLMClient(
            DeterministicTestLLM()
        ),
        mcp_factory=lambda ignored_settings: FixedMCP(Scenario.APPROVAL),
    )

    assert result.outcome is Outcome.DENIED
    root = next(
        span
        for span in exporter.get_finished_spans()
        if span.name == "agentshark.task"
    )
    assert root.status.status_code is StatusCode.OK
    assert guard.action_invocations == 1


def _request(scenario: Scenario) -> RunnerStartRequest:
    run_id = uuid4()
    return RunnerStartRequest(
        runId=run_id,
        scenario=scenario,
        delayMs=0,
        taskId=f"task-{run_id}",
        sessionId=f"session-{run_id}",
        requestId=f"request-{run_id}",
    )


def _runtime(
    request: RunnerStartRequest,
    exporter: InMemorySpanExporter,
    guard: FixedGuard,
) -> AgentShark:
    config = SDKConfig(
        trace_endpoint="http://127.0.0.1:4318/v1/traces",
        ingest_token="demo-test-ingest-token",
        guard_enabled=False,
        environment="test",
        batch_schedule_delay_ms=1,
        batch_max_queue_size=128,
        batch_max_export_size=32,
    )
    return AgentShark(
        config,
        agent_id="demo-incident-investigator",
        session_id=request.session_id,
        _exporter=exporter,
        _guard_adapter=guard,
    )


def _settings() -> DemoSettings:
    return DemoSettings(
        llm_base_url="http://fixture.invalid/v1",
        llm_model="agentshark-demo-model-v1",
        mcp_url="http://fixture.invalid/mcp",
    )


def _countable(spans: Any, kind: str) -> list[Any]:
    return [
        span
        for span in spans
        if span.attributes.get("agentshark.countable") is True
        and span.attributes.get("openinference.span.kind") == kind
    ]
