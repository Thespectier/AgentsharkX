"""Unified AgentShark runtime API."""

from __future__ import annotations

import asyncio
import atexit
import hashlib
import json
import logging
import os
import threading
import uuid
from collections.abc import Callable, Mapping, MutableMapping, Sequence
from contextvars import Token
from types import TracebackType
from typing import Any, Literal

from opentelemetry import baggage, trace
from opentelemetry import context as otel_context
from opentelemetry.sdk.trace.export import SpanExporter
from opentelemetry.trace import Link, Status, StatusCode, format_span_id, format_trace_id
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from agentshark.config import ContentMode, SDKConfig, validate_identity
from agentshark.context import (
    RuntimeContext,
    _reset_current_context,
    _set_current_context,
)
from agentshark.errors import ConcurrentTaskError, RuntimeClosedError
from agentshark.integrations.a2a import (
    AgentInvocation,
    RemoteContext,
    acall_agent,
    call_agent,
    link_from_carrier,
)
from agentshark.integrations.agentguard import AgentGuardIntegration, GuardAdapter
from agentshark.integrations.langchain import attach_once
from agentshark.integrations.mcp import MCPCall, mark_mcp_tool, wrap_mcp_callable
from agentshark.tracing import (
    AGENT_ID,
    CONTENT_MODE,
    COUNTABLE,
    RUNTIME_ID,
    SESSION_ID,
    TASK_ID,
    TelemetryManager,
    TraceIdentity,
    get_telemetry_manager,
)

_TRACE_PROPAGATOR = TraceContextTextMapPropagator()
logger = logging.getLogger("agentshark")

TaskAttributeValue = str | bool | int | float
_DEMO_ATTRIBUTE_ROOT = "agentshark.demo"
_FORBIDDEN_DEMO_ATTRIBUTE_FRAGMENTS = (
    "api_key",
    "authorization",
    "cookie",
    "credential",
    "password",
    "secret",
    "token",
)


