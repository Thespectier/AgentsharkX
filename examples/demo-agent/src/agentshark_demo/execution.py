"""Per-Run runtime construction and deterministic workflow execution."""

from __future__ import annotations

from collections.abc import Callable

from agentguard.sandbox import PermissionProfile
from agentshark import AgentShark

from agentshark_demo.config import DemoSettings
from agentshark_demo.llm import GatewayFixtureLLM, StageLLMClient
from agentshark_demo.mcp_client import MCPFixtureClient
from agentshark_demo.models import DemoExecutionResult, RunnerStartRequest
from agentshark_demo.scenarios import FIXTURE_VERSION, ROOT_AGENT_ID
from agentshark_demo.workflow import DemoWorkflow, ExecutionHooks, LLMClient, MCPClient

DEMO_PLUGIN_CONFIG = {
    "phases": {
        "tool_before": {
            "client": [],
            "server": [{"name": "demo_tripwire", "env": {}}],
        },
        "llm_before": {"client": [], "server": []},
        "llm_after": {"client": [], "server": []},
        "tool_after": {"client": [], "server": []},
        "global": {"client": [], "server": []},
    }
}

RuntimeFactory = Callable[[RunnerStartRequest], AgentShark]
LLMFactory = Callable[[AgentShark, DemoSettings], LLMClient]
MCPFactory = Callable[[DemoSettings], MCPClient]


def execute_demo(
    request: RunnerStartRequest,
    *,
    settings: DemoSettings | None = None,
    hooks: ExecutionHooks | None = None,
    runtime_factory: RuntimeFactory | None = None,
    llm_factory: LLMFactory | None = None,
    mcp_factory: MCPFactory | None = None,
) -> DemoExecutionResult:
    selected_settings = settings or DemoSettings.from_env()
    runtime = (runtime_factory or _runtime)(request)
    try:
        llm = (llm_factory or _llm)(runtime, selected_settings)
        mcp = (mcp_factory or _mcp)(selected_settings)
        workflow = DemoWorkflow(
            runtime=runtime,
            llm=llm,
            mcp=mcp,
            delay_ms=request.delay_ms,
            hooks=hooks,
        )
        with runtime.task(
            task_id=request.task_id,
            goal=f"deterministic demo scenario {request.scenario.value}",
            attributes={
                "agentshark.demo": True,
                "agentshark.demo.run_id": str(request.run_id),
                "agentshark.demo.scenario": request.scenario.value,
                "agentshark.demo.fixture_version": FIXTURE_VERSION,
            },
        ) as context:
            if hooks is not None:
                hooks.on_identity(context.trace_id, context.root_span_id)
            return workflow.run(request.scenario, context.trace_id, context.root_span_id)
    finally:
        runtime.close()


def _runtime(request: RunnerStartRequest) -> AgentShark:
    return AgentShark.from_env(
        agent_id=ROOT_AGENT_ID,
        session_id=request.session_id,
        guard_plugin_config=DEMO_PLUGIN_CONFIG,
        guard_sandbox_profile=_demo_sandbox_profile(),
    )


def _demo_sandbox_profile() -> PermissionProfile:
    return PermissionProfile(
        allowed_domains=["quarantine.example.com"],
        allow_subprocess=False,
        allow_network=True,
        allow_write=False,
    )


def _llm(runtime: AgentShark, settings: DemoSettings) -> StageLLMClient:
    return StageLLMClient(
        GatewayFixtureLLM(
            base_url=settings.llm_base_url,
            model_name=settings.llm_model,
            timeout_seconds=settings.request_timeout_seconds,
            inject_context=runtime.inject_context,
        )
    )


def _mcp(settings: DemoSettings) -> MCPFixtureClient:
    return MCPFixtureClient(settings.mcp_url)
