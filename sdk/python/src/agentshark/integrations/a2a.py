"""Explicit A2A spans, W3C propagation, and asynchronous Span Links."""

from __future__ import annotations

from collections.abc import Callable, Mapping, MutableMapping, Sequence
from contextvars import Token
from types import TracebackType
from typing import Any, Literal

from opentelemetry import context as otel_context
from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.trace import Link, Span, SpanContext, Status, StatusCode, Tracer
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from agentshark.tracing import COUNTABLE

_PROPAGATOR = TraceContextTextMapPropagator()


class AgentInvocation:
    def __init__(
        self,
        tracer: Tracer,
        *,
        peer_agent_id: str,
        interaction_id: str | None,
        links: Sequence[Link],
        detached: bool,
    ) -> None:
        self._tracer = tracer
        self._peer_agent_id = peer_agent_id
        self._attributes: dict[str, str | bool] = {
            "openinference.span.kind": "AGENT",
            "gen_ai.operation.name": "invoke_agent",
            "agentshark.peer_agent.id": peer_agent_id,
            COUNTABLE: True,
        }
        if interaction_id:
            self._attributes["agentshark.interaction.id"] = interaction_id
        self._parent_context = Context() if detached else None
        self._links = tuple(links)
        self._span: Span | None = None
        self._context: Context | None = None
        self._token: Token[Context] | None = None

    @property
    def span_context(self) -> SpanContext:
        if self._span is None:
            raise RuntimeError("AgentInvocation must be entered before reading its span context")
        return self._span.get_span_context()

    def inject(self, carrier: MutableMapping[str, str]) -> MutableMapping[str, str]:
        if self._context is None:
            raise RuntimeError("AgentInvocation must be entered before injecting Trace context")
        _PROPAGATOR.inject(carrier, context=self._context)
        return carrier

    def __enter__(self) -> AgentInvocation:
        if self._span is not None:
            raise RuntimeError("AgentInvocation context cannot be entered more than once")
        self._span = self._tracer.start_span(
            f"invoke_agent {self._peer_agent_id}",
            context=self._parent_context,
            attributes=self._attributes,
            links=self._links,
        )
        self._context = trace.set_span_in_context(self._span, self._parent_context)
        self._token = otel_context.attach(self._context)
        return self

    async def __aenter__(self) -> AgentInvocation:
        return self.__enter__()

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> Literal[False]:
        _ = (exc_type, traceback)
        if self._span is None:
            raise RuntimeError("AgentInvocation context was not entered")
        if self._token is not None:
            otel_context.detach(self._token)
            self._token = None
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


class RemoteContext:
    def __init__(self, carrier: Mapping[str, str]) -> None:
        self.context = _PROPAGATOR.extract(carrier)
        self._token: Token[Context] | None = None

    def __enter__(self) -> Context:
        self._token = otel_context.attach(self.context)
        return self.context

    async def __aenter__(self) -> Context:
        return self.__enter__()

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> Literal[False]:
        _ = (exc_type, exc_value, traceback)
        if self._token is not None:
            otel_context.detach(self._token)
            self._token = None
        return False

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> bool:
        return self.__exit__(exc_type, exc_value, traceback)


def link_from_carrier(
    carrier: Mapping[str, str], attributes: Mapping[str, Any] | None = None
) -> Link:
    context = _PROPAGATOR.extract(carrier)
    span_context = trace.get_current_span(context).get_span_context()
    if not span_context.is_valid:
        raise ValueError("carrier does not contain a valid W3C traceparent")
    return Link(span_context, attributes=attributes)


def call_agent(
    invocation: AgentInvocation,
    call: Callable[..., Any],
    *args: Any,
    carrier: MutableMapping[str, str] | None = None,
    **kwargs: Any,
) -> Any:
    with invocation:
        if carrier is not None:
            invocation.inject(carrier)
        return call(*args, **kwargs)


async def acall_agent(
    invocation: AgentInvocation,
    call: Callable[..., Any],
    *args: Any,
    carrier: MutableMapping[str, str] | None = None,
    **kwargs: Any,
) -> Any:
    async with invocation:
        if carrier is not None:
            invocation.inject(carrier)
        return await call(*args, **kwargs)