class AgentShark:
    """Own tracing, AgentGuard, task context, and framework instrumentation."""

    def __init__(
        self,
        config: SDKConfig,
        *,
        agent_id: str,
        session_id: str,
        user_id: str | None = None,
        _exporter: SpanExporter | None = None,
        _guard_adapter: GuardAdapter | None = None,
        _telemetry_manager: TelemetryManager | None = None,
        guard_plugin_config: Mapping[str, Any] | None = None,
        guard_sandbox_profile: Any | None = None,
    ) -> None:
        self.config = config
        self.agent_id = validate_identity(agent_id, "agent_id")
        self.session_id = validate_identity(session_id, "session_id")
        resolved_user_id = user_id if user_id is not None else config.user_id
        self.user_id = (
            validate_identity(resolved_user_id, "user_id") if resolved_user_id is not None else None
        )
        self.runtime_id = uuid.uuid4().hex
        self._state_lock = threading.RLock()
        self._active_task: object | None = None
        self._closed = False
        self._close_result = True
        self._manager = _telemetry_manager or get_telemetry_manager(config)
        self._manager.register_runtime(self.runtime_id, config, exporter=_exporter)
        try:
            self._manager.ensure_instrumentation()
            self._guard = _guard_adapter or AgentGuardIntegration(
                config,
                agent_id=self.agent_id,
                session_id=self.session_id,
                user_id=self.user_id,
                plugin_config=guard_plugin_config,
                guard_sandbox_profile=guard_sandbox_profile,
            )
        except Exception:
            self._manager.unregister_runtime(self.runtime_id)
            raise
        self._tracer = self._manager.tracer()
        atexit.register(self._atexit_close)

    @classmethod
    def from_env(
        cls,
        *,
        agent_id: str,
        session_id: str,
        user_id: str | None = None,
        environ: Mapping[str, str] | None = None,
        _exporter: SpanExporter | None = None,
        _guard_adapter: GuardAdapter | None = None,
        _telemetry_manager: TelemetryManager | None = None,
        guard_plugin_config: Mapping[str, Any] | None = None,
        guard_sandbox_profile: Any | None = None,
    ) -> AgentShark:
        config = SDKConfig.from_env(os.environ if environ is None else environ)
        return cls(
            config,
            agent_id=agent_id,
            session_id=session_id,
            user_id=user_id,
            _exporter=_exporter,
            _guard_adapter=_guard_adapter,
            _telemetry_manager=_telemetry_manager,
            guard_plugin_config=guard_plugin_config,
            guard_sandbox_profile=guard_sandbox_profile,
        )

    def attach_langchain(self, agent: Any) -> Any:
        self._require_open()
        self._manager.ensure_langchain_instrumented(required=True)
        return attach_once(agent, self.runtime_id, self._guard.attach_langchain)

    def task(
        self,
        *,
        task_id: str | None = None,
        goal: Any | None = None,
        attributes: Mapping[str, TaskAttributeValue] | None = None,
    ) -> _TaskScope:
        self._require_open()
        resolved_task_id = (
            validate_identity(task_id, "task_id")
            if task_id is not None
            else f"task_{uuid.uuid4().hex}"
        )
        return _TaskScope(self, resolved_task_id, goal, _demo_task_attributes(attributes))

    def guard_tool(
        self,
        call: Callable[..., Any],
        *,
        name: str | None = None,
        description: str = "",
        capabilities: Sequence[str] = (),
    ) -> Callable[..., Any]:
        """Wrap one explicit local callable with this runtime's AgentGuard session."""

        self._require_open()
        resolved_name = validate_identity(name or getattr(call, "__name__", None), "tool_name")
        resolved_description = description.strip()
        if len(resolved_description) > 512:
            raise ValueError("tool description must not exceed 512 characters")
        resolved_capabilities = tuple(
            validate_identity(capability, "capability") for capability in capabilities
        )
        return self._guard.guard_tool(
            call,
            name=resolved_name,
            description=resolved_description,
            capabilities=resolved_capabilities,
        )

    def mcp_call(
        self,
        *,
        server_name: str,
        method: str = "tools/call",
        tool_name: str | None = None,
        interaction_id: str | None = None,
    ) -> MCPCall:
        self._require_open()
        server_name = validate_identity(server_name, "server_name")
        method = validate_identity(method, "method")
        if tool_name is not None:
            tool_name = validate_identity(tool_name, "tool_name")
        if interaction_id is not None:
            interaction_id = validate_identity(interaction_id, "interaction_id")
        return MCPCall(
            self._tracer,
            server_name=server_name,
            method=method,
            tool_name=tool_name,
            interaction_id=interaction_id,
        )

    def wrap_mcp_tool(
        self,
        call: Callable[..., Any],
        *,
        server_name: str,
        method: str = "tools/call",
        tool_name: str | None = None,
        interaction_id: str | None = None,
    ) -> Callable[..., Any]:
        self._require_open()
        return wrap_mcp_callable(
            call,
            self._tracer,
            server_name=validate_identity(server_name, "server_name"),
            method=validate_identity(method, "method"),
            tool_name=(
                validate_identity(tool_name, "tool_name") if tool_name is not None else None
            ),
            interaction_id=(
                validate_identity(interaction_id, "interaction_id")
                if interaction_id is not None
                else None
            ),
        )

    def mark_mcp_tool(
        self,
        tool: Any,
        *,
        server_name: str,
        method: str = "tools/call",
        tool_name: str | None = None,
        interaction_id: str | None = None,
    ) -> Any:
        self._require_open()
        return mark_mcp_tool(
            tool,
            self._tracer,
            server_name=validate_identity(server_name, "server_name"),
            method=validate_identity(method, "method"),
            tool_name=(
                validate_identity(tool_name, "tool_name") if tool_name is not None else None
            ),
            interaction_id=(
                validate_identity(interaction_id, "interaction_id")
                if interaction_id is not None
                else None
            ),
        )

    def invoke_agent(
        self,
        *,
        peer_agent_id: str,
        interaction_id: str | None = None,
        links: Sequence[Link] = (),
        detached: bool = False,
    ) -> AgentInvocation:
        self._require_open()
        return AgentInvocation(
            self._tracer,
            peer_agent_id=validate_identity(peer_agent_id, "peer_agent_id"),
            interaction_id=(
                validate_identity(interaction_id, "interaction_id")
                if interaction_id is not None
                else None
            ),
            links=links,
            detached=detached,
        )

    def call_agent(
        self,
        *,
        peer_agent_id: str,
        call: Callable[..., Any],
        args: Sequence[Any] = (),
        kwargs: Mapping[str, Any] | None = None,
        carrier: MutableMapping[str, str] | None = None,
        interaction_id: str | None = None,
        links: Sequence[Link] = (),
        detached: bool = False,
    ) -> Any:
        invocation = self.invoke_agent(
            peer_agent_id=peer_agent_id,
            interaction_id=interaction_id,
            links=links,
            detached=detached,
        )
        return call_agent(invocation, call, *args, carrier=carrier, **dict(kwargs or {}))

    async def ainvoke_agent(
        self,
        *,
        peer_agent_id: str,
        call: Callable[..., Any],
        args: Sequence[Any] = (),
        kwargs: Mapping[str, Any] | None = None,
        carrier: MutableMapping[str, str] | None = None,
        interaction_id: str | None = None,
        links: Sequence[Link] = (),
        detached: bool = False,
    ) -> Any:
        invocation = self.invoke_agent(
            peer_agent_id=peer_agent_id,
            interaction_id=interaction_id,
            links=links,
            detached=detached,
        )
        return await acall_agent(invocation, call, *args, carrier=carrier, **dict(kwargs or {}))

    def inject_context(self, carrier: MutableMapping[str, str]) -> MutableMapping[str, str]:
        self._require_open()
        _TRACE_PROPAGATOR.inject(carrier)
        return carrier

    def remote_context(self, carrier: Mapping[str, str]) -> RemoteContext:
        self._require_open()
        return RemoteContext(carrier)

    def link_from_carrier(
        self,
        carrier: Mapping[str, str],
        attributes: Mapping[str, Any] | None = None,
    ) -> Link:
        self._require_open()
        return link_from_carrier(carrier, attributes)

    def flush(self, timeout_seconds: float | None = None) -> bool:
        self._require_open()
        return self._force_flush(timeout_seconds)

    async def aflush(self, timeout_seconds: float | None = None) -> bool:
        return await asyncio.to_thread(self.flush, timeout_seconds)

    def close(self, timeout_seconds: float | None = None) -> bool:
        with self._state_lock:
            if self._closed:
                return self._close_result
            if self._active_task is not None:
                raise ConcurrentTaskError("cannot close AgentShark while a task is active")
            self._closed = True

        flushed = False
        try:
            flushed = self._force_flush(timeout_seconds)
        finally:
            try:
                self._guard.close()
            finally:
                self._manager.unregister_runtime(self.runtime_id)
                try:
                    atexit.unregister(self._atexit_close)
                except Exception:
                    pass
                self._close_result = flushed
        return flushed

    def __enter__(self) -> AgentShark:
        self._require_open()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> Literal[False]:
        _ = (exc_type, exc_value, traceback)
        self.close()
        return False

    async def __aenter__(self) -> AgentShark:
        self._require_open()
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> bool:
        _ = (exc_type, exc_value, traceback)
        await asyncio.to_thread(self.close)
        return False

    def _begin_task(self) -> object:
        with self._state_lock:
            self._require_open_locked()
            if self._active_task is not None:
                raise ConcurrentTaskError(
                    "one AgentShark runtime allows only one active task; create a runtime per "
                    "worker or session"
                )
            token = object()
            self._active_task = token
            return token

    def _end_task(self, token: object) -> None:
        with self._state_lock:
            if self._active_task is token:
                self._active_task = None

    def _force_flush(self, timeout_seconds: float | None) -> bool:
        timeout = self.config.flush_timeout_seconds if timeout_seconds is None else timeout_seconds
        if timeout <= 0:
            raise ValueError("flush timeout must be greater than zero")
        return self._manager.force_flush(timeout)

    def _require_open(self) -> None:
        with self._state_lock:
            self._require_open_locked()

    def _require_open_locked(self) -> None:
        if self._closed:
            raise RuntimeClosedError("AgentShark runtime is closed")

    def _atexit_close(self) -> None:
        try:
            self.close()
        except Exception:
            return


