"""Shared fixed scenario and Runner protocol models."""

from __future__ import annotations

from datetime import datetime
from enum import StrEnum
from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field


class Scenario(StrEnum):
    HAPPY = "happy"
    APPROVAL = "approval"
    FAILURE = "failure"


class Outcome(StrEnum):
    NONE = "none"
    NORMAL = "normal"
    APPROVED = "approved"
    DENIED = "denied"
    DEGRADED = "degraded"
    CANCELLED = "cancelled"
    FAILED = "failed"


class RunStatus(StrEnum):
    QUEUED = "queued"
    STARTING = "starting"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"


TERMINAL_STATUSES = frozenset(
    {RunStatus.SUCCEEDED, RunStatus.FAILED, RunStatus.CANCELLED}
)


class ExpectedMetrics(BaseModel):
    model_config = ConfigDict(frozen=True)

    llm_calls: int = Field(alias="llmCalls", ge=0)
    mcp_calls: int = Field(alias="mcpCalls", ge=0)
    local_tool_calls: int = Field(alias="localToolCalls", ge=0)
    a2a_calls: int = Field(alias="a2aCalls", ge=0)
    human_checks: int = Field(alias="humanChecks", ge=0)


class ExecutionMetrics(BaseModel):
    model_config = ConfigDict(frozen=True)

    llm_calls: int = Field(default=0, alias="llmCalls", ge=0)
    mcp_calls: int = Field(default=0, alias="mcpCalls", ge=0)
    local_tool_calls: int = Field(default=0, alias="localToolCalls", ge=0)
    a2a_calls: int = Field(default=0, alias="a2aCalls", ge=0)
    error_count: int = Field(default=0, alias="errorCount", ge=0)


class DemoExecutionResult(BaseModel):
    model_config = ConfigDict(frozen=True)

    scenario: Scenario
    outcome: Outcome
    trace_id: str = Field(alias="traceId", min_length=32, max_length=32)
    root_span_id: str = Field(alias="rootSpanId", min_length=16, max_length=16)
    report: str
    metrics: ExecutionMetrics
    completed_steps: tuple[str, ...] = Field(alias="completedSteps")


class RunnerStartRequest(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True, frozen=True)

    run_id: UUID = Field(alias="runId")
    scenario: Scenario
    delay_ms: int = Field(alias="delayMs", ge=0, le=2_000)
    task_id: str = Field(alias="taskId", min_length=1, max_length=256)
    session_id: str = Field(alias="sessionId", min_length=1, max_length=256)
    request_id: str = Field(alias="requestId", min_length=1, max_length=256)


class RunnerSnapshot(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True, frozen=True)

    run_id: UUID = Field(alias="runId")
    scenario: Scenario
    status: RunStatus
    outcome: Outcome
    delay_ms: int = Field(alias="delayMs")
    task_id: str = Field(alias="taskId")
    session_id: str = Field(alias="sessionId")
    request_id: str = Field(alias="requestId")
    trace_id: str | None = Field(default=None, alias="traceId")
    root_span_id: str | None = Field(default=None, alias="rootSpanId")
    current_step: str | None = Field(default=None, alias="currentStep")
    completed_steps: int = Field(default=0, alias="completedSteps")
    total_steps: int = Field(alias="totalSteps")
    started_at: datetime | None = Field(default=None, alias="startedAt")
    completed_at: datetime | None = Field(default=None, alias="completedAt")
    heartbeat_at: datetime = Field(alias="heartbeatAt")
    cancel_requested: bool = Field(default=False, alias="cancelRequested")
    error_code: str | None = Field(default=None, alias="errorCode")
    error_summary: str | None = Field(default=None, alias="errorSummary")
    metrics: ExecutionMetrics | None = None


def public_json(model: BaseModel) -> dict[str, Any]:
    return model.model_dump(mode="json", by_alias=True)
