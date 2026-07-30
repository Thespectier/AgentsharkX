"""Fixed LangGraph StateGraph for all three Demo scenarios."""

from __future__ import annotations

import time
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, Protocol, TypedDict, cast

from agentshark import AgentShark
from langchain_core.tools import StructuredTool
from langgraph.graph import END, START, StateGraph

from agentshark_demo.agents.risk_reviewer import review_risk
from agentshark_demo.errors import DemoCancelled, DemoMCPTimeout
from agentshark_demo.models import (
    DemoExecutionResult,
    ExecutionMetrics,
    Outcome,
    Scenario,
)
from agentshark_demo.scenarios import (
    DEMO_ACTION_URL,
    DEMO_HOST,
    DEMO_INDICATOR,
    MCP_SERVER_NAME,
    PEER_AGENT_ID,
)
from agentshark_demo.tools.risk_score import calculate_risk_score
from agentshark_demo.tools.simulated_action import send_http


class LLMClient(Protocol):
    def complete(self, stage: str, scenario: Scenario) -> str: ...


class MCPClient(Protocol):
    def asset_lookup(self, hostname: str) -> dict[str, Any]: ...

    def threat_intel_lookup(self, indicator: str, scenario: str) -> dict[str, Any]: ...


@dataclass(frozen=True, slots=True)
class ExecutionHooks:
    on_identity: Callable[[str, str], None] = lambda trace_id, span_id: None
    on_step_started: Callable[[str], None] = lambda step: None
    on_step_completed: Callable[[str], None] = lambda step: None
    is_cancelled: Callable[[], bool] = lambda: False
    wait: Callable[[float], bool] = lambda seconds: _sleep(seconds)


def _sleep(seconds: float) -> bool:
    time.sleep(seconds)
    return False


class WorkflowState(TypedDict, total=False):
    scenario: Scenario
    plan: str
    evidence: dict[str, Any]
    analysis: str
    risk: dict[str, Any]
    peer_review: str
    action_result: dict[str, Any]
    outcome: Outcome
    report: str
    completed_steps: list[str]
    llm_calls: int
    mcp_calls: int
    local_tool_calls: int
    a2a_calls: int
    error_count: int


