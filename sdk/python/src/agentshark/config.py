"""Validated environment configuration for the local SDK."""

from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass
from enum import Enum
from urllib.parse import urlsplit

from agentshark.errors import ConfigurationError


class ContentMode(str, Enum):
    NONE = "none"
    METADATA = "metadata"
    FULL = "full"


class GuardFailureMode(str, Enum):
    OPEN = "open"
    CLOSED = "closed"


@dataclass(frozen=True, slots=True)
class SDKConfig:
    """Secret-safe runtime settings parsed from environment variables."""

    trace_endpoint: str
    ingest_token: str
    content_mode: ContentMode = ContentMode.METADATA
    export_timeout_seconds: float = 10.0
    flush_timeout_seconds: float = 10.0
    guard_url: str | None = None
    guard_api_key: str | None = None
    guard_failure_mode: GuardFailureMode = GuardFailureMode.CLOSED
    guard_timeout_seconds: float = 5.0
    environment: str = "production"
    user_id: str | None = None
    service_name: str = "agentshark-agent"
    payload_limit_bytes: int = 262_144
    batch_schedule_delay_ms: int = 1_000
    batch_max_queue_size: int = 2_048
    batch_max_export_size: int = 512
    guard_enabled: bool = True
    guard_policy: str | None = None
    guard_remote_retries: int = 0

    def __post_init__(self) -> None:
        content_mode = _enum(ContentMode, self.content_mode, "AGENTSHARK_TRACE_CONTENT_MODE")
        failure_mode = _enum(
            GuardFailureMode,
            self.guard_failure_mode,
            "AGENTSHARK_GUARD_FAILURE_MODE",
        )
        object.__setattr__(self, "content_mode", content_mode)
        object.__setattr__(self, "guard_failure_mode", failure_mode)
        _validate_trace_url(self.trace_endpoint, "AGENTSHARK_TRACE_ENDPOINT")
        if len(self.ingest_token) < 16:
            raise ConfigurationError(
                "AGENTSHARK_TRACE_INGEST_TOKEN must contain at least 16 characters"
            )
        if self.guard_url is not None:
            _validate_url(self.guard_url, "AGENTGUARD_SERVER_URL")
        if (
            self.guard_enabled
            and failure_mode is GuardFailureMode.CLOSED
            and self.guard_url is None
        ):
            raise ConfigurationError(
                "AGENTGUARD_SERVER_URL is required when AgentGuard is enabled in closed mode"
            )
        if self.payload_limit_bytes < 1_024 or self.payload_limit_bytes > 16 * 1024 * 1024:
            raise ConfigurationError(
                "AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES must be between 1024 and 16777216"
            )
        if self.batch_max_export_size > self.batch_max_queue_size:
            raise ConfigurationError(
                "AGENTSHARK_TRACE_MAX_EXPORT_BATCH_SIZE cannot exceed "
                "AGENTSHARK_TRACE_MAX_QUEUE_SIZE"
            )

    @property
    def otlp_endpoint(self) -> str:
        return self.trace_endpoint

    @property
    def payload_max_bytes(self) -> int:
        return self.payload_limit_bytes

    @property
    def guard_server_url(self) -> str | None:
        return self.guard_url

    @property
    def guard_remote_timeout_seconds(self) -> float:
        return self.guard_timeout_seconds

    def __repr__(self) -> str:
        return (
            "SDKConfig("
            f"trace_endpoint={self.trace_endpoint!r}, ingest_token='<redacted>', "
            f"service_name={self.service_name!r}, environment={self.environment!r}, "
            f"content_mode={self.content_mode.value!r}, "
            f"payload_limit_bytes={self.payload_limit_bytes}, "
            f"export_timeout_seconds={self.export_timeout_seconds}, "
            f"guard_enabled={self.guard_enabled}, "
            f"guard_failure_mode={self.guard_failure_mode.value!r}, "
            f"guard_url={self.guard_url!r}, guard_api_key='<redacted>', "
            f"guard_policy={self.guard_policy!r})"
        )

    @classmethod
    def from_env(cls, environ: Mapping[str, str] | None = None) -> SDKConfig:
        environ = os.environ if environ is None else environ
        endpoint = (
            _optional(environ.get("AGENTSHARK_TRACE_ENDPOINT"))
            or _optional(environ.get("AGENTSHARK_OTLP_ENDPOINT"))
            or "http://127.0.0.1:4318/v1/traces"
        )
        _validate_trace_url(endpoint, "AGENTSHARK_TRACE_ENDPOINT")
        token = _optional(environ.get("AGENTSHARK_TRACE_INGEST_TOKEN")) or _optional(
            environ.get("AGENTSHARK_INGEST_TOKEN")
        )
        if token is None:
            raise ConfigurationError("AGENTSHARK_TRACE_INGEST_TOKEN is required")
        if len(token) < 16:
            raise ConfigurationError(
                "AGENTSHARK_TRACE_INGEST_TOKEN must contain at least 16 characters"
            )

        content_mode = _enum(
            ContentMode,
            environ.get("AGENTSHARK_TRACE_CONTENT_MODE", ContentMode.METADATA.value),
            "AGENTSHARK_TRACE_CONTENT_MODE",
        )
        failure_mode = _enum(
            GuardFailureMode,
            environ.get("AGENTSHARK_GUARD_FAILURE_MODE", GuardFailureMode.CLOSED.value),
            "AGENTSHARK_GUARD_FAILURE_MODE",
        )
        guard_enabled = _boolean(
            environ.get("AGENTSHARK_GUARD_ENABLED", "true"), "AGENTSHARK_GUARD_ENABLED"
        )
        guard_url = _optional(environ.get("AGENTGUARD_SERVER_URL"))
        if guard_url is not None:
            _validate_url(guard_url, "AGENTGUARD_SERVER_URL")
        if guard_enabled and failure_mode is GuardFailureMode.CLOSED and guard_url is None:
            raise ConfigurationError(
                "AGENTGUARD_SERVER_URL is required when AgentGuard is enabled in closed mode"
            )

        max_queue = _integer(environ, "AGENTSHARK_TRACE_MAX_QUEUE_SIZE", 2048, minimum=1)
        max_export = _integer(environ, "AGENTSHARK_TRACE_MAX_EXPORT_BATCH_SIZE", 512, minimum=1)
        if max_export > max_queue:
            raise ConfigurationError(
                "AGENTSHARK_TRACE_MAX_EXPORT_BATCH_SIZE cannot exceed "
                "AGENTSHARK_TRACE_MAX_QUEUE_SIZE"
            )

        return cls(
            trace_endpoint=endpoint,
            ingest_token=token,
            service_name=_nonempty(
                environ.get("AGENTSHARK_SERVICE_NAME", "agentshark-agent"),
                "AGENTSHARK_SERVICE_NAME",
            ),
            environment=_nonempty(
                environ.get("AGENTSHARK_ENVIRONMENT", "production"),
                "AGENTSHARK_ENVIRONMENT",
            ),
            content_mode=content_mode,
            payload_limit_bytes=_integer(
                environ,
                "AGENTSHARK_TRACE_PAYLOAD_LIMIT_BYTES",
                262_144,
                minimum=1_024,
                maximum=16 * 1024 * 1024,
            ),
            export_timeout_seconds=_number(
                environ,
                "AGENTSHARK_TRACE_EXPORT_TIMEOUT_SECONDS",
                10.0,
                minimum=0.1,
                maximum=120.0,
            ),
            flush_timeout_seconds=_number(
                environ,
                "AGENTSHARK_TRACE_FLUSH_TIMEOUT_SECONDS",
                10.0,
                minimum=0.1,
                maximum=120.0,
            ),
            batch_schedule_delay_ms=_integer(
                environ,
                "AGENTSHARK_TRACE_BATCH_DELAY_MS",
                1_000,
                minimum=1,
                maximum=60_000,
            ),
            batch_max_queue_size=max_queue,
            batch_max_export_size=max_export,
            guard_enabled=guard_enabled,
            guard_failure_mode=failure_mode,
            guard_url=guard_url,
            guard_api_key=_optional(environ.get("AGENTGUARD_API_KEY")),
            guard_policy=_optional(environ.get("AGENTGUARD_POLICY")),
            guard_timeout_seconds=_number(
                environ,
                "AGENTSHARK_GUARD_TIMEOUT_SECONDS",
                5.0,
                minimum=0.1,
                maximum=120.0,
            ),
            guard_remote_retries=_integer(
                environ,
                "AGENTSHARK_GUARD_REMOTE_RETRIES",
                0,
                minimum=0,
                maximum=5,
            ),
            user_id=_optional(environ.get("AGENTSHARK_USER_ID")),
        )


