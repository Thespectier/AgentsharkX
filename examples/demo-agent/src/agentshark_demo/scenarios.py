"""Immutable scenario definitions and expected counts."""

from __future__ import annotations

from dataclasses import dataclass

from agentshark_demo.models import ExpectedMetrics, Outcome, Scenario

FIXTURE_VERSION = "v1"
ROOT_AGENT_ID = "demo-incident-investigator"
PEER_AGENT_ID = "demo-risk-reviewer"
MCP_SERVER_NAME = "demo-security"
DEMO_HOST = "web-01"
DEMO_INDICATOR = "203.0.113.42"
DEMO_ACTION_URL = "https://quarantine.example.com/hosts/web-01"

BASE_STEPS = (
    "bootstrap",
    "plan",
    "asset_lookup",
    "threat_intel_lookup",
    "analyze_evidence",
    "calculate_risk_score",
    "invoke_risk_reviewer",
    "render_report",
    "finish",
)


@dataclass(frozen=True, slots=True)
class ScenarioSpec:
    scenario: Scenario
    expected_outcome: Outcome
    expected_metrics: ExpectedMetrics
    risk_label: str

    @property
    def steps(self) -> tuple[str, ...]:
        if self.scenario is Scenario.APPROVAL:
            return (*BASE_STEPS[:7], "guarded_action", *BASE_STEPS[7:])
        return BASE_STEPS


SCENARIOS = {
    Scenario.HAPPY: ScenarioSpec(
        scenario=Scenario.HAPPY,
        expected_outcome=Outcome.NORMAL,
        expected_metrics=ExpectedMetrics(
            llmCalls=3,
            mcpCalls=2,
            localToolCalls=1,
            a2aCalls=1,
            humanChecks=0,
        ),
        risk_label="low",
    ),
    Scenario.APPROVAL: ScenarioSpec(
        scenario=Scenario.APPROVAL,
        expected_outcome=Outcome.APPROVED,
        expected_metrics=ExpectedMetrics(
            llmCalls=3,
            mcpCalls=2,
            localToolCalls=2,
            a2aCalls=1,
            humanChecks=1,
        ),
        risk_label="high",
    ),
    Scenario.FAILURE: ScenarioSpec(
        scenario=Scenario.FAILURE,
        expected_outcome=Outcome.DEGRADED,
        expected_metrics=ExpectedMetrics(
            llmCalls=3,
            mcpCalls=2,
            localToolCalls=1,
            a2aCalls=1,
            humanChecks=0,
        ),
        risk_label="degraded",
    ),
}


def scenario_spec(scenario: Scenario) -> ScenarioSpec:
    return SCENARIOS[scenario]


def stage_prompt(stage: str, scenario: Scenario) -> str:
    if stage not in {"demo.plan", "demo.analyze", "demo.peer_review"}:
        raise ValueError("unknown deterministic LLM stage")
    return "\n".join(
        (
            "SIMULATED DETERMINISTIC FIXTURE",
            f"[STAGE:{stage}]",
            f"scenario={scenario.value}",
            f"host={DEMO_HOST}",
            f"indicator={DEMO_INDICATOR}",
        )
    )
