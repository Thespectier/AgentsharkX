from __future__ import annotations

import threading
import time
from collections.abc import Callable
from uuid import UUID, uuid4

import pytest
from agentshark_demo.config import RunnerSettings
from agentshark_demo.errors import DemoCancelled
from agentshark_demo.models import (
    DemoExecutionResult,
    ExecutionMetrics,
    Outcome,
    RunnerStartRequest,
    RunStatus,
)
from agentshark_demo.runner.api import create_app
from agentshark_demo.runner.registry import RunBusy, RunIDConflict, RunRegistry
from agentshark_demo.workflow import ExecutionHooks
from fastapi.testclient import TestClient


def _request(*, run_id: UUID | None = None, scenario: str = "happy") -> RunnerStartRequest:
    selected_id = run_id or uuid4()
    return RunnerStartRequest(
        runId=selected_id,
        scenario=scenario,
        delayMs=0,
        taskId=f"task-{selected_id}",
        sessionId=f"session-{selected_id}",
        requestId=f"request-{selected_id}",
    )


def _result(request: RunnerStartRequest) -> DemoExecutionResult:
    return DemoExecutionResult(
        scenario=request.scenario,
        outcome=Outcome.NORMAL,
        traceId="1" * 32,
        rootSpanId="2" * 16,
        report="private report",
        metrics=ExecutionMetrics(
            llmCalls=3,
            mcpCalls=2,
            localToolCalls=1,
            a2aCalls=1,
            errorCount=0,
        ),
        completedSteps=("bootstrap", "finish"),
    )


def _wait_for(
    registry: RunRegistry,
    run_id: UUID,
    predicate: Callable[[RunStatus], bool],
) -> None:
    deadline = time.monotonic() + 2
    while time.monotonic() < deadline:
        if predicate(registry.get(run_id).status):
            return
        time.sleep(0.005)
    raise AssertionError("Runner did not reach the expected state")


def test_registry_is_idempotent_and_rejects_busy_or_changed_run() -> None:
    release = threading.Event()
    entered = threading.Event()

    def blocked(request: RunnerStartRequest, hooks: ExecutionHooks) -> DemoExecutionResult:
        _ = hooks
        entered.set()
        release.wait(timeout=2)
        return _result(request)

    registry = RunRegistry(blocked)
    first_request = _request()
    first, created = registry.start(first_request)
    assert created is True
    assert entered.wait(timeout=1)

    retry, created = registry.start(first_request)
    assert created is False
    assert retry.run_id == first.run_id
    with pytest.raises(RunBusy):
        registry.start(_request())
    changed = first_request.model_copy(update={"delay_ms": 1})
    with pytest.raises(RunIDConflict):
        registry.start(changed)

    release.set()
    _wait_for(registry, first_request.run_id, lambda status: status is RunStatus.SUCCEEDED)


def test_registry_cooperatively_cancels_at_wait_boundary() -> None:
    entered = threading.Event()

    def cancellable(
        request: RunnerStartRequest, hooks: ExecutionHooks
    ) -> DemoExecutionResult:
        _ = request
        entered.set()
        if hooks.wait(2):
            raise DemoCancelled(DemoCancelled.code)
        raise AssertionError("expected cancellation")

    registry = RunRegistry(cancellable)
    request = _request()
    registry.start(request)
    assert entered.wait(timeout=1)

    requested = registry.cancel(request.run_id)
    assert requested.cancel_requested is True
    _wait_for(registry, request.run_id, lambda status: status is RunStatus.CANCELLED)
    final = registry.get(request.run_id)
    assert final.outcome is Outcome.CANCELLED
    assert final.error_code == "demo_cancelled"


def test_runner_http_protocol_auth_validation_and_payload_boundary() -> None:
    token = "runner-test-token-that-is-at-least-32-bytes"
    registry = RunRegistry(lambda request, hooks: _result(request))
    app = create_app(RunnerSettings(token=token), registry)
    request = _request()
    body = request.model_dump(mode="json", by_alias=True)

    with TestClient(app) as client:
        health = client.get("/healthz")
        unauthorized = client.post("/internal/v1/runs", json=body)
        started = client.post(
            "/internal/v1/runs",
            json=body,
            headers={"Authorization": f"Bearer {token}"},
        )
        invalid = client.post(
            "/internal/v1/runs",
            json={**body, "url": "https://arbitrary.example.test"},
            headers={"Authorization": f"Bearer {token}"},
        )

    assert health.status_code == 200
    assert health.json()["maxConcurrency"] == 1
    assert unauthorized.status_code == 401
    assert unauthorized.json() == {"code": "demo_runner_unauthorized"}
    assert started.status_code == 202
    assert "report" not in started.json()
    assert token not in started.text
    assert invalid.status_code == 422
    assert invalid.json() == {"code": "demo_run_invalid"}
    assert "arbitrary.example.test" not in invalid.text
