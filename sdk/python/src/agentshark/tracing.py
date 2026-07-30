"""Process-wide OpenTelemetry setup and per-runtime export routing."""

from __future__ import annotations

import importlib.util
import logging
import re
import threading
from collections import defaultdict
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from typing import cast

from opentelemetry import baggage
from opentelemetry.context import Context
from opentelemetry.exporter.otlp.proto.http import Compression
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import Event, ReadableSpan, Span, SpanProcessor, TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor, SpanExporter, SpanExportResult
from opentelemetry.trace import Link, Status, Tracer
from opentelemetry.util.types import AttributeValue

from agentshark.config import ContentMode, SDKConfig
from agentshark.context import get_current_context
from agentshark.errors import ConfigurationError

logger = logging.getLogger("agentshark")

RUNTIME_ID = "agentshark.runtime.id"
AGENT_ID = "agentshark.agent.id"
SESSION_ID = "agentshark.session.id"
TASK_ID = "agentshark.task.id"
COUNTABLE = "agentshark.countable"
CONTENT_MODE = "agentshark.content.mode"
CONTENT_STATE = "agentshark.content.state"

_BAGGAGE_KEYS = (RUNTIME_ID, AGENT_ID, SESSION_ID, TASK_ID)
_INTERACTION_KINDS = {"LLM", "TOOL", "RETRIEVER"}
_CONTENT_EXACT = {
    "input.value",
    "output.value",
    "metadata",
    "llm.invocation_parameters",
    "llm.prompts",
    "llm.choices",
    "llm.input_messages",
    "llm.output_messages",
    "llm.tools",
    "llm.prompt_template.template",
    "llm.prompt_template.variables",
    "gen_ai.completion",
    "gen_ai.input.messages",
    "gen_ai.output.messages",
    "gen_ai.prompt",
    "gen_ai.system.instructions",
    "gen_ai.tool.call.arguments",
    "gen_ai.tool.call.result",
    "retrieval.documents",
    "embedding.embeddings",
    "reranker.input_documents",
    "reranker.output_documents",
    "tool.arguments",
    "tool.input",
    "tool.output",
    "tool.parameters",
    "tool.result",
    "tool_call.function.arguments",
    "agentshark.task.goal",
    "exception.message",
    "exception.stacktrace",
}
_CONTENT_PREFIXES = (
    "llm.input_messages.",
    "llm.output_messages.",
    "llm.prompts.",
    "llm.choices.",
    "llm.tools.",
    "embedding.embeddings.",
    "retrieval.documents.",
    "reranker.input_documents.",
    "reranker.output_documents.",
    "gen_ai.input.messages",
    "gen_ai.output.messages",
    "gen_ai.retrieval.",
)
_SECRET_KEY_FRAGMENTS = {
    "access.token",
    "api.key",
    "apikey",
    "authorization",
    "client.secret",
    "cookie",
    "credential",
    "credentials",
    "password",
    "passwd",
    "proxy.authorization",
    "refresh.token",
    "secret",
    "secret.access.key",
    "session.key",
    "set.cookie",
}
_SECRET_VALUE_PATTERN = re.compile(
    r"""(?i)(?:authorization|api[-_ ]?key|password|secret|cookie)["']?\s*[:=]"""
    r"|(?i:bearer\s+\S+)"
)


@dataclass(frozen=True, slots=True)
class TraceIdentity:
    runtime_id: str
    agent_id: str
    session_id: str
    task_id: str
    task_attributes: Mapping[str, AttributeValue]


@dataclass(slots=True)
class _Route:
    exporter: SpanExporter
    content_mode: ContentMode
    payload_max_bytes: int
    resource: Resource
    warning_emitted: bool = False


