from __future__ import annotations

import socket
import threading
import time
from collections.abc import Iterator
from contextlib import contextmanager

import pytest
import uvicorn
from agentshark_demo.errors import DemoMCPTimeout
from agentshark_demo.fixtures.app import app as fixture_app
from agentshark_demo.fixtures.llm import DEMO_MODEL, router
from agentshark_demo.fixtures.mcp import asset_lookup, mcp_fixture, threat_intel_lookup
from agentshark_demo.mcp_client import MCPFixtureClient
from agentshark_demo.scenarios import DEMO_ACTION_URL, DEMO_HOST, DEMO_INDICATOR
from agentshark_demo.tools.simulated_action import InvalidDemoTarget, send_http
from fastapi import FastAPI
from fastapi.testclient import TestClient


def _llm_client() -> TestClient:
    app = FastAPI()
    app.include_router(router)
    return TestClient(app)


def test_llm_fixture_is_stable_and_rejects_unknown_requests() -> None:
    request = {
        "model": DEMO_MODEL,
        "messages": [{"role": "user", "content": "[STAGE:demo.plan]"}],
        "stream": False,
        "temperature": 0,
    }
    with _llm_client() as client:
        models = client.get("/v1/models")
        first = client.post("/v1/chat/completions", json=request)
        second = client.post("/v1/chat/completions", json=request)
        unknown = client.post(
            "/v1/chat/completions",
            json={**request, "messages": [{"role": "user", "content": "free prompt"}]},
        )
        streaming = client.post(
            "/v1/chat/completions",
            json={**request, "stream": True},
        )

    assert models.status_code == 200
    assert models.json() == {
        "object": "list",
        "data": [
            {
                "id": DEMO_MODEL,
                "object": "model",
                "created": 1_784_688_000,
                "owned_by": "agentsharkx",
            }
        ],
    }
    assert first.status_code == 200
    assert first.json() == second.json()
    assert first.json()["usage"] == {
        "prompt_tokens": 31,
        "completion_tokens": 13,
        "total_tokens": 44,
    }
    assert unknown.status_code == 400
    assert streaming.status_code == 400


def test_mcp_fixture_has_only_fixed_targets_and_controlled_failure() -> None:
    assert asset_lookup(DEMO_HOST)["simulated"] is True
    assert threat_intel_lookup(DEMO_INDICATOR, "approval")["malicious"] is True
    with pytest.raises(ValueError, match="DEMO_TARGET_REJECTED"):
        asset_lookup("other-host")
    with pytest.raises(ValueError, match="DEMO_TARGET_REJECTED"):
        threat_intel_lookup("198.51.100.8", "happy")
    with pytest.raises(RuntimeError, match="DEMO_MCP_TIMEOUT"):
        threat_intel_lookup(DEMO_INDICATOR, "failure")


def test_mcp_fixture_allows_only_local_and_fixed_compose_hosts() -> None:
    security = mcp_fixture.settings.transport_security

    assert security is not None
    assert security.enable_dns_rebinding_protection is True
    assert security.allowed_hosts == [
        "127.0.0.1:*",
        "localhost:*",
        "[::1]:*",
        "demo-fixtures:39200",
    ]


def test_mcp_streamable_http_transport() -> None:
    with _serve_fixture() as url:
        client = MCPFixtureClient(f"{url}/mcp")

        assert client.asset_lookup(DEMO_HOST)["hostname"] == DEMO_HOST
        assert client.threat_intel_lookup(DEMO_INDICATOR, "approval")["malicious"] is True
        with pytest.raises(DemoMCPTimeout):
            client.threat_intel_lookup(DEMO_INDICATOR, "failure")


def test_simulated_action_never_performs_an_external_request() -> None:
    result = send_http(
        DEMO_ACTION_URL,
        {"host": DEMO_HOST, "action": "quarantine"},
    )

    assert result == {
        "simulated": True,
        "status": "accepted",
        "host": DEMO_HOST,
        "sideEffect": False,
    }
    with pytest.raises(InvalidDemoTarget, match="DEMO_TARGET_REJECTED"):
        send_http(
            "https://quarantine.example.com/hosts/other",
            {"host": "other", "action": "quarantine"},
        )


@contextmanager
def _serve_fixture() -> Iterator[str]:
    listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(("127.0.0.1", 0))
    server = uvicorn.Server(
        uvicorn.Config(fixture_app, log_level="critical", access_log=False)
    )
    thread = threading.Thread(
        target=server.run,
        kwargs={"sockets": [listener]},
        daemon=True,
    )
    thread.start()
    try:
        deadline = time.monotonic() + 2
        while not server.started and time.monotonic() < deadline:
            time.sleep(0.01)
        if not server.started:
            raise RuntimeError("fixture server did not start")
        host, port = listener.getsockname()
        yield f"http://{host}:{port}"
    finally:
        server.should_exit = True
        thread.join(timeout=2)
        listener.close()
