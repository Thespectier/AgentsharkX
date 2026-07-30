"""Deterministic local risk scoring."""

from __future__ import annotations

from typing import Any


def calculate_risk_score(evidence: dict[str, Any]) -> dict[str, Any]:
    """Calculate one deterministic score from fixture-owned evidence."""

    threat = evidence.get("threat")
    if isinstance(threat, dict) and threat.get("malicious") is True:
        score = 92
        severity = "critical"
    elif evidence.get("threat_error") == "DEMO_MCP_TIMEOUT":
        score = 61
        severity = "medium"
    else:
        score = 18
        severity = "low"
    return {"score": score, "severity": severity, "simulated": True}