class _RoutingExporter(SpanExporter):
    def __init__(self) -> None:
        self._routes: dict[str, _Route] = {}
        self._lock = threading.RLock()

    def register(self, runtime_id: str, route: _Route) -> None:
        with self._lock:
            if runtime_id in self._routes:
                raise ConfigurationError("runtime exporter is already registered")
            self._routes[runtime_id] = route

    def unregister(self, runtime_id: str) -> None:
        with self._lock:
            route = self._routes.pop(runtime_id, None)
        if route is None:
            return
        try:
            route.exporter.shutdown()
        except Exception:
            self._warn_once(route)

    def export(self, spans: Sequence[ReadableSpan]) -> SpanExportResult:
        grouped: dict[str, list[ReadableSpan]] = defaultdict(list)
        with self._lock:
            routes = dict(self._routes)

        for span in spans:
            runtime_id = str((span.attributes or {}).get(RUNTIME_ID) or "")
            route = routes.get(runtime_id)
            if route is None:
                continue
            grouped[runtime_id].append(_sanitize_span(span, route))

        result = SpanExportResult.SUCCESS
        for runtime_id, batch in grouped.items():
            route = routes[runtime_id]
            try:
                exported = route.exporter.export(batch)
            except Exception:
                exported = SpanExportResult.FAILURE
            if exported is not SpanExportResult.SUCCESS:
                result = SpanExportResult.FAILURE
                self._warn_once(route)
        return result

    def force_flush(self, timeout_millis: int = 30_000) -> bool:
        with self._lock:
            routes = tuple(self._routes.values())
        flushed = True
        for route in routes:
            try:
                flushed = bool(route.exporter.force_flush(timeout_millis)) and flushed
            except Exception:
                flushed = False
                self._warn_once(route)
        return flushed

    def shutdown(self) -> None:
        with self._lock:
            runtime_ids = tuple(self._routes)
        for runtime_id in runtime_ids:
            self.unregister(runtime_id)

    @staticmethod
    def _warn_once(route: _Route) -> None:
        if route.warning_emitted:
            return
        route.warning_emitted = True
        logger.warning(
            "Agentshark trace export is unavailable; agent execution will continue without blocking"
        )


class _ContextEnricher(SpanProcessor):
    def __init__(
        self,
        trace_identities: dict[int, dict[str, TraceIdentity]],
        lock: threading.RLock,
    ) -> None:
        self._trace_identities = trace_identities
        self._lock = lock

    def on_start(self, span: Span, parent_context: Context | None = None) -> None:
        span_attributes = span.attributes or {}
        values: dict[str, AttributeValue] = {
            key: str(span_attributes[key])
            for key in _BAGGAGE_KEYS
            if span_attributes.get(key) is not None
        }
        for key in _BAGGAGE_KEYS:
            value = baggage.get_baggage(key, context=parent_context)
            if value is not None:
                values.setdefault(key, str(value))

        current = get_current_context()
        if current is not None:
            values.setdefault(RUNTIME_ID, current.runtime_id)
            values.setdefault(AGENT_ID, current.agent_id)
            values.setdefault(SESSION_ID, current.session_id)
            values.setdefault(TASK_ID, current.task_id)

        span_context = span.get_span_context()
        with self._lock:
            identities = self._trace_identities.get(span_context.trace_id, {})
            runtime_id_value = values.get(RUNTIME_ID)
            if runtime_id_value is not None:
                identity = identities.get(str(runtime_id_value))
            elif len(identities) == 1:
                identity = next(iter(identities.values()))
            else:
                identity = None
        if identity is not None:
            values.setdefault(RUNTIME_ID, identity.runtime_id)
            values.setdefault(AGENT_ID, identity.agent_id)
            values.setdefault(SESSION_ID, identity.session_id)
            values.setdefault(TASK_ID, identity.task_id)
            for key, value in identity.task_attributes.items():
                if key not in span_attributes:
                    values.setdefault(key, value)
        if values:
            span.set_attributes(values)

    def on_end(self, span: ReadableSpan) -> None:
        _ = span

    def shutdown(self) -> None:
        return None

    def force_flush(self, timeout_millis: int = 30_000) -> bool:
        _ = timeout_millis
        return True


