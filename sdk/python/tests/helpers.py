from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from opentelemetry.sdk.trace.export import SpanExporter, SpanExportResult
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from agentshark import AgentShark
from agentshark.config import SDKConfig
from agentshark.context import RuntimeContext


def environment(
    content_mode: str = "metadata",
    *,
    guard_enabled: bool = False,
    failure_mode: str = "closed",
) -> dict[str, str]:
    values = {
        "AGENTSHARK_TRACE_ENDPOINT": "http://127.0.0.1:4318/v1/traces",
        "AGENTSHARK_TRACE_INGEST_TOKEN": "trace-token-for-tests",
        "AGENTSHARK_TRACE_CONTENT_MODE": content_mode,
        "AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES": "1024",
        "AGENTSHARK_TRACE_BATCH_DELAY_MS": "1",
        "AGENTSHARK_TRACE_MAX_QUEUE_SIZE": "128",
        "AGENTSHARK_TRACE_MAX_EXPORT_BATCH_SIZE": "32",
        "AGENTSHARK_GUARD_ENABLED": "true" if guard_enabled else "false",
        "AGENTSHARK_GUARD_FAILURE_MODE": failure_mode,
        "AGENTSHARK_ENVIRONMENT": "test",
        "AGENTSHARK_USER_ID": "user-a",
    }
    if guard_enabled:
        values.update(
            {
                "AGENTGUARD_SERVER_URL": "http://127.0.0.1:38080",
                "AGENTGUARD_API_KEY": "guard-token-for-tests",
            }
        )
    return values


def config(content_mode: str = "metadata") -> SDKConfig:
    return SDKConfig.from_env(environment(content_mode))


@dataclass
class FakeGuard:
    attach_count: int = 0
    contexts: list[RuntimeContext] = field(default_factory=list)
    goal_metadata: list[dict[str, Any]] = field(default_factory=list)
    restore_count: int = 0
    close_count: int = 0
    deny_attach: bool = False
    restore_error: Exception | None = None
    guarded_tools: list[dict[str, Any]] = field(default_factory=list)

    def attach_langchain(self, agent: Any) -> Any:
        self.attach_count += 1
        if self.deny_attach:
            raise PermissionError("denied")
        return agent

    def guard_tool(
        self,
        call: Any,
        *,
        name: str,
        description: str,
        capabilities: Any,
    ) -> Any:
        self.guarded_tools.append(
            {
                "name": name,
                "description": description,
                "capabilities": tuple(capabilities),
            }
        )
        return call

    def bind_task(self, context: RuntimeContext, goal_metadata: dict[str, Any]) -> object:
        self.contexts.append(context)
        self.goal_metadata.append(dict(goal_metadata))
        return object()

    def restore_task(self, state: Any) -> None:
        assert state is not None
        self.restore_count += 1
        if self.restore_error is not None:
            raise self.restore_error

    def close(self) -> None:
        self.close_count += 1


class FailingExporter(SpanExporter):
    def __init__(self) -> None:
        self.exports = 0

    def export(self, spans: Any) -> SpanExportResult:
        self.exports += 1
        return SpanExportResult.FAILURE

    def shutdown(self) -> None:
        return None

    def force_flush(self, timeout_millis: int = 30_000) -> bool:
        return True


def runtime(
    content_mode: str = "metadata",
    guard: FakeGuard | None = None,
    exporter: SpanExporter | None = None,
) -> tuple[AgentShark, InMemorySpanExporter | SpanExporter, FakeGuard]:
    selected_exporter = exporter or InMemorySpanExporter()
    selected_guard = guard or FakeGuard()
    sdk = AgentShark(
        config(content_mode),
        agent_id="agent-a",
        session_id="session-a",
        _exporter=selected_exporter,
        _guard_adapter=selected_guard,
    )
    return sdk, selected_exporter, selected_guard
