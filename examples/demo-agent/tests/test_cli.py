from __future__ import annotations

import json

import pytest
from agentshark_demo import cli
from agentshark_demo.models import (
    DemoExecutionResult,
    ExecutionMetrics,
    Outcome,
    RunnerStartRequest,
    Scenario,
)


def test_cli_runs_only_a_fixed_scenario(
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    captured: list[RunnerStartRequest] = []

    def execute(request: RunnerStartRequest) -> DemoExecutionResult:
        captured.append(request)
        return DemoExecutionResult(
            scenario=request.scenario,
            outcome=Outcome.NORMAL,
            traceId="1" * 32,
            rootSpanId="2" * 16,
            report="SIMULATED",
            metrics=ExecutionMetrics(
                llmCalls=3,
                mcpCalls=2,
                localToolCalls=1,
                a2aCalls=1,
                errorCount=0,
            ),
            completedSteps=("bootstrap", "finish"),
        )

    monkeypatch.setattr(cli, "execute_demo", execute)

    assert cli.run(["--scenario", "happy", "--delay-ms", "0"]) == 0

    output = json.loads(capsys.readouterr().out)
    assert output["outcome"] == "normal"
    assert captured[0].scenario is Scenario.HAPPY
    assert captured[0].delay_ms == 0
    assert captured[0].task_id.startswith("demo-task-")
    assert captured[0].session_id.startswith("demo-session-")


def test_cli_rejects_out_of_range_delay() -> None:
    with pytest.raises(SystemExit) as raised:
        cli.run(["--scenario", "happy", "--delay-ms", "2001"])

    assert raised.value.code == 2