class TelemetryManager:
    """One provider/instrumentor pair with isolated exporters per runtime."""

    def __init__(self, config: SDKConfig) -> None:
        self._batch_config = (
            config.batch_schedule_delay_ms,
            config.batch_max_queue_size,
            config.batch_max_export_size,
        )
        self._routing_exporter = _RoutingExporter()
        self._trace_identities: dict[int, dict[str, TraceIdentity]] = {}
        self._lock = threading.RLock()
        self._provider = TracerProvider(
            resource=Resource.create(
                {
                    "service.name": "agentshark-python-sdk",
                    "service.version": "0.1.0",
                    "agentshark.agentguard.revision": ("4b755fb4a4a2763b7e817b3d0220fe5c22187b59"),
                }
            )
        )
        self._provider.add_span_processor(_ContextEnricher(self._trace_identities, self._lock))
        self._provider.add_span_processor(
            BatchSpanProcessor(
                self._routing_exporter,
                schedule_delay_millis=config.batch_schedule_delay_ms,
                max_queue_size=config.batch_max_queue_size,
                max_export_batch_size=config.batch_max_export_size,
                export_timeout_millis=int(config.export_timeout_seconds * 1_000),
            )
        )
        self._instrument_lock = threading.Lock()
        self._langchain_attempted = False
        self._langchain_initialized = False
        self._langchain_failure: str | None = None
        self._mcp_attempted = False
        self._mcp_initialized = False
        self._langchain_initializations = 0
        self._mcp_initializations = 0

    def verify_compatible(self, config: SDKConfig) -> None:
        candidate = (
            config.batch_schedule_delay_ms,
            config.batch_max_queue_size,
            config.batch_max_export_size,
        )
        if candidate != self._batch_config:
            raise ConfigurationError(
                "all AgentShark runtimes in one process must use identical batch settings"
            )

    @property
    def provider(self) -> TracerProvider:
        return self._provider

    def tracer(self) -> Tracer:
        return self._provider.get_tracer("agentshark.sdk", "0.1.0")

    def register_runtime(
        self,
        runtime_id: str,
        config: SDKConfig,
        *,
        exporter: SpanExporter | None = None,
    ) -> None:
        resolved_exporter = exporter or OTLPSpanExporter(
            endpoint=config.otlp_endpoint,
            headers={"Authorization": f"Bearer {config.ingest_token}"},
            timeout=config.export_timeout_seconds,
            compression=Compression.Gzip,
        )
        resource = Resource.create(
            {
                "service.name": config.service_name,
                "deployment.environment.name": config.environment,
            }
        )
        self._routing_exporter.register(
            runtime_id,
            _Route(
                exporter=resolved_exporter,
                content_mode=config.content_mode,
                payload_max_bytes=config.payload_max_bytes,
                resource=resource,
            ),
        )

    def unregister_runtime(self, runtime_id: str) -> None:
        self._routing_exporter.unregister(runtime_id)

    def bind_trace(self, trace_id: int, identity: TraceIdentity) -> None:
        with self._lock:
            identities = self._trace_identities.setdefault(trace_id, {})
            identities[identity.runtime_id] = identity

    def unbind_trace(self, trace_id: int, runtime_id: str) -> None:
        with self._lock:
            identities = self._trace_identities.get(trace_id)
            if identities is None:
                return
            identities.pop(runtime_id, None)
            if not identities:
                self._trace_identities.pop(trace_id, None)

    def force_flush(self, timeout_seconds: float) -> bool:
        return bool(self._provider.force_flush(timeout_millis=int(timeout_seconds * 1_000)))

    def ensure_instrumentation(self) -> None:
        self.ensure_langchain_instrumented(required=False)
        self.ensure_mcp_instrumented()

    def ensure_langchain_instrumented(self, *, required: bool) -> bool:
        with self._instrument_lock:
            if self._langchain_attempted:
                if required and not self._langchain_initialized:
                    raise ConfigurationError(
                        self._langchain_failure
                        or "LangChain automatic tracing is unavailable"
                    )
                return self._langchain_initialized
            self._langchain_attempted = True
            if importlib.util.find_spec("langchain_core") is None:
                self._langchain_failure = (
                    "langchain-core is required before attach_langchain() can be used"
                )
                if required:
                    raise ConfigurationError(self._langchain_failure)
                return False

            from openinference.instrumentation import TraceConfig
            from openinference.instrumentation.langchain import LangChainInstrumentor

            instrumentor = LangChainInstrumentor()
            if instrumentor.is_instrumented_by_opentelemetry:
                self._langchain_failure = (
                    "LangChain is already instrumented by another owner; "
                    "Agentshark cannot guarantee Trace export through its provider"
                )
                if required:
                    raise ConfigurationError(self._langchain_failure)
                return False
            instrumentor.instrument(
                tracer_provider=self._provider,
                config=TraceConfig(enable_genai_semconv=True),
            )
            if not instrumentor.is_instrumented_by_opentelemetry:
                self._langchain_failure = (
                    "LangChain instrumentation did not initialize through the Agentshark provider"
                )
                if required:
                    raise ConfigurationError(self._langchain_failure)
                return False
            self._langchain_initialized = True
            self._langchain_initializations += 1
            return True

    def ensure_mcp_instrumented(self) -> bool:
        with self._instrument_lock:
            if self._mcp_attempted:
                return self._mcp_initialized
            self._mcp_attempted = True
            if importlib.util.find_spec("mcp") is None:
                return False

            from openinference.instrumentation.mcp import MCPInstrumentor

            MCPInstrumentor().instrument()
            self._mcp_initialized = True
            self._mcp_initializations += 1
            return True

    @property
    def instrumentation_counts(self) -> tuple[int, int]:
        return self._langchain_initializations, self._mcp_initializations