class _TaskScope:
    def __init__(
        self,
        runtime: AgentShark,
        task_id: str,
        goal: Any | None,
        attributes: Mapping[str, TaskAttributeValue],
    ) -> None:
        self._runtime = runtime
        self._task_id = task_id
        self._goal = goal
        self._attributes = dict(attributes)
        self._active_token: object | None = None
        self._span: Any | None = None
        self._otel_token: Token[otel_context.Context] | None = None
        self._context_token: Any | None = None
        self._guard_state: Any | None = None
        self._trace_id: int | None = None

    def __enter__(self) -> RuntimeContext:
        if self._active_token is not None:
            raise ConcurrentTaskError("a task context manager cannot be entered twice")
        self._active_token = self._runtime._begin_task()
        try:
            trace_attributes, guard_metadata = _goal_attributes(
                self._goal,
                self._runtime.config.content_mode,
                self._runtime.config.payload_max_bytes,
            )
            attributes: dict[str, TaskAttributeValue] = {
                "agentshark.task.root": True,
                RUNTIME_ID: self._runtime.runtime_id,
                AGENT_ID: self._runtime.agent_id,
                SESSION_ID: self._runtime.session_id,
                TASK_ID: self._task_id,
                COUNTABLE: False,
                CONTENT_MODE: self._runtime.config.content_mode.value,
                **self._attributes,
                **trace_attributes,
            }
            if self._runtime.user_id is not None:
                attributes["agentshark.user.id"] = self._runtime.user_id
            self._span = self._runtime._tracer.start_span(
                "agentshark.task",
                attributes=attributes,
            )
            span_context = self._span.get_span_context()
            self._trace_id = span_context.trace_id
            trace_id = format_trace_id(span_context.trace_id)
            root_span_id = format_span_id(span_context.span_id)
            runtime_context = RuntimeContext(
                runtime_id=self._runtime.runtime_id,
                agent_id=self._runtime.agent_id,
                session_id=self._runtime.session_id,
                task_id=self._task_id,
                trace_id=trace_id,
                root_span_id=root_span_id,
                user_id=self._runtime.user_id,
                environment=self._runtime.config.environment,
            )
            identity = TraceIdentity(
                runtime_id=self._runtime.runtime_id,
                agent_id=self._runtime.agent_id,
                session_id=self._runtime.session_id,
                task_id=self._task_id,
                task_attributes=self._attributes,
            )
            self._runtime._manager.bind_trace(span_context.trace_id, identity)

            context = trace.set_span_in_context(self._span)
            for key, value in (
                (RUNTIME_ID, self._runtime.runtime_id),
                (AGENT_ID, self._runtime.agent_id),
                (SESSION_ID, self._runtime.session_id),
                (TASK_ID, self._task_id),
            ):
                context = baggage.set_baggage(key, value, context=context)
            self._otel_token = otel_context.attach(context)
            self._context_token = _set_current_context(runtime_context)
            self._guard_state = self._runtime._guard.bind_task(runtime_context, guard_metadata)
            return runtime_context
        except Exception:
            self._abort_enter()
            raise

    async def __aenter__(self) -> RuntimeContext:
        return self.__enter__()

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> Literal[False]:
        _ = (exc_type, traceback)
        guard_restore_error: Exception | None = None
        try:
            if self._span is not None:
                if exc_value is not None:
                    self._span.record_exception(exc_value)
                    self._span.set_status(Status(StatusCode.ERROR, _exception_type(exc_value)))
                else:
                    self._span.set_status(Status(StatusCode.OK))
                self._span.end()
        finally:
            guard_restore_error = self._cleanup()
        if guard_restore_error is not None:
            if exc_value is None:
                raise guard_restore_error
            logger.warning(
                "AgentGuard task context restore failed; preserving the agent business exception"
            )
        return False

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> bool:
        return self.__exit__(exc_type, exc_value, traceback)

    def _abort_enter(self) -> None:
        if self._span is not None:
            self._span.set_status(Status(StatusCode.ERROR, "task context initialization failed"))
            self._span.end()
        if self._cleanup() is not None:
            logger.warning(
                "AgentGuard task context restore failed; preserving the task initialization error"
            )

    def _cleanup(self) -> Exception | None:
        guard_restore_error: Exception | None = None
        try:
            self._runtime._guard.restore_task(self._guard_state)
        except Exception as exc:
            guard_restore_error = exc
        finally:
            if self._context_token is not None:
                _reset_current_context(self._context_token)
                self._context_token = None
            if self._otel_token is not None:
                otel_context.detach(self._otel_token)
                self._otel_token = None
            if self._trace_id is not None:
                self._runtime._manager.unbind_trace(
                    self._trace_id,
                    self._runtime.runtime_id,
                )
                self._trace_id = None
            if self._active_token is not None:
                self._runtime._end_task(self._active_token)
                self._active_token = None
        return guard_restore_error


