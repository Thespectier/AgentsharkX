"""Bounded, single-concurrency in-memory state for the internal Runner."""

from __future__ import annotations

import threading
from collections import OrderedDict
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from uuid import UUID

from agentshark_demo.errors import DemoCancelled, DemoError
from agentshark_demo.models import (
    TERMINAL_STATUSES,
    DemoExecutionResult,
    ExecutionMetrics,
    Outcome,
    RunnerSnapshot,
    RunnerStartRequest,
    RunStatus,
)
from agentshark_demo.scenarios import scenario_spec
from agentshark_demo.workflow import ExecutionHooks

Executor = Callable[[RunnerStartRequest, ExecutionHooks], DemoExecutionResult]


class RegistryError(RuntimeError):
    """Finite internal protocol error."""

    code = "demo_runner_failed"
    status_code = 500


class RunBusy(RegistryError):
    code = "demo_run_busy"
    status_code = 409


class RunIDConflict(RegistryError):
    code = "demo_run_id_conflict"
    status_code = 409


class RunNotFound(RegistryError):
    code = "demo_run_not_found"
    status_code = 404


@dataclass(slots=True)
class _RunRecord:
    request: RunnerStartRequest
    status: RunStatus
    outcome: Outcome
    heartbeat_at: datetime
    total_steps: int
    cancellation: threading.Event = field(default_factory=threading.Event)
    trace_id: str | None = None
    root_span_id: str | None = None
    current_step: str | None = None
    completed_steps: int = 0
    started_at: datetime | None = None
    completed_at: datetime | None = None
    cancel_requested: bool = False
    error_code: str | None = None
    error_summary: str | None = None
    metrics: ExecutionMetrics | None = None