_MANAGER: TelemetryManager | None = None
_MANAGER_LOCK = threading.Lock()


def get_telemetry_manager(config: SDKConfig) -> TelemetryManager:
    global _MANAGER
    with _MANAGER_LOCK:
        if _MANAGER is None:
            _MANAGER = TelemetryManager(config)
        else:
            _MANAGER.verify_compatible(config)
        return _MANAGER


def _sanitize_span(span: ReadableSpan, route: _Route) -> ReadableSpan:
    attributes = dict(span.attributes or {})
    attributes = _classify_countable(attributes, span)
    attributes, truncated, redacted, content_seen = _sanitize_attributes(
        attributes,
        route.content_mode,
        route.payload_max_bytes,
    )
    events, event_truncated, event_redacted, event_content_seen = _sanitize_events(
        span.events,
        route.content_mode,
        route.payload_max_bytes,
    )
    links, link_truncated, link_redacted, link_content_seen = _sanitize_links(
        span.links,
        route.content_mode,
        route.payload_max_bytes,
    )
    truncated = truncated or event_truncated
    truncated = truncated or link_truncated
    redacted = redacted or event_redacted
    redacted = redacted or link_redacted
    content_seen = content_seen or event_content_seen
    content_seen = content_seen or link_content_seen

    status = span.status
    status_description = status.description
    if status_description:
        content_seen = True
        if route.content_mode is not ContentMode.FULL or _contains_secret(status_description):
            status = Status(status.status_code, "__REDACTED__")
            redacted = True
        else:
            description, status_truncated = _truncate_attribute(
                status_description,
                route.payload_max_bytes,
            )
            status = Status(status.status_code, cast(str, description))
            truncated = truncated or status_truncated

    if route.content_mode is ContentMode.FULL:
        if not content_seen and not redacted:
            attributes[CONTENT_STATE] = "not_collected"
        elif truncated:
            attributes[CONTENT_STATE] = "truncated"
        elif redacted:
            attributes[CONTENT_STATE] = "redacted"
        else:
            attributes[CONTENT_STATE] = "captured"
    elif route.content_mode is ContentMode.METADATA:
        attributes[CONTENT_STATE] = "redacted" if content_seen or redacted else "not_collected"
    else:
        attributes[CONTENT_STATE] = "not_collected"
    attributes[CONTENT_MODE] = route.content_mode.value

    return ReadableSpan(
        name=span.name,
        context=span.context,
        parent=span.parent,
        resource=span.resource.merge(route.resource),
        attributes=attributes,
        events=events,
        links=links,
        kind=span.kind,
        status=status,
        start_time=span.start_time,
        end_time=span.end_time,
        instrumentation_scope=span.instrumentation_scope,
    )


def _classify_countable(
    attributes: dict[str, AttributeValue],
    span: ReadableSpan,
) -> dict[str, AttributeValue]:
    explicit = attributes.get(COUNTABLE)
    has_explicit = isinstance(explicit, bool)
    countable = bool(explicit) if has_explicit else False
    scope_name = span.instrumentation_scope.name if span.instrumentation_scope else ""
    oi_kind = str(attributes.get("openinference.span.kind") or "").upper()
    if (
        not has_explicit
        and scope_name == "openinference.instrumentation.langchain"
        and oi_kind in _INTERACTION_KINDS
    ):
        countable = True

    tool_kind = str(attributes.get("agentshark.tool.kind") or "")
    mcp_method = str(attributes.get("agentshark.mcp.method") or "")
    if tool_kind == "mcp" and mcp_method != "tools/call":
        countable = False

    operation = str(attributes.get("gen_ai.operation.name") or "")
    peer_agent = str(attributes.get("agentshark.peer_agent.id") or "")
    if operation == "invoke_agent" or peer_agent:
        countable = operation == "invoke_agent" and bool(peer_agent)
    attributes[COUNTABLE] = countable
    return attributes


