"""Strict Streamable HTTP MCP tools with fixture-owned targets."""

from __future__ import annotations

from typing import Any

from mcp.server.fastmcp import FastMCP
from mcp.server.transport_security import TransportSecuritySettings

from agentshark_demo.models import Scenario
from agentshark_demo.scenarios import DEMO_HOST, DEMO_INDICATOR

mcp_fixture = FastMCP(
    "demo-security",
    instructions="SIMULATED deterministic fixture only",
    streamable_http_path="/mcp",
    json_response=True,
    stateless_http=True,
    transport_security=TransportSecuritySettings(
        enable_dns_rebinding_protection=True,
        allowed_hosts=[
            "127.0.0.1:*",
            "localhost:*",
            "[::1]:*",
            "demo-fixtures:39200",
        ],
        allowed_origins=[
            "http://127.0.0.1:*",
            "http://localhost:*",
            "http://[::1]:*",
        ],
    ),
)


@mcp_fixture.tool(name="asset_lookup")
def asset_lookup(hostname: str) -> dict[str, Any]:
    """Return the one fixed simulated asset."""

    if hostname != DEMO_HOST:
        raise ValueError("DEMO_TARGET_REJECTED")
    return {
        "hostname": DEMO_HOST,
        "owner": "demo-web-team",
        "criticality": "high",
        "environment": "demo",
        "simulated": True,
    }


@mcp_fixture.tool(name="threat_intel_lookup")
def threat_intel_lookup(indicator: str, scenario: str) -> dict[str, Any]:
    """Return deterministic threat intelligence or a controlled failure."""

    if indicator != DEMO_INDICATOR:
        raise ValueError("DEMO_TARGET_REJECTED")
    try:
        selected = Scenario(scenario)
    except ValueError as exc:
        raise ValueError("DEMO_SCENARIO_REJECTED") from exc
    if selected is Scenario.FAILURE:
        raise RuntimeError("DEMO_MCP_TIMEOUT")
    malicious = selected is Scenario.APPROVAL
    return {
        "indicator": DEMO_INDICATOR,
        "malicious": malicious,
        "labels": ["demo-malicious"] if malicious else [],
        "confidence": 0.97 if malicious else 0.08,
        "simulated": True,
    }
