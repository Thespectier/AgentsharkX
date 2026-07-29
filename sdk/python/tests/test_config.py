from __future__ import annotations

import pytest
from helpers import environment

from agentshark.config import ContentMode, GuardFailureMode, SDKConfig
from agentshark.errors import AgentSharkConfigurationError


@pytest.mark.parametrize("mode", ["none", "metadata", "full"])
def test_content_modes(mode: str) -> None:
    env = environment(mode)
    assert SDKConfig.from_env(env).content_mode == mode


def test_canonical_trace_variables_take_priority_over_legacy_aliases() -> None:
    env = environment()
    env["AGENTSHARK_OTLP_ENDPOINT"] = "http://legacy.invalid/v1/traces"
    env["AGENTSHARK_INGEST_TOKEN"] = "legacy-token-value"
    config = SDKConfig.from_env(env)
    assert config.trace_endpoint == "http://127.0.0.1:4318/v1/traces"
    assert config.ingest_token == "trace-token-for-tests"


def test_from_env_defaults_to_process_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    for key, value in environment().items():
        monkeypatch.setenv(key, value)
    assert SDKConfig.from_env().content_mode is ContentMode.METADATA


def test_default_guard_policy_is_closed() -> None:
    env = environment(guard_enabled=True)
    assert SDKConfig.from_env(env).guard_failure_mode is GuardFailureMode.CLOSED


def test_config_repr_never_contains_tokens() -> None:
    env = environment(guard_enabled=True)
    rendered = repr(SDKConfig.from_env(env))
    assert env["AGENTSHARK_TRACE_INGEST_TOKEN"] not in rendered
    assert env["AGENTGUARD_API_KEY"] not in rendered
    assert "<redacted>" in rendered


@pytest.mark.parametrize(
    ("key", "value"),
    [
        ("AGENTSHARK_TRACE_INGEST_TOKEN", "short"),
        ("AGENTSHARK_TRACE_ENDPOINT", "http://127.0.0.1:4318/not-traces"),
        ("AGENTSHARK_TRACE_CONTENT_MODE", "everything"),
        ("AGENTSHARK_GUARD_FAILURE_MODE", "sometimes"),
        ("AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES", "100"),
    ],
)
def test_invalid_configuration_is_explicit(key: str, value: str) -> None:
    env = environment()
    env[key] = value
    with pytest.raises(AgentSharkConfigurationError):
        SDKConfig.from_env(env)


def test_closed_guard_requires_remote_url() -> None:
    env = environment(guard_enabled=True)
    env.pop("AGENTGUARD_SERVER_URL")
    with pytest.raises(AgentSharkConfigurationError, match="AGENTGUARD_SERVER_URL"):
        SDKConfig.from_env(env)