def _sanitize_attributes(
    attributes: Mapping[str, AttributeValue],
    mode: ContentMode,
    max_bytes: int,
) -> tuple[dict[str, AttributeValue], bool, bool, bool]:
    sanitized: dict[str, AttributeValue] = {}
    truncated = False
    redacted = False
    content_seen = False
    for key, value in attributes.items():
        content = _is_content_key(key)
        content_seen = content_seen or content or key.startswith("agentshark.task.goal.")
        if mode is ContentMode.NONE and not _allowed_in_none(key):
            continue
        if _is_secret_key(key):
            sanitized[key] = "__REDACTED__"
            redacted = True
            continue
        if content and mode is not ContentMode.FULL:
            continue
        value, secret_redacted = _redact_secret_value(value)
        if secret_redacted:
            sanitized[key] = value
            redacted = True
            continue
        if content:
            value, did_truncate = _truncate_attribute(value, max_bytes)
            truncated = truncated or did_truncate
        sanitized[key] = value
    return sanitized, truncated, redacted, content_seen


def _sanitize_events(
    events: Sequence[Event],
    mode: ContentMode,
    max_bytes: int,
) -> tuple[tuple[Event, ...], bool, bool, bool]:
    sanitized: list[Event] = []
    truncated = False
    redacted = False
    content_seen = False
    for event in events:
        attributes = dict(event.attributes or {})
        if event.name == "exception" and mode is not ContentMode.FULL:
            attributes.pop("exception.stacktrace", None)
            if "exception.message" in attributes:
                attributes["exception.message"] = "__REDACTED__"
        filtered, event_truncated, event_redacted, event_content_seen = _sanitize_attributes(
            attributes,
            mode,
            max_bytes,
        )
        truncated = truncated or event_truncated
        redacted = redacted or event_redacted
        content_seen = content_seen or event_content_seen
        sanitized.append(Event(event.name, filtered, event.timestamp))
    return tuple(sanitized), truncated, redacted, content_seen


def _sanitize_links(
    links: Sequence[Link],
    mode: ContentMode,
    max_bytes: int,
) -> tuple[tuple[Link, ...], bool, bool, bool]:
    sanitized: list[Link] = []
    truncated = False
    redacted = False
    content_seen = False
    for link in links:
        attributes, link_truncated, link_redacted, link_content_seen = _sanitize_attributes(
            dict(link.attributes or {}),
            mode,
            max_bytes,
        )
        truncated = truncated or link_truncated
        redacted = redacted or link_redacted
        content_seen = content_seen or link_content_seen
        sanitized.append(Link(link.context, attributes=attributes))
    return tuple(sanitized), truncated, redacted, content_seen


def _allowed_in_none(key: str) -> bool:
    if key.startswith("agentshark."):
        return key != "agentshark.task.goal"
    return key in {"openinference.span.kind", "gen_ai.operation.name"}


def _is_content_key(key: str) -> bool:
    return key in _CONTENT_EXACT or key.startswith(_CONTENT_PREFIXES)


def _is_secret_key(key: str) -> bool:
    canonical = re.sub(r"[-_/]+", ".", key.strip().lower())
    canonical = re.sub(r"\.+", ".", canonical).strip(".")
    padded = f".{canonical}."
    return any(f".{fragment}." in padded for fragment in _SECRET_KEY_FRAGMENTS)


def _contains_secret(value: str) -> bool:
    return bool(_SECRET_VALUE_PATTERN.search(value))


def _redact_secret_value(value: AttributeValue) -> tuple[AttributeValue, bool]:
    if isinstance(value, str):
        if _contains_secret(value):
            return "__REDACTED__", True
        return value, False
    if isinstance(value, Sequence) and not isinstance(value, (bytes, bytearray, str)):
        items: list[str | bool | int | float] = []
        redacted = False
        for item in value:
            if isinstance(item, str) and _contains_secret(item):
                items.append("__REDACTED__")
                redacted = True
            else:
                items.append(item)
        return cast(AttributeValue, tuple(items)), redacted
    return value, False


def _truncate_attribute(value: AttributeValue, max_bytes: int) -> tuple[AttributeValue, bool]:
    if isinstance(value, str):
        encoded = value.encode("utf-8")
        if len(encoded) <= max_bytes:
            return value, False
        suffix = "...[truncated]"
        budget = max(0, max_bytes - len(suffix.encode("utf-8")))
        prefix = encoded[:budget].decode("utf-8", errors="ignore")
        return prefix + suffix, True
    if isinstance(value, Sequence) and not isinstance(value, (bytes, bytearray, str)):
        items: list[str | bool | int | float] = []
        truncated = False
        for item in value:
            if isinstance(item, str):
                truncated_item, item_truncated = _truncate_attribute(item, max_bytes)
                truncated = truncated or item_truncated
                items.append(cast(str, truncated_item))
            else:
                items.append(item)
        return cast(AttributeValue, tuple(items)), truncated
    return value, False