def _goal_attributes(
    goal: Any | None,
    mode: ContentMode,
    max_bytes: int,
) -> tuple[dict[str, str | int], dict[str, Any]]:
    if goal is None or mode is ContentMode.NONE:
        return {}, {}
    serialized = _serialize_goal(goal)
    encoded = serialized.encode("utf-8")
    digest = hashlib.sha256(encoded).hexdigest()
    trace_attributes: dict[str, str | int] = {
        "agentshark.task.goal.type": type(goal).__name__,
        "agentshark.task.goal.length": len(encoded),
    }
    if mode is ContentMode.METADATA:
        trace_attributes["agentshark.task.goal.sha256"] = digest
        return trace_attributes, {"goal_length": len(encoded), "goal_sha256": digest}

    captured = _truncate_text(serialized, max_bytes)
    trace_attributes["agentshark.task.goal"] = captured
    return trace_attributes, {
        "goal": captured,
        "goal_capture_state": "truncated" if captured != serialized else "captured",
    }


def _demo_task_attributes(
    attributes: Mapping[str, TaskAttributeValue] | None,
) -> dict[str, TaskAttributeValue]:
    if attributes is None:
        return {}
    if len(attributes) > 16:
        raise ValueError("task attributes must not contain more than 16 entries")
    validated: dict[str, TaskAttributeValue] = {}
    for raw_key, value in attributes.items():
        key = str(raw_key).strip()
        if key != _DEMO_ATTRIBUTE_ROOT and not key.startswith(f"{_DEMO_ATTRIBUTE_ROOT}."):
            raise ValueError("task attributes must use the agentshark.demo namespace")
        lowered = key.lower()
        if any(fragment in lowered for fragment in _FORBIDDEN_DEMO_ATTRIBUTE_FRAGMENTS):
            raise ValueError("task attributes must not contain credential-like fields")
        if not isinstance(value, (str, bool, int, float)):
            raise TypeError("task attribute values must be scalar strings, booleans, or numbers")
        if isinstance(value, str) and len(value.encode("utf-8")) > 512:
            raise ValueError("string task attributes must not exceed 512 bytes")
        validated[key] = value
    return validated


def _serialize_goal(goal: Any) -> str:
    if isinstance(goal, str):
        return goal
    if isinstance(goal, bytes):
        return goal.decode("utf-8", errors="replace")
    return json.dumps(goal, ensure_ascii=True, sort_keys=True, default=str)


def _truncate_text(value: str, max_bytes: int) -> str:
    encoded = value.encode("utf-8")
    if len(encoded) <= max_bytes:
        return value
    suffix = "...[truncated]"
    budget = max(0, max_bytes - len(suffix.encode("utf-8")))
    return encoded[:budget].decode("utf-8", errors="ignore") + suffix


def _exception_type(error: BaseException) -> str:
    return f"{type(error).__module__}.{type(error).__qualname__}"
