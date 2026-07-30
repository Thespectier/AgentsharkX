"""Fixed peer reviewer that cannot select tools or routing."""

from __future__ import annotations

from typing import Protocol

from agentshark_demo.models import Scenario


class ReviewerLLM(Protocol):
    def complete(self, stage: str, scenario: Scenario) -> str: ...


def review_risk(llm: ReviewerLLM, scenario: Scenario) -> str:
    return llm.complete("demo.peer_review", scenario)
