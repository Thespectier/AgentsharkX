"""Adapter for the exact AgentGuard facade verified by AgentsharkX."""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import Any, Protocol

from agentshark.config import GuardFailureMode, SDKConfig
from agentshark.context import RuntimeContext
from agentshark.errors import AgentGuardAttachError, AgentGuardUnavailableError

logger = logging.getLogger("agentshark")

AGENTGUARD_REVISION = "4b755fb4a4a2763b7e817b3d0220fe5c22187b59"


class GuardAdapter(Protocol):
    def attach_langchain(self, agent: Any) -> Any: ...

    def bind_task(self, context: RuntimeContext, goal_metadata: dict[str, Any]) -> Any: ...

    def restore_task(self, state: Any) -> None: ...

    def close(self) -> None: ...


@dataclass(slots=True)
class _TaskState:
    task_id: Any
    metadata: dict[str, Any]


class _AlwaysTrialBreaker:
    @property
    def is_open(self) -> bool:
        return False


class _FailureModeRemote:
    def __init__(self, remote: Any, mode: GuardFailureMode, allow_factory: Any) -> None:
        self._remote = remote
        self._mode = mode
        self._allow_factory = allow_factory
        self.breaker = remote.breaker if mode is GuardFailureMode.OPEN else _AlwaysTrialBreaker()

    @property
    def enabled(self) -> bool:
        return bool(self._remote.enabled)

    def decide(self, *args: Any, **kwargs: Any) -> Any:
        try:
            return self._remote.decide(*args, **kwargs)
        except Exception:
            if self._mode is GuardFailureMode.CLOSED:
                raise
            return self._allow_factory(
                "Remote AgentGuard unavailable under explicit open failure mode.",
                metadata={"route": "agentshark_failure_open"},
            )

    def __getattr__(self, name: str) -> Any:
        return getattr(self._remote, name)


class AgentGuardIntegration:
    """Own Guard/Principal construction and mutable task context updates."""

    def __init__(
        self,
        config: SDKConfig,
        *,
        agent_id: str,
        session_id: str,
        user_id: str | None,
        guard_factory: Any | None = None,
        principal_factory: Any | None = None,
        allow_factory: Any | None = None,
    ) -> None:
        self._config = config
        self._guard: Any | None = None
        self._closed = False
        logger.info("AgentGuard failure mode: %s", config.guard_failure_mode.value)

        if not config.guard_enabled:
            return

        guard: Any | None = None
        try:
            if guard_factory is None or principal_factory is None or allow_factory is None:
                from agentguard import Guard, Principal
                from agentguard.schemas.decisions import GuardDecision

                guard_factory = guard_factory or Guard
                principal_factory = principal_factory or Principal
                allow_factory = allow_factory or GuardDecision.allow

            guard = guard_factory(
                remote_url=config.guard_server_url,
                api_key=config.guard_api_key,
                policy=config.guard_policy,
                environment=config.environment,
                mode="enforce",
                fail_open=config.guard_failure_mode is GuardFailureMode.OPEN,
                remote_timeout_s=config.guard_remote_timeout_seconds,
                remote_retries=config.guard_remote_retries,
            )
            principal = principal_factory(
                agent_id=agent_id,
                session_id=session_id,
                user_id=user_id,
                environment=config.environment,
                metadata={
                    "agentshark_sdk": "0.1.0",
                    "agentshark_agentguard_revision": AGENTGUARD_REVISION,
                },
            )
            guard.start(principal=principal)
            self._install_failure_policy(guard, allow_factory)
            self._probe_remote(guard)
            self._guard = guard
        except Exception as exc:
            if guard is not None:
                try:
                    guard.close()
                except Exception:
                    logger.warning("AgentGuard cleanup failed after an initialization error")
            if config.guard_failure_mode is GuardFailureMode.CLOSED:
                raise AgentGuardUnavailableError(
                    "AgentGuard initialization failed under closed failure mode"
                ) from exc
            logger.warning(
                "AgentGuard is unavailable under explicit open failure mode; execution will "
                "continue unprotected"
            )
            self._guard = None

    @property
    def enabled(self) -> bool:
        return self._guard is not None

    def attach_langchain(self, agent: Any) -> Any:
        if self._guard is None:
            return agent
        try:
            self._guard.attach_langchain(agent)
        except Exception as exc:
            if self._config.guard_failure_mode is GuardFailureMode.CLOSED:
                raise AgentGuardAttachError(
                    "AgentGuard could not attach the LangChain agent"
                ) from exc
            logger.warning(
                "AgentGuard attach failed under explicit open failure mode; execution will "
                "continue unprotected"
            )
        return agent

    def bind_task(
        self, context: RuntimeContext, goal_metadata: dict[str, Any]
    ) -> _TaskState | None:
        if self._guard is None:
            return None
        native_context = self._guard.context
        state = _TaskState(
            task_id=getattr(native_context, "task_id", None),
            metadata=dict(getattr(native_context, "metadata", {}) or {}),
        )
        native_context.task_id = context.task_id
        native_context.metadata.update(
            {
                "trace_id": context.trace_id,
                "agentshark_runtime_id": context.runtime_id,
                **goal_metadata,
            }
        )
        return state

    def restore_task(self, state: _TaskState | None) -> None:
        if self._guard is None or state is None:
            return
        native_context = self._guard.context
        native_context.task_id = state.task_id
        native_context.metadata.clear()
        native_context.metadata.update(state.metadata)

    def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        if self._guard is not None:
            self._guard.close()

    def _install_failure_policy(self, guard: Any, allow_factory: Any) -> None:
        facade_guard: Any = getattr(guard, "_guard", None)
        native_remote = getattr(facade_guard, "_remote", None)
        native_enforcer = getattr(facade_guard, "_enforcer", None)
        if native_remote is None or native_enforcer is None:
            return
        remote = _FailureModeRemote(
            native_remote,
            self._config.guard_failure_mode,
            allow_factory,
        )
        facade_guard._remote = remote
        native_enforcer.remote = remote

    def _probe_remote(self, guard: Any) -> None:
        if self._config.guard_server_url is None:
            if self._config.guard_failure_mode is GuardFailureMode.CLOSED:
                raise AgentGuardUnavailableError("AgentGuard closed mode requires a remote server")
            return
        facade_guard = getattr(guard, "_guard", None)
        remote = getattr(facade_guard, "_remote", None)
        fetch_snapshot = getattr(remote, "fetch_snapshot", None)
        if callable(fetch_snapshot):
            fetch_snapshot()


class NullGuardIntegration:
    def attach_langchain(self, agent: Any) -> Any:
        return agent

    def bind_task(self, context: RuntimeContext, goal_metadata: dict[str, Any]) -> None:
        _ = (context, goal_metadata)
        return None

    def restore_task(self, state: Any) -> None:
        _ = state

    def close(self) -> None:
        return None
