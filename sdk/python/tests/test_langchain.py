from __future__ import annotations

from types import SimpleNamespace
from typing import Any

import pytest
from helpers import config, runtime
from langchain_core.documents import Document
from langchain_core.language_models.fake import FakeListLLM
from langchain_core.retrievers import BaseRetriever
from langchain_core.tools import tool
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.sdk.util.instrumentation import InstrumentationScope

from agentshark.errors import AgentSharkConfigurationError
from agentshark.tracing import TelemetryManager, _classify_countable


class StaticRetriever(BaseRetriever):
    def _get_relevant_documents(self, query: str, *, run_manager: Any) -> list[Document]:
        _ = run_manager
        return [Document(page_content=f"private document for {query}")]


def test_langchain_llm_tool_and_retriever_spans_are_automatic_and_countable() -> None:
    sdk, exporter, _ = runtime("metadata")

    @tool
    def uppercase(value: str) -> str:
        """Uppercase a value."""

        return value.upper()

    with sdk.task(task_id="langchain-task"):
        assert FakeListLLM(responses=["private completion"]).invoke("private prompt") == (
            "private completion"
        )
        assert uppercase.invoke({"value": "private tool argument"}) == "PRIVATE TOOL ARGUMENT"
        assert StaticRetriever().invoke("private query")[0].page_content.startswith("private")
    sdk.flush()

    assert isinstance(exporter, InMemorySpanExporter)
    spans = exporter.get_finished_spans()
    interactions = {
        str(span.attributes.get("openinference.span.kind")): span
        for span in spans
        if span.attributes.get("openinference.span.kind") in {"LLM", "TOOL", "RETRIEVER"}
    }
    assert set(interactions) == {"LLM", "TOOL", "RETRIEVER"}
    assert all(span.attributes["agentshark.countable"] is True for span in interactions.values())
    assert all(
        span.attributes["agentshark.task.id"] == "langchain-task" for span in interactions.values()
    )
    rendered = repr([span.attributes for span in interactions.values()])
    assert "private prompt" not in rendered
    assert "private completion" not in rendered
    assert "private tool argument" not in rendered
    assert "private query" not in rendered
    sdk.close()


def test_instrumentor_initializes_only_once_for_multiple_runtimes() -> None:
    left, _, _ = runtime()
    right, _, _ = runtime()
    left._manager.ensure_langchain_instrumented(required=True)
    right._manager.ensure_langchain_instrumented(required=True)
    langchain_count, _ = left._manager.instrumentation_counts
    assert langchain_count == 1
    left.close()
    right.close()


def test_explicit_non_countable_overrides_langchain_scope_inference() -> None:
    attributes = {
        "agentshark.countable": False,
        "openinference.span.kind": "TOOL",
    }
    span = SimpleNamespace(
        instrumentation_scope=InstrumentationScope("openinference.instrumentation.langchain")
    )
    classified = _classify_countable(attributes, span)  # type: ignore[arg-type]
    assert classified["agentshark.countable"] is False


def test_existing_foreign_langchain_instrumentation_is_rejected(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from openinference.instrumentation.langchain import LangChainInstrumentor

    manager = TelemetryManager(config())
    monkeypatch.setattr(
        LangChainInstrumentor,
        "is_instrumented_by_opentelemetry",
        property(lambda self: True),
    )
    try:
        with pytest.raises(AgentSharkConfigurationError, match="already instrumented"):
            manager.ensure_langchain_instrumented(required=True)
    finally:
        manager.provider.shutdown()
