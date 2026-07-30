from __future__ import annotations

from uuid import uuid4

import pytest
from agentshark_demo.config import (
    DemoConfigurationError,
    DemoSettings,
    RunnerSettings,
)
from agentshark_demo.models import RunnerStartRequest
from pydantic import ValidationError


def test_demo_settings_have_fixed_internal_defaults() -> None:
    settings = DemoSettings.from_env({})

    assert settings.llm_base_url == "http://agentshark-demo-gateway:39000/v1"
    assert settings.mcp_url == "http://demo-fixtures:39200/mcp"
    assert settings.llm_model == "agentshark-demo-model-v1"


@pytest.mark.parametrize(
    ("name", "value"),
    [
        ("AGENTSHARK_DEMO_LLM_BASE_URL", "file:///tmp/model"),
        ("AGENTSHARK_DEMO_LLM_BASE_URL", "http://user:password@example.test/v1"),
        ("AGENTSHARK_DEMO_MCP_URL", "http://fixture.test/mcp?target=other"),
    ],
)
def test_demo_settings_reject_unsafe_urls(name: str, value: str) -> None:
    with pytest.raises(DemoConfigurationError):
        DemoSettings.from_env({name: value})


def test_runner_requires_long_token_and_single_concurrency() -> None:
    with pytest.raises(DemoConfigurationError):
        RunnerSettings.from_env({"AGENTSHARK_DEMO_RUNNER_TOKEN": "short"})
    with pytest.raises(DemoConfigurationError):
        RunnerSettings.from_env(
            {
                "AGENTSHARK_DEMO_RUNNER_TOKEN": "x" * 32,
                "AGENTSHARK_DEMO_MAX_CONCURRENCY": "2",
            }
        )


def test_runner_request_rejects_delay_and_arbitrary_target_fields() -> None:
    base = {
        "runId": str(uuid4()),
        "scenario": "happy",
        "delayMs": 0,
        "taskId": "task-a",
        "sessionId": "session-a",
        "requestId": "request-a",
    }
    with pytest.raises(ValidationError):
        RunnerStartRequest.model_validate({**base, "delayMs": 2_001})
    with pytest.raises(ValidationError):
        RunnerStartRequest.model_validate(
            {**base, "url": "https://arbitrary.example.test"}
        )
