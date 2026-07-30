from __future__ import annotations

import threading

from agentguard.sandbox import SandboxExecutor
from agentshark_demo.execution import DEMO_PLUGIN_CONFIG, _demo_sandbox_profile
from agentshark_demo.scenarios import DEMO_ACTION_URL, DEMO_HOST
from agentshark_demo.tools.simulated_action import send_http
from backend.runtime.plugins.tool_before.demo_tripwire import DemoTripwirePlugin
from backend.runtime.review import ReviewQueue
from shared.schemas.context import RuntimeContext
from shared.schemas.decisions import DecisionType
from shared.schemas.events import tool_invoke


def test_demo_plugin_config_runs_tripwire_only_on_server() -> None:
    phase = DEMO_PLUGIN_CONFIG["phases"]["tool_before"]

    assert phase == {
        "client": [],
        "server": [{"name": "demo_tripwire", "env": {}}],
    }


def test_pinned_server_tripwire_requires_one_human_check() -> None:
    context = RuntimeContext(session_id="demo-session", agent_id="demo-agent")
    event = tool_invoke(
        context,
        "send_http",
        {"url": DEMO_ACTION_URL},
        capabilities=["network"],
    )

    result = DemoTripwirePlugin(env={}).check(event, context)

    assert result.is_final is True
    assert result.decision_candidate is not None
    assert result.decision_candidate.decision_type is DecisionType.HUMAN_CHECK
    assert result.risk_signals.count("demo_external_send") == 1


def test_demo_sandbox_profile_allows_only_the_fixed_simulated_network_action() -> None:
    profile = _demo_sandbox_profile()
    assert profile is not _demo_sandbox_profile()
    assert profile.allowed_domains == ["quarantine.example.com"]
    assert profile.allow_network is True
    assert profile.allow_subprocess is False
    assert profile.allow_write is False

    sandbox = SandboxExecutor("local", profile)
    allowed = sandbox.run(
        send_http,
        {
            "url": DEMO_ACTION_URL,
            "body": {"host": DEMO_HOST, "action": "quarantine"},
        },
        capabilities=["network"],
        tool_name="send_http",
    )
    other_domain = sandbox.run(
        lambda url: url,
        {"url": "https://example.net/action"},
        capabilities=["network"],
        tool_name="other_network_action",
    )
    lookalike_domain = sandbox.run(
        send_http,
        {
            "url": "https://not-quarantine.example.com/hosts/web-01",
            "body": {"host": DEMO_HOST, "action": "quarantine"},
        },
        capabilities=["network"],
        tool_name="send_http",
    )
    shell = sandbox.run(
        lambda: "ran",
        {},
        capabilities=["shell"],
        tool_name="shell_action",
    )
    write = sandbox.run(
        lambda path: path,
        {"path": "/tmp/demo"},
        capabilities=["write_file"],
        tool_name="write_action",
    )

    assert allowed.success is True
    assert allowed.value["sideEffect"] is False
    assert other_domain.success is False
    assert other_domain.error == "permission denied: domain not in allowlist: example.net"
    assert lookalike_domain.success is False
    assert lookalike_domain.error == "DEMO_TARGET_REJECTED"
    assert shell.success is False
    assert shell.error == "permission denied: subprocess/shell not permitted"
    assert write.success is False
    assert write.error == "permission denied: file write not permitted"


def test_review_queue_resolves_approve_and_deny_waiters() -> None:
    queue = ReviewQueue()
    approved = queue.enqueue(
        event={"event_id": "approve"},
        decision={"decision_type": "human_check"},
    )
    denied = queue.enqueue(
        event={"event_id": "deny"},
        decision={"decision_type": "human_check"},
    )
    observed: list[str] = []

    def wait_for_approval() -> None:
        resolved = queue.wait(approved["ticket_id"], timeout_s=1)
        assert resolved is not None
        observed.append(resolved["resolved_decision"]["decision_type"])

    waiter = threading.Thread(target=wait_for_approval)
    waiter.start()
    queue.resolve(approved["ticket_id"], approved=True, note="demo approve")
    queue.resolve(denied["ticket_id"], approved=False, note="demo deny")
    waiter.join(timeout=1)

    assert observed == ["allow"]
    assert queue.get(denied["ticket_id"])["resolved_decision"]["decision_type"] == "deny"
