"""Validated operator-owned configuration for the demo processes."""

from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass, field
from urllib.parse import urlsplit


class DemoConfigurationError(ValueError):
    """A Demo service setting is missing or unsafe."""


@dataclass(frozen=True, slots=True)
class DemoSettings:
    llm_base_url: str
    llm_model: str
    mcp_url: str
    request_timeout_seconds: float = 10.0
    fixture_version: str = "v1"

    @classmethod
    def from_env(cls, environ: Mapping[str, str] | None = None) -> DemoSettings:
        values = os.environ if environ is None else environ
        return cls(
            llm_base_url=_url(
                values.get(
                    "AGENTSHARK_DEMO_LLM_BASE_URL",
                    "http://agentshark-demo-gateway:39000/v1",
                ),
                "AGENTSHARK_DEMO_LLM_BASE_URL",
            ).rstrip("/"),
            llm_model=_nonempty(
                values.get("AGENTSHARK_DEMO_LLM_MODEL", "agentshark-demo-model-v1"),
                "AGENTSHARK_DEMO_LLM_MODEL",
            ),
            mcp_url=_url(
                values.get("AGENTSHARK_DEMO_MCP_URL", "http://demo-fixtures:39200/mcp"),
                "AGENTSHARK_DEMO_MCP_URL",
            ),
            request_timeout_seconds=_number(
                values.get("AGENTSHARK_DEMO_REQUEST_TIMEOUT_SECONDS", "10"),
                "AGENTSHARK_DEMO_REQUEST_TIMEOUT_SECONDS",
                minimum=0.5,
                maximum=60.0,
            ),
        )


@dataclass(frozen=True, slots=True)
class RunnerSettings:
    token: str = field(repr=False)
    host: str = "0.0.0.0"
    port: int = 39100
    max_concurrency: int = 1
    history_limit: int = 100

    @classmethod
    def from_env(cls, environ: Mapping[str, str] | None = None) -> RunnerSettings:
        values = os.environ if environ is None else environ
        token = _nonempty(
            values.get("AGENTSHARK_DEMO_RUNNER_TOKEN", ""),
            "AGENTSHARK_DEMO_RUNNER_TOKEN",
        )
        if len(token.encode("utf-8")) < 32:
            raise DemoConfigurationError(
                "AGENTSHARK_DEMO_RUNNER_TOKEN must contain at least 32 bytes"
            )
        concurrency = _integer(
            values.get("AGENTSHARK_DEMO_MAX_CONCURRENCY", "1"),
            "AGENTSHARK_DEMO_MAX_CONCURRENCY",
            minimum=1,
            maximum=1,
        )
        return cls(
            token=token,
            host=_nonempty(
                values.get("AGENTSHARK_DEMO_RUNNER_HOST", "0.0.0.0"),
                "AGENTSHARK_DEMO_RUNNER_HOST",
            ),
            port=_integer(
                values.get("AGENTSHARK_DEMO_RUNNER_PORT", "39100"),
                "AGENTSHARK_DEMO_RUNNER_PORT",
                minimum=1,
                maximum=65_535,
            ),
            max_concurrency=concurrency,
            history_limit=_integer(
                values.get("AGENTSHARK_DEMO_RUNNER_HISTORY_LIMIT", "100"),
                "AGENTSHARK_DEMO_RUNNER_HISTORY_LIMIT",
                minimum=1,
                maximum=1_000,
            ),
        )


def _url(value: str, name: str) -> str:
    normalized = _nonempty(value, name)
    parsed = urlsplit(normalized)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise DemoConfigurationError(f"{name} must be an absolute http(s) URL")
    if parsed.username is not None or parsed.password is not None:
        raise DemoConfigurationError(f"{name} must not contain credentials")
    if parsed.query or parsed.fragment:
        raise DemoConfigurationError(f"{name} must not contain a query or fragment")
    return normalized


def _nonempty(value: str, name: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise DemoConfigurationError(f"{name} is required")
    return normalized


def _number(value: str, name: str, *, minimum: float, maximum: float) -> float:
    try:
        result = float(value)
    except ValueError as exc:
        raise DemoConfigurationError(f"{name} must be a number") from exc
    if result < minimum or result > maximum:
        raise DemoConfigurationError(f"{name} must be between {minimum} and {maximum}")
    return result


def _integer(value: str, name: str, *, minimum: int, maximum: int) -> int:
    try:
        result = int(value)
    except ValueError as exc:
        raise DemoConfigurationError(f"{name} must be an integer") from exc
    if result < minimum or result > maximum:
        raise DemoConfigurationError(f"{name} must be between {minimum} and {maximum}")
    return result