class DemoWorkflow:
    def __init__(
        self,
        *,
        runtime: AgentShark,
        llm: LLMClient,
        mcp: MCPClient,
        delay_ms: int,
        hooks: ExecutionHooks | None = None,
    ) -> None:
        if delay_ms < 0 or delay_ms > 2_000:
            raise ValueError("delay_ms must be between 0 and 2000")
        self._runtime = runtime
        self._llm = llm
        self._mcp = mcp
        self._delay_seconds = delay_ms / 1_000
        self._hooks = hooks or ExecutionHooks()
        guarded_score = runtime.guard_tool(
            calculate_risk_score,
            name="calculate_risk_score",
            description="Deterministic local risk score",
            capabilities=("local_compute",),
        )
        guarded_action = runtime.guard_tool(
            send_http,
            name="send_http",
            description="Simulated quarantine action with no network side effect",
            capabilities=("network",),
        )
        self._score_tool = StructuredTool.from_function(
            func=guarded_score,
            name="calculate_risk_score",
            description="Calculate a deterministic risk score.",
        )
        self._action_tool = StructuredTool.from_function(
            func=guarded_action,
            name="send_http",
            description="Simulate one fixed quarantine request.",
        )
        self._graph = self._build_graph()

    def run(self, scenario: Scenario, trace_id: str, root_span_id: str) -> DemoExecutionResult:
        initial: WorkflowState = {
            "scenario": scenario,
            "evidence": {},
            "completed_steps": [],
            "llm_calls": 0,
            "mcp_calls": 0,
            "local_tool_calls": 0,
            "a2a_calls": 0,
            "error_count": 0,
            "outcome": Outcome.NONE,
        }
        final = cast(WorkflowState, self._graph.invoke(initial))
        return DemoExecutionResult(
            scenario=scenario,
            outcome=final["outcome"],
            traceId=trace_id,
            rootSpanId=root_span_id,
            report=final["report"],
            metrics=ExecutionMetrics(
                llmCalls=final["llm_calls"],
                mcpCalls=final["mcp_calls"],
                localToolCalls=final["local_tool_calls"],
                a2aCalls=final["a2a_calls"],
                errorCount=final["error_count"],
            ),
            completedSteps=tuple(final["completed_steps"]),
        )

    def _build_graph(self) -> Any:
        graph = StateGraph(WorkflowState)
        add_node = cast(
            Callable[[str, Callable[[WorkflowState], WorkflowState]], Any],
            graph.add_node,
        )
        add_node("bootstrap", self._node("bootstrap", self._bootstrap))
        add_node("plan", self._node("plan", self._plan))
        add_node("asset_lookup", self._node("asset_lookup", self._asset_lookup))
        add_node(
            "threat_intel_lookup",
            self._node("threat_intel_lookup", self._threat_intel_lookup),
        )
        add_node(
            "analyze_evidence",
            self._node("analyze_evidence", self._analyze_evidence),
        )
        add_node(
            "calculate_risk_score",
            self._node("calculate_risk_score", self._calculate_risk_score),
        )
        add_node(
            "invoke_risk_reviewer",
            self._node("invoke_risk_reviewer", self._invoke_risk_reviewer),
        )
        add_node(
            "guarded_action",
            self._node("guarded_action", self._guarded_action),
        )
        add_node("render_report", self._node("render_report", self._render_report))
        add_node("finish", self._node("finish", self._finish))
        graph.add_edge(START, "bootstrap")
        graph.add_edge("bootstrap", "plan")
        graph.add_edge("plan", "asset_lookup")
        graph.add_edge("asset_lookup", "threat_intel_lookup")
        graph.add_edge("threat_intel_lookup", "analyze_evidence")
        graph.add_edge("analyze_evidence", "calculate_risk_score")
        graph.add_edge("calculate_risk_score", "invoke_risk_reviewer")
        graph.add_conditional_edges(
            "invoke_risk_reviewer",
            lambda state: (
                "guarded_action"
                if state["scenario"] is Scenario.APPROVAL
                else "render_report"
            ),
        )
        graph.add_edge("guarded_action", "render_report")
        graph.add_edge("render_report", "finish")
        graph.add_edge("finish", END)
        return graph.compile()

    def _node(
        self,
        name: str,
        call: Callable[[WorkflowState], dict[str, Any]],
    ) -> Callable[[WorkflowState], WorkflowState]:
        def invoke(state: WorkflowState) -> WorkflowState:
            self._checkpoint(name)
            update = call(state)
            update["completed_steps"] = [*state.get("completed_steps", []), name]
            self._hooks.on_step_completed(name)
            return cast(WorkflowState, update)

        return invoke

    def _checkpoint(self, step: str) -> None:
        if self._hooks.is_cancelled():
            raise DemoCancelled(DemoCancelled.code)
        self._hooks.on_step_started(step)
        if self._delay_seconds and self._hooks.wait(self._delay_seconds):
            raise DemoCancelled(DemoCancelled.code)
        if self._hooks.is_cancelled():
            raise DemoCancelled(DemoCancelled.code)

    @staticmethod
    def _bootstrap(state: WorkflowState) -> dict[str, Any]:
        _ = state
        return {}

    def _plan(self, state: WorkflowState) -> dict[str, Any]:
        return {
            "plan": self._llm.complete("demo.plan", state["scenario"]),
            "llm_calls": state["llm_calls"] + 1,
        }

    def _asset_lookup(self, state: WorkflowState) -> dict[str, Any]:
        with self._runtime.mcp_call(
            server_name=MCP_SERVER_NAME,
            method="tools/call",
            tool_name="asset_lookup",
        ):
            asset = self._mcp.asset_lookup(DEMO_HOST)
        return {
            "evidence": {**state["evidence"], "asset": asset},
            "mcp_calls": state["mcp_calls"] + 1,
        }

    def _threat_intel_lookup(self, state: WorkflowState) -> dict[str, Any]:
        try:
            with self._runtime.mcp_call(
                server_name=MCP_SERVER_NAME,
                method="tools/call",
                tool_name="threat_intel_lookup",
            ):
                threat = self._mcp.threat_intel_lookup(
                    DEMO_INDICATOR,
                    state["scenario"].value,
                )
        except DemoMCPTimeout:
            return {
                "evidence": {**state["evidence"], "threat_error": DemoMCPTimeout.code},
                "mcp_calls": state["mcp_calls"] + 1,
                "error_count": state["error_count"] + 1,
            }
        return {
            "evidence": {**state["evidence"], "threat": threat},
            "mcp_calls": state["mcp_calls"] + 1,
        }

    def _analyze_evidence(self, state: WorkflowState) -> dict[str, Any]:
        return {
            "analysis": self._llm.complete("demo.analyze", state["scenario"]),
            "llm_calls": state["llm_calls"] + 1,
        }

    def _calculate_risk_score(self, state: WorkflowState) -> dict[str, Any]:
        risk = self._score_tool.invoke({"evidence": state["evidence"]})
        if not isinstance(risk, dict):
            raise RuntimeError("DEMO_RISK_RESULT_INVALID")
        return {
            "risk": risk,
            "local_tool_calls": state["local_tool_calls"] + 1,
        }

    def _invoke_risk_reviewer(self, state: WorkflowState) -> dict[str, Any]:
        with self._runtime.invoke_agent(peer_agent_id=PEER_AGENT_ID):
            review = review_risk(self._llm, state["scenario"])
        return {
            "peer_review": review,
            "llm_calls": state["llm_calls"] + 1,
            "a2a_calls": state["a2a_calls"] + 1,
        }

    def _guarded_action(self, state: WorkflowState) -> dict[str, Any]:
        result = self._action_tool.invoke(
            {
                "url": DEMO_ACTION_URL,
                "body": {"host": DEMO_HOST, "action": "quarantine"},
            }
        )
        denied = isinstance(result, dict) and result.get("agentguard") == "blocked"
        return {
            "action_result": result if isinstance(result, dict) else {"simulated": True},
            "outcome": Outcome.DENIED if denied else Outcome.APPROVED,
            "local_tool_calls": state["local_tool_calls"] + 1,
        }

    @staticmethod
    def _render_report(state: WorkflowState) -> dict[str, Any]:
        outcome = state.get("outcome", Outcome.NONE)
        if state["scenario"] is Scenario.HAPPY:
            outcome = Outcome.NORMAL
        elif state["scenario"] is Scenario.FAILURE:
            outcome = Outcome.DEGRADED
        report = " | ".join(
            (
                "SIMULATED",
                f"scenario={state['scenario'].value}",
                f"outcome={outcome.value}",
                f"risk={state['risk']['score']}",
            )
        )
        return {"outcome": outcome, "report": report}

    @staticmethod
    def _finish(state: WorkflowState) -> dict[str, Any]:
        _ = state
        return {}
