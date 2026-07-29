from __future__ import annotations

import asyncio
import gc
import threading
import weakref

import pytest
from helpers import FailingExporter, FakeGuard, runtime
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import StatusCode

from agentshark import AgentAlreadyAttachedError, ConcurrentTaskError, RuntimeClosedError
from agentshark.context import current_task
from agentshark.integrations.langchain import attachments


def finished(exporter: object):
    assert isinstance(exporter, InMemorySpanExporter)
    return exporter.get_finished_spans()


def test_task_root_success_and_guard_context() -> None:
    sdk, exporter, guard = runtime()
    with sdk.task(task_id="task-a", goal="private prompt") as task:
        assert task.task_id == "task-a"
        assert current_task() == task
    assert sdk.flush()
    spans = finished(exporter)
    assert len(spans) == 1
    span = spans[0]
    assert span.name == "agentshark.task"
    assert span.status.status_code is StatusCode.OK
    assert span.attributes["agentshark.task.root"] is True
    assert span.attributes["agentshark.task.goal.length"] == len("private prompt")
    assert "private prompt" not in repr(span.attributes)
    assert guard.contexts[0].trace_id == f"{span.context.trace_id:032x}"
    assert guard.restore_count == 1
    sdk.close()


def test_task_error_is_rethrown_without_message_capture() -> None:
    sdk, exporter, _ = runtime()
    with pytest.raises(ValueError, match="private failure value"):
        with sdk.task(task_id="task-error"):
            raise ValueError("private failure value")
    sdk.flush()
    span = finished(exporter)[0]
    assert span.status.status_code is StatusCode.ERROR
    assert span.status.description == "__REDACTED__"
    assert "private failure value" not in repr(span.events)
    sdk.close()


@pytest.mark.parametrize("mode", ["none", "metadata", "full"])
def test_goal_content_policy(mode: str) -> None:
    sdk, exporter, _ = runtime(mode)
    with sdk.task(task_id=f"task-{mode}", goal="goal body"):
        pass
    sdk.flush()
    attributes = finished(exporter)[0].attributes
    if mode == "none":
        assert not any("goal" in key for key in attributes)
        assert attributes["agentshark.content.state"] == "not_collected"
    elif mode == "metadata":
        assert attributes["agentshark.task.goal.length"] == 9
        assert attributes["agentshark.content.state"] == "redacted"
        assert "goal body" not in repr(attributes)
    else:
        assert attributes["agentshark.task.goal"] == "goal body"
        assert attributes["agentshark.content.state"] == "captured"
    sdk.close()


@pytest.mark.parametrize("mode", ["metadata", "full"])
def test_span_without_content_is_not_collected(mode: str) -> None:
    sdk, exporter, _ = runtime(mode)
    with sdk.task(task_id=f"empty-{mode}"):
        pass
    sdk.flush()
    attributes = finished(exporter)[0].attributes
    assert attributes["agentshark.content.state"] == "not_collected"
    sdk.close()


def test_full_mode_truncates_content_and_redacts_credentials() -> None:
    sdk, exporter, _ = runtime("full")
    with sdk.task(task_id="task-full"):
        with sdk._tracer.start_as_current_span(
            "content-span",
            attributes={
                "input.value": '{"authorization":"Bearer private-token"}',
                "output.value": "x" * 2048,
                "http.request.header.x-api-key": "private-token",
            },
        ) as span:
            span.add_event("content", {"tool.arguments": "y" * 2048})
        with sdk._tracer.start_as_current_span(
            "redacted-span",
            attributes={"input.value": '{"password":"private-token"}'},
        ):
            pass
    sdk.flush()
    content_span = next(span for span in finished(exporter) if span.name == "content-span")
    attributes = content_span.attributes
    assert attributes["input.value"] == "__REDACTED__"
    assert attributes["http.request.header.x-api-key"] == "__REDACTED__"
    assert len(attributes["output.value"].encode("utf-8")) <= 1024
    assert attributes["output.value"].endswith("...[truncated]")
    assert content_span.events[0].attributes["tool.arguments"].endswith("...[truncated]")
    assert attributes["agentshark.content.state"] == "truncated"
    assert "private-token" not in repr(attributes)
    redacted_span = next(span for span in finished(exporter) if span.name == "redacted-span")
    assert redacted_span.attributes["input.value"] == "__REDACTED__"
    assert redacted_span.attributes["agentshark.content.state"] == "redacted"
    sdk.close()


def test_attach_is_idempotent_and_cross_runtime_is_rejected() -> None:
    sdk, _, guard = runtime()
    other, _, _ = runtime()
    agent = object()
    assert sdk.attach_langchain(agent) is agent
    assert sdk.attach_langchain(agent) is agent
    assert guard.attach_count == 1
    with pytest.raises(AgentAlreadyAttachedError):
        other.attach_langchain(agent)
    sdk.close()
    other.close()


