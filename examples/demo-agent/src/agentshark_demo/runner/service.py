"""Process entry point for the private Demo Runner."""

from __future__ import annotations

import uvicorn

from agentshark_demo.config import DemoSettings, RunnerSettings
from agentshark_demo.execution import execute_demo
from agentshark_demo.runner.api import create_app
from agentshark_demo.runner.registry import RunRegistry


def main() -> None:
    runner_settings = RunnerSettings.from_env()
    demo_settings = DemoSettings.from_env()
    registry = RunRegistry(
        lambda request, hooks: execute_demo(
            request,
            settings=demo_settings,
            hooks=hooks,
        ),
        history_limit=runner_settings.history_limit,
    )
    app = create_app(runner_settings, registry)
    uvicorn.run(
        app,
        host=runner_settings.host,
        port=runner_settings.port,
        log_level="info",
        access_log=False,
    )
