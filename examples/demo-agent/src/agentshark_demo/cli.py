"""Strict local smoke command for one fixed Demo scenario."""

from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Sequence
from uuid import uuid4

from agentshark_demo.config import DemoConfigurationError
from agentshark_demo.errors import DemoError
from agentshark_demo.execution import execute_demo
from agentshark_demo.models import RunnerStartRequest, Scenario, public_json


def run(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="agentshark-demo-run")
    parser.add_argument("--scenario", choices=[item.value for item in Scenario], required=True)
    parser.add_argument("--delay-ms", type=_delay, default=0)
    args = parser.parse_args(argv)

    run_id = uuid4()
    request = RunnerStartRequest(
        runId=run_id,
        scenario=Scenario(args.scenario),
        delayMs=args.delay_ms,
        taskId=f"demo-task-{run_id}",
        sessionId=f"demo-session-{run_id}",
        requestId=f"demo-cli-{run_id}",
    )
    try:
        result = execute_demo(request)
    except DemoConfigurationError:
        _write_error("DEMO_CONFIGURATION_INVALID")
        return 2
    except DemoError as exc:
        _write_error(exc.code)
        return 1
    except Exception:
        _write_error("DEMO_FAILED")
        return 1

    print(json.dumps(public_json(result), sort_keys=True, separators=(",", ":")))
    return 0


def main() -> None:
    raise SystemExit(run())


def _delay(value: str) -> int:
    try:
        delay = int(value)
    except ValueError as exc:
        raise argparse.ArgumentTypeError("delay must be an integer") from exc
    if delay < 0 or delay > 2_000:
        raise argparse.ArgumentTypeError("delay must be between 0 and 2000")
    return delay


def _write_error(code: str) -> None:
    print(json.dumps({"code": code}, separators=(",", ":")), file=sys.stderr)
