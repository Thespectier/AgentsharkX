"""Emit one real AgentGuard tool event for the AgentsharkX quickstart."""

from __future__ import annotations

import os

from agentguard import AgentGuard


def inventory_status(sku: str) -> str:
    """Return a harmless fixture value without external side effects."""
    return f"{sku}: available"


def required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SystemExit(f"{name} is required")
    return value


def main() -> None:
    guard = AgentGuard(
        session_id="agentshark-quickstart-session",
        agent_id="agentshark-quickstart-agent",
        user_id="quickstart-operator",
        policy="enterprise_default",
        server_url=required("AGENTGUARD_SERVER_URL"),
        api_key=required("AGENTGUARD_API_KEY"),
    )
    guarded_tool = guard.wrap_tool(inventory_status, capabilities=["read_inventory"])
    try:
        print(guarded_tool("demo-sku"))
    finally:
        guard.close()


if __name__ == "__main__":
    main()
