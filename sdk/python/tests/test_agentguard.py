from __future__ import annotations

from dataclasses import dataclass, field
from types import SimpleNamespace
from typing import Any

import pytest
from helpers import environment

from agentshark.config import SDKConfig
from agentshark.context import RuntimeContext
from agentshark.errors import AgentGuardUnavailableError
from agentshark.integrations.agentguard import AgentGuardIntegration


class RemoteFailure(RuntimeError):
    pass


@dataclass
class FakeRemote:
    probe_error: Exception | None = None
    decide_error: Exception | None = None
    enabled: bool = True
    breaker: Any = field(default_factory=lambda: SimpleNamespace(is_open=False))

    def fetch_snapshot(self) -> dict[str, Any]:
        if self.probe_error:
            raise self.probe_error
        return {"version": "test"}

    def decide(self, *args: Any, **kwargs: Any) -> str:
        _ = (args, kwargs)
        if self.decide_error:
            raise self.decide_error
        return "allow"


class FakeFacade:
    def __init__(self, remote: FakeRemote) -> None:
        self._guard = SimpleNamespace(
            _remote=remote,
            _enforcer=SimpleNamespace(remote=remote),
        )
        self.context = SimpleNamespace(task_id=None, metadata={"existing": "value"})
        self.attached: list[Any] = []
        self.close_count = 0

    def start(self, *, principal: Any) -> FakeFacade:
        self.principal = principal
        return self

    def attach_langchain(self, agent: Any) -> None:
        self.attached.append(agent)

    def close(self) -> None:
        self.close_count += 1


def sdk_config(mode: str) -> SDKConfig:
    return SDKConfig.from_env(environment(guard_enabled=True, failure_mode=mode))


def factories(facade: FakeFacade):
    def guard_factory(**kwargs: Any) -> FakeFacade:
        facade.guard_kwargs = kwargs
        return facade

    def principal_factory(**kwargs: Any) -> Any:
        return SimpleNamespace(**kwargs)

    def allow_factory(reason: str, **kwargs: Any) -> tuple[str, str, dict[str, Any]]:
        return "allow", reason, kwargs

    return guard_factory, principal_factory, allow_factory


def integration(mode: str, remote: FakeRemote) -> tuple[AgentGuardIntegration, FakeFacade]:
    facade = FakeFacade(remote)
    guard_factory, principal_factory, allow_factory = factories(facade)
    return (
        AgentGuardIntegration(
            sdk_config(mode),
            agent_id="agent-a",
            session_id="session-a",
            user_id="user-a",
            guard_factory=guard_factory,
            principal_factory=principal_factory,
            allow_factory=allow_factory,
        ),
        facade,
    )


def test_closed_mode_rejects_unavailable_guard_at_start() -> None:
    facade = FakeFacade(FakeRemote(probe_error=RemoteFailure("offline")))
    guard_factory, principal_factory, allow_factory = factories(facade)
    with pytest.raises(AgentGuardUnavailableError):
        AgentGuardIntegration(
            sdk_config("closed"),
            agent_id="agent-a",
            session_id="session-a",
            user_id="user-a",
            guard_factory=guard_factory,
            principal_factory=principal_factory,
            allow_factory=allow_factory,
        )
    assert facade.close_count == 1


def test_open_mode_allows_unavailable_guard_at_start() -> None:
    adapter, facade = integration("open", FakeRemote(probe_error=RemoteFailure("offline")))
    agent = object()
    assert adapter.enabled is False
    assert adapter.attach_langchain(agent) is agent
    assert facade.close_count == 1


def test_open_and_closed_remote_failure_policies_are_deterministic() -> None:
    open_adapter, open_facade = integration(
        "open", FakeRemote(decide_error=RemoteFailure("offline"))
    )
    assert open_facade._guard._remote.decide()[0] == "allow"
    open_adapter.close()

    closed_adapter, closed_facade = integration(
        "closed", FakeRemote(decide_error=RemoteFailure("offline"))
    )
    with pytest.raises(RemoteFailure):
        closed_facade._guard._remote.decide()
    closed_adapter.close()


def test_task_context_is_applied_and_restored() -> None:
    adapter, facade = integration("closed", FakeRemote())
    context = RuntimeContext(
        runtime_id="runtime-a",
        agent_id="agent-a",
        session_id="session-a",
        task_id="task-a",
        trace_id="1" * 32,
        user_id="user-a",
        environment="test",
    )
    state = adapter.bind_task(context, {"goal_length": 9})
    assert facade.context.task_id == "task-a"
    assert facade.context.metadata["trace_id"] == "1" * 32
    assert facade.context.metadata["goal_length"] == 9
    adapter.restore_task(state)
    assert facade.context.task_id is None
    assert facade.context.metadata == {"existing": "value"}
    adapter.close()
    adapter.close()
    assert facade.close_count == 1