def validate_identity(value: str | None, field: str) -> str:
    if value is None or not value.strip():
        raise ConfigurationError(f"{field} must not be empty")
    normalized = value.strip()
    if len(normalized) > 256:
        raise ConfigurationError(f"{field} must not exceed 256 characters")
    return normalized


def _required(environ: Mapping[str, str], name: str) -> str:
    value = _optional(environ.get(name))
    if value is None:
        raise ConfigurationError(f"{name} is required")
    return value


def _optional(value: str | None) -> str | None:
    if value is None:
        return None
    stripped = value.strip()
    return stripped or None


def _nonempty(value: str, name: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise ConfigurationError(f"{name} must not be empty")
    return normalized


def _validate_url(value: str, name: str) -> None:
    parsed = urlsplit(value)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise ConfigurationError(f"{name} must be an absolute http(s) URL")
    if parsed.username is not None or parsed.password is not None:
        raise ConfigurationError(f"{name} must not contain credentials")
    if parsed.fragment:
        raise ConfigurationError(f"{name} must not contain a URL fragment")


def _validate_trace_url(value: str, name: str) -> None:
    _validate_url(value, name)
    if urlsplit(value).path.rstrip("/") != "/v1/traces":
        raise ConfigurationError(f"{name} must end with /v1/traces")


def _enum(  # type: ignore[no-untyped-def]
    enum_type: type[ContentMode] | type[GuardFailureMode],
    value: ContentMode | GuardFailureMode | str,
    name: str,
):
    if isinstance(value, enum_type):
        return value
    try:
        return enum_type(str(value).strip().lower())
    except ValueError as exc:
        choices = "|".join(item.value for item in enum_type)
        raise ConfigurationError(f"{name} must be one of {choices}") from exc


def _boolean(value: str, name: str) -> bool:
    normalized = value.strip().lower()
    if normalized in {"1", "true", "yes", "on"}:
        return True
    if normalized in {"0", "false", "no", "off"}:
        return False
    raise ConfigurationError(f"{name} must be true or false")


def _integer(
    environ: Mapping[str, str],
    name: str,
    default: int,
    *,
    minimum: int,
    maximum: int | None = None,
) -> int:
    raw = environ.get(name)
    try:
        value = default if raw is None else int(raw)
    except ValueError as exc:
        raise ConfigurationError(f"{name} must be an integer") from exc
    if value < minimum or (maximum is not None and value > maximum):
        suffix = f" and at most {maximum}" if maximum is not None else ""
        raise ConfigurationError(f"{name} must be at least {minimum}{suffix}")
    return value


def _number(
    environ: Mapping[str, str],
    name: str,
    default: float,
    *,
    minimum: float,
    maximum: float,
) -> float:
    raw = environ.get(name)
    try:
        value = default if raw is None else float(raw)
    except ValueError as exc:
        raise ConfigurationError(f"{name} must be a number") from exc
    if value < minimum or value > maximum:
        raise ConfigurationError(f"{name} must be between {minimum} and {maximum}")
    return value