@pytest.mark.parametrize("fail", [False, True])
def test_concurrent_attach_waits_for_the_single_owner(fail: bool) -> None:
    class Agent:
        pass

    class BlockingGuard(FakeGuard):
        def __init__(self) -> None:
            super().__init__()
            self.started = threading.Event()
            self.release = threading.Event()

        def attach_langchain(self, agent: object) -> object:
            self.attach_count += 1
            self.started.set()
            if not self.release.wait(timeout=2):
                raise TimeoutError("test attach was not released")
            if fail:
                raise PermissionError("attach failed")
            return agent

    guard = BlockingGuard()
    sdk, _, _ = runtime(guard=guard)
    agent = Agent()
    results: list[object] = []
    errors: list[BaseException] = []
    second_entered = threading.Event()
    second_done = threading.Event()

    def run_attach(
        *, entered: threading.Event | None = None, done: threading.Event | None = None
    ) -> None:
        if entered is not None:
            entered.set()
        try:
            results.append(sdk.attach_langchain(agent))
        except BaseException as exc:
            errors.append(exc)
        finally:
            if done is not None:
                done.set()

    first = threading.Thread(target=run_attach)
    first.start()
    assert guard.started.wait(timeout=1)
    second = threading.Thread(
        target=run_attach,
        kwargs={"entered": second_entered, "done": second_done},
    )
    second.start()
    assert second_entered.wait(timeout=1)
    returned_before_owner = second_done.wait(timeout=0.1)
    guard.release.set()
    first.join(timeout=2)
    second.join(timeout=2)

    assert not first.is_alive()
    assert not second.is_alive()
    assert returned_before_owner is False
    assert guard.attach_count == 1
    if fail:
        assert results == []
        assert len(errors) == 2
        assert all(isinstance(error, PermissionError) for error in errors)
        assert id(agent) not in attachments._attachments
    else:
        assert errors == []
        assert results == [agent, agent]
    sdk.close()


def test_weak_reference_attachment_is_removed_after_agent_collection() -> None:
    class Agent:
        pass

    sdk, _, _ = runtime()
    agent = Agent()
    object_id = id(agent)
    reference = weakref.ref(agent)
    assert sdk.attach_langchain(agent) is agent
    assert object_id in attachments._attachments
    del agent
    gc.collect()
    assert reference() is None
    assert object_id not in attachments._attachments
    sdk.close()


def test_guard_deny_is_not_swallowed() -> None:
    sdk, _, _ = runtime(guard=FakeGuard(deny_attach=True))
    with pytest.raises(PermissionError, match="denied"):
        sdk.attach_langchain(object())
    sdk.close()


def test_guard_restore_failure_does_not_replace_business_exception(
    caplog: pytest.LogCaptureFixture,
) -> None:
    guard = FakeGuard(restore_error=RuntimeError("private restore failure"))
    sdk, _, _ = runtime(guard=guard)
    business_error = ValueError("business failure")
    with pytest.raises(ValueError) as caught:
        with sdk.task(task_id="restore-error"):
            raise business_error
    assert caught.value is business_error
    assert current_task() is None
    assert guard.restore_count == 1
    warnings = [
        record.message
        for record in caplog.records
        if "AgentGuard task context restore failed" in record.message
    ]
    assert warnings == [
        "AgentGuard task context restore failed; preserving the agent business exception"
    ]
    assert "private restore failure" not in repr(warnings)
    sdk.close()


def test_same_runtime_rejects_overlapping_tasks() -> None:
    sdk, _, _ = runtime()
    with sdk.task(task_id="first"):
        with pytest.raises(ConcurrentTaskError):
            with sdk.task(task_id="second"):
                pass
    sdk.close()


@pytest.mark.asyncio
async def test_contextvars_isolate_two_async_runtimes() -> None:
    left, _, _ = runtime()
    right, _, _ = runtime()
    observed: dict[str, str] = {}

    async def run_one(name: str, sdk: object) -> None:
        async with sdk.task(task_id=name):  # type: ignore[attr-defined]
            active = current_task()
            assert active is not None
            observed[name] = active.task_id
            await asyncio.sleep(0)
            active = current_task()
            assert active is not None and active.task_id == name

    await asyncio.gather(run_one("left", left), run_one("right", right))
    assert observed == {"left": "left", "right": "right"}
    left.close()
    right.close()


def test_contextvars_isolate_two_threads() -> None:
    left, _, _ = runtime()
    right, _, _ = runtime()
    barrier = threading.Barrier(2)
    observed: dict[str, str] = {}

    def run_one(name: str, sdk: object) -> None:
        with sdk.task(task_id=name):  # type: ignore[attr-defined]
            barrier.wait(timeout=2)
            active = current_task()
            assert active is not None
            observed[name] = active.task_id

    threads = [
        threading.Thread(target=run_one, args=("left", left)),
        threading.Thread(target=run_one, args=("right", right)),
    ]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join(timeout=3)
        assert not thread.is_alive()
    assert observed == {"left": "left", "right": "right"}
    left.close()
    right.close()


def test_exporter_failure_does_not_change_business_result(caplog: pytest.LogCaptureFixture) -> None:
    exporter = FailingExporter()
    sdk, _, _ = runtime(exporter=exporter)
    with sdk.task(task_id="task-a"):
        result = "business-result"
    assert result == "business-result"
    sdk.flush()
    sdk.flush()
    assert exporter.exports >= 1
    warnings = [
        record for record in caplog.records if "trace export is unavailable" in record.message
    ]
    assert len(warnings) == 1
    sdk.close()


def test_close_flushes_and_is_idempotent() -> None:
    sdk, _, guard = runtime()
    assert sdk.close()
    assert sdk.close()
    assert guard.close_count == 1
    with pytest.raises(RuntimeClosedError):
        sdk.task(task_id="late")


def test_close_reports_flush_timeout_and_still_closes_guard(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    sdk, _, guard = runtime()
    monkeypatch.setattr(sdk._manager, "force_flush", lambda timeout_seconds: False)
    assert sdk.close(timeout_seconds=0.01) is False
    assert sdk.close() is False
    assert guard.close_count == 1
