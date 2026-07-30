"""A fixed AgentGuard target that never performs network I/O."""

from __future__ import annotations

from typing import Any

from agentshark_demo.scenarios import DEMO_ACTION_URL, DEMO_HOST


class InvalidDemoTarget(ValueError):
    """A caller attempted to change the immutable simulated action."""


def send_http(url: str, body: dict[str, Any]) -> dict[str, Any]:
    """Return a simulated quarantine receipt without sending HTTP."""

    if url != DEMO_ACTION_URL or body != {"host": DEMO_HOST, "action": "quarantine"}:
        raise InvalidDemoTarget("DEMO_TARGET_REJECTED")
    return {
        "simulated": True,
        "status": "accepted",
        "host": DEMO_HOST,
        "sideEffect": False,
    }
