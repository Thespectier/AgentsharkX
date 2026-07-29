"""Emit one local LangChain Task Trace through the Agentshark SDK."""

from __future__ import annotations

import os

from langchain_core.runnables import RunnableLambda

from agentshark import AgentShark


def main() -> None:
    runtime = AgentShark.from_env(
        agent_id=os.getenv("AGENTSHARK_EXAMPLE_AGENT_ID", "trace-quickstart-agent"),
        session_id=os.getenv("AGENTSHARK_EXAMPLE_SESSION_ID", "trace-quickstart-session"),
        user_id=os.getenv("AGENTSHARK_EXAMPLE_USER_ID", "trace-quickstart-user"),
    )
    agent = runtime.attach_langchain(
        RunnableLambda(lambda request: {"status": "processed", "type": type(request).__name__})
    )
    try:
        with runtime.task(task_id="trace-quickstart-task", goal="local quickstart request"):
            result = agent.invoke({"request": "local quickstart"})
        if not runtime.flush():
            raise RuntimeError("Trace flush did not complete before its configured timeout")
    finally:
        runtime.close()
    print(f"Task completed: {result['status']}")


if __name__ == "__main__":
    main()