class RunRegistry:
    """Runs one workflow at a time and retains a bounded terminal history."""

    def __init__(self, executor: Executor, *, history_limit: int = 100) -> None:
        if history_limit < 1:
            raise ValueError("history_limit must be positive")
        self._executor = executor
        self._history_limit = history_limit
        self._records: OrderedDict[UUID, _RunRecord] = OrderedDict()
        self._active_run_id: UUID | None = None
        self._lock = threading.RLock()

    def start(self, request: RunnerStartRequest) -> tuple[RunnerSnapshot, bool]:
        """Start a Run, or return the existing Run for an exact idempotent retry."""

        with self._lock:
            existing = self._records.get(request.run_id)
            if existing is not None:
                if existing.request != request:
                    raise RunIDConflict(RunIDConflict.code)
                return self._snapshot(existing), False
            if self._active_run_id is not None:
                raise RunBusy(RunBusy.code)

            self._prune_for_insert()
            now = _now()
            record = _RunRecord(
                request=request,
                status=RunStatus.QUEUED,
                outcome=Outcome.NONE,
                heartbeat_at=now,
                total_steps=len(scenario_spec(request.scenario).steps),
            )
            self._records[request.run_id] = record
            self._active_run_id = request.run_id
            worker = threading.Thread(
                target=self._execute,
                args=(request.run_id,),
                name="agentshark-demo-runner",
                daemon=True,
            )
            worker.start()
            return self._snapshot(record), True

    def get(self, run_id: UUID) -> RunnerSnapshot:
        with self._lock:
            record = self._records.get(run_id)
            if record is None:
                raise RunNotFound(RunNotFound.code)
            if record.status not in TERMINAL_STATUSES:
                record.heartbeat_at = _now()
            return self._snapshot(record)

    def cancel(self, run_id: UUID) -> RunnerSnapshot:
        with self._lock:
            record = self._records.get(run_id)
            if record is None:
                raise RunNotFound(RunNotFound.code)
            if record.status not in TERMINAL_STATUSES:
                record.cancel_requested = True
                record.cancellation.set()
                record.heartbeat_at = _now()
            return self._snapshot(record)

    def active_run_id(self) -> UUID | None:
        with self._lock:
            return self._active_run_id

    def _execute(self, run_id: UUID) -> None:
        try:
            request, hooks = self._prepare_execution(run_id)
            result = self._executor(request, hooks)
        except DemoCancelled:
            self._finish(
                run_id,
                status=RunStatus.CANCELLED,
                outcome=Outcome.CANCELLED,
                error_code=DemoCancelled.code.lower(),
                error_summary="Demo run cancelled.",
            )
        except DemoError as exc:
            self._finish(
                run_id,
                status=RunStatus.FAILED,
                outcome=Outcome.FAILED,
                error_code=exc.code.lower(),
                error_summary="Demo dependency failed.",
            )
        except Exception:
            self._finish(
                run_id,
                status=RunStatus.FAILED,
                outcome=Outcome.FAILED,
                error_code="demo_failed",
                error_summary="Demo run failed.",
            )
        else:
            self._finish(
                run_id,
                status=RunStatus.SUCCEEDED,
                outcome=result.outcome,
                trace_id=result.trace_id,
                root_span_id=result.root_span_id,
                completed_steps=len(result.completed_steps),
                metrics=result.metrics,
            )

    def _prepare_execution(
        self, run_id: UUID
    ) -> tuple[RunnerStartRequest, ExecutionHooks]:
        with self._lock:
            record = self._records[run_id]
            record.status = RunStatus.STARTING
            record.started_at = _now()
            record.heartbeat_at = record.started_at
            if record.cancellation.is_set():
                raise DemoCancelled(DemoCancelled.code)
            record.status = RunStatus.RUNNING
            record.heartbeat_at = _now()
            cancellation = record.cancellation

        return record.request, ExecutionHooks(
            on_identity=lambda trace_id, span_id: self._set_identity(
                run_id, trace_id, span_id
            ),
            on_step_started=lambda step: self._step_started(run_id, step),
            on_step_completed=lambda step: self._step_completed(run_id, step),
            is_cancelled=cancellation.is_set,
            wait=cancellation.wait,
        )

    def _set_identity(self, run_id: UUID, trace_id: str, root_span_id: str) -> None:
        with self._lock:
            record = self._records[run_id]
            record.trace_id = trace_id
            record.root_span_id = root_span_id
            record.heartbeat_at = _now()

    def _step_started(self, run_id: UUID, step: str) -> None:
        with self._lock:
            record = self._records[run_id]
            record.current_step = step
            record.heartbeat_at = _now()

    def _step_completed(self, run_id: UUID, step: str) -> None:
        with self._lock:
            record = self._records[run_id]
            record.current_step = step
            record.completed_steps = min(
                record.completed_steps + 1,
                record.total_steps,
            )
            record.heartbeat_at = _now()

    def _finish(
        self,
        run_id: UUID,
        *,
        status: RunStatus,
        outcome: Outcome,
        trace_id: str | None = None,
        root_span_id: str | None = None,
        completed_steps: int | None = None,
        metrics: ExecutionMetrics | None = None,
        error_code: str | None = None,
        error_summary: str | None = None,
    ) -> None:
        with self._lock:
            record = self._records[run_id]
            if record.status in TERMINAL_STATUSES:
                return
            now = _now()
            record.status = status
            record.outcome = outcome
            record.trace_id = trace_id or record.trace_id
            record.root_span_id = root_span_id or record.root_span_id
            if completed_steps is not None:
                record.completed_steps = min(completed_steps, record.total_steps)
            record.metrics = metrics
            record.error_code = error_code
            record.error_summary = error_summary
            record.completed_at = now
            record.heartbeat_at = now
            if self._active_run_id == run_id:
                self._active_run_id = None

    def _prune_for_insert(self) -> None:
        while len(self._records) >= self._history_limit:
            oldest_id, oldest = next(iter(self._records.items()))
            if oldest.status not in TERMINAL_STATUSES:
                raise RunBusy(RunBusy.code)
            del self._records[oldest_id]

    @staticmethod
    def _snapshot(record: _RunRecord) -> RunnerSnapshot:
        request = record.request
        return RunnerSnapshot(
            runId=request.run_id,
            scenario=request.scenario,
            status=record.status,
            outcome=record.outcome,
            delayMs=request.delay_ms,
            taskId=request.task_id,
            sessionId=request.session_id,
            requestId=request.request_id,
            traceId=record.trace_id,
            rootSpanId=record.root_span_id,
            currentStep=record.current_step,
            completedSteps=record.completed_steps,
            totalSteps=record.total_steps,
            startedAt=record.started_at,
            completedAt=record.completed_at,
            heartbeatAt=record.heartbeat_at,
            cancelRequested=record.cancel_requested,
            errorCode=record.error_code,
            errorSummary=record.error_summary,
            metrics=record.metrics,
        )


def _now() -> datetime:
    return datetime.now(UTC)
