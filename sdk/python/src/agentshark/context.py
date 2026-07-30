"""Task-local execution context shared by tracing integrations."""

from __future__ import annotations

from contextvars import ContextVar, Token
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class RuntimeContext:
    runtime_id: str
    agent_id: str
    session_id: str
    task_id: str
    trace_id: str
    root_span_id: str
    user_id: str | None
    environment: str


_CURRENT_CONTEXT: ContextVar[RuntimeContext | None] = ContextVar(
    "agentshark_runtime_context",
    default=None,
)


def get_current_context() -> RuntimeContext | None:
    """Return the active task context for this thread or asyncio Task."""

    return _CURRENT_CONTEXT.get()


def current_task() -> RuntimeContext | None:
    """Compatibility name for the active task context."""

    return get_current_context()


def _set_current_context(context: RuntimeContext) -> Token[RuntimeContext | None]:
    return _CURRENT_CONTEXT.set(context)


def _reset_current_context(token: Token[RuntimeContext | None]) -> None:
    _CURRENT_CONTEXT.reset(token)
