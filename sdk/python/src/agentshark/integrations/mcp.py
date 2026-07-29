"""Explicit MCP tool classification and W3C context propagation."""

from __future__ import annotations

import functools
import inspect
from collections.abc import Callable
from contextvars import Token
from types import TracebackType
from typing import Any, Literal, TypeVar, cast

from opentelemetry import context as otel_context
from opentelemetry import trace
from opentelemetry.trace import Span, Status, StatusCode, Tracer

from agentshark.errors import MCPInstrumentationError
from agentshark.tracing import COUNTABLE

F = TypeVar("F", bound=Callable[..., Any])


class MCPCall:
    def __init__(
        self,
        tracer: Tracer,
        *,
        server_name: str,
        method: str,
        tool_name: str | None,
        interaction_id: str | None,
    ) -> None:
        self._tracer = tracer
        self._attributes: dict[str, str | bool] = {
            "openinference.span.kind": "TOOL",
            "agentshark.tool.kind": "mcp",
            "agentshark.mcp.server": server_name,
            "agentshark.mcp.method": method,
            COUNTABLE: method == "tools/call",
        }
        if tool_name:
            self._attributes["tool.name"] = tool_name
        if interaction_id:
            self._attributes["agentshark.interaction.id"] = interaction_id
        self._span: Span | None = None
        self._owns_span = False
        self._context_token: Token[otel_context.Context] | None = None

    def __enter__(self) -> MCPCall:
        span = _current_langchain_span()
        if span is None or not span.is_recording():
            span = self._tracer.start_span(
                str(self._attributes.get("tool.name") or self._attributes["agentshark.mcp.method"]),
                attributes=self._attributes,
            )
            self._owns_span = True
        else:
            span.set_attributes(self._attributes)
        self._span = span
        self._context_token = otel_context.attach(trace.set_span_in_context(span))
        return self

    async def __aenter__(self) -> MCPCall:
        return self.__enter__()

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> Literal[False]:
        _ = (exc_type, traceback)
        if self._context_token is not None:
            otel_context.detach(self._context_token)
            self._context_token = None
        if self._span is not None and self._owns_span:
            if exc_value is not None:
                self._span.record_exception(exc_value)
                self._span.set_status(
                    Status(
                        StatusCode.ERROR,
                        f"{type(exc_value).__module__}.{type(exc_value).__qualname__}",
                    )
                )
            else:
                self._span.set_status(Status(StatusCode.OK))
            self._span.end()
        return False

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> bool:
        return self.__exit__(exc_type, exc_value, traceback)


def wrap_mcp_callable(
    call: F,
    tracer: Tracer,
    *,
    server_name: str,
    method: str,
    tool_name: str | None,
    interaction_id: str | None,
) -> F:
    marker = (server_name, method, tool_name, interaction_id)
    existing = getattr(call, "__agentshark_mcp_marker__", None)
    if existing is not None:
        if existing != marker:
            raise MCPInstrumentationError("MCP callable is already marked with different metadata")
        return call

    if inspect.iscoroutinefunction(call):

        @functools.wraps(call)
        async def async_wrapper(*args: Any, **kwargs: Any) -> Any:
            async with MCPCall(
                tracer,
                server_name=server_name,
                method=method,
                tool_name=tool_name,
                interaction_id=interaction_id,
            ):
                return await call(*args, **kwargs)

        wrapped = async_wrapper
    else:

        @functools.wraps(call)
        def sync_wrapper(*args: Any, **kwargs: Any) -> Any:
            with MCPCall(
                tracer,
                server_name=server_name,
                method=method,
                tool_name=tool_name,
                interaction_id=interaction_id,
            ):
                return call(*args, **kwargs)

        wrapped = sync_wrapper

    cast(Any, wrapped).__agentshark_mcp_marker__ = marker
    return cast(F, wrapped)


def mark_mcp_tool(
    tool: Any,
    tracer: Tracer,
    *,
    server_name: str,
    method: str,
    tool_name: str | None,
    interaction_id: str | None,
) -> Any:
    if inspect.isfunction(tool) or inspect.ismethod(tool):
        return wrap_mcp_callable(
            tool,
            tracer,
            server_name=server_name,
            method=method,
            tool_name=tool_name or getattr(tool, "__name__", None),
            interaction_id=interaction_id,
        )

    resolved_name = tool_name or getattr(tool, "name", None)
    candidates = [name for name in ("func", "coroutine") if callable(getattr(tool, name, None))]
    if not candidates:
        candidates = [name for name in ("invoke", "ainvoke") if callable(getattr(tool, name, None))]
    if not candidates:
        raise MCPInstrumentationError("MCP tool exposes no supported callable entrypoint")

    changed = False
    for name in candidates:
        original = getattr(tool, name)
        wrapped = wrap_mcp_callable(
            original,
            tracer,
            server_name=server_name,
            method=method,
            tool_name=resolved_name,
            interaction_id=interaction_id,
        )
        if wrapped is original:
            continue
        try:
            setattr(tool, name, wrapped)
        except (AttributeError, TypeError, ValueError) as exc:
            raise MCPInstrumentationError(
                f"MCP tool entrypoint {name!r} cannot be wrapped in place"
            ) from exc
        changed = True
    if not changed and not all(
        getattr(getattr(tool, name), "__agentshark_mcp_marker__", None) is not None
        for name in candidates
    ):
        raise MCPInstrumentationError("MCP tool could not be marked")
    return tool


def _current_langchain_span() -> Span | None:
    try:
        from openinference.instrumentation.langchain import get_current_span

        return get_current_span()
    except (ImportError, ModuleNotFoundError, RuntimeError):
        return None
