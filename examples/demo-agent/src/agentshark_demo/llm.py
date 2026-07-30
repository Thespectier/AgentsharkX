"""Three-stage OpenAI-compatible LLM client routed through agentgateway."""

from __future__ import annotations

from collections.abc import Callable, MutableMapping
from typing import Any

import httpx
from langchain_core.callbacks.manager import CallbackManagerForLLMRun
from langchain_core.language_models.llms import LLM
from pydantic import ConfigDict, Field

from agentshark_demo.errors import DemoLLMError
from agentshark_demo.models import Scenario
from agentshark_demo.scenarios import stage_prompt


class GatewayFixtureLLM(LLM):
    """Minimal LangChain LLM with no retry, streaming, or fallback path."""

    model_config = ConfigDict(arbitrary_types_allowed=True)

    base_url: str
    model_name: str
    timeout_seconds: float = Field(gt=0)
    inject_context: Callable[[MutableMapping[str, str]], MutableMapping[str, str]] = Field(
        exclude=True
    )

    @property
    def _llm_type(self) -> str:
        return "agentshark-demo-gateway-fixture"

    @property
    def _identifying_params(self) -> dict[str, Any]:
        return {"model": self.model_name, "base_url": self.base_url}

    def _call(
        self,
        prompt: str,
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> str:
        _ = (run_manager, kwargs)
        if stop:
            raise DemoLLMError(DemoLLMError.code)
        headers: dict[str, str] = {
            "Content-Type": "application/json",
            "X-Agentshark-Demo": "deterministic-fixture",
        }
        self.inject_context(headers)
        try:
            response = httpx.post(
                f"{self.base_url}/chat/completions",
                headers=headers,
                json={
                    "model": self.model_name,
                    "messages": [{"role": "user", "content": prompt}],
                    "stream": False,
                    "temperature": 0,
                },
                timeout=self.timeout_seconds,
            )
            response.raise_for_status()
            payload = response.json()
            if not isinstance(payload, dict):
                raise ValueError("invalid completion payload")
            choices = payload.get("choices")
            if not isinstance(choices, list) or len(choices) != 1:
                raise ValueError("invalid choices")
            choice = choices[0]
            if not isinstance(choice, dict):
                raise ValueError("invalid choice")
            message = choice.get("message")
            content = message.get("content") if isinstance(message, dict) else None
            if not isinstance(content, str) or not content:
                raise ValueError("missing content")
            return content
        except (httpx.HTTPError, TypeError, ValueError) as exc:
            raise DemoLLMError(DemoLLMError.code) from exc


class StageLLMClient:
    def __init__(self, model: LLM) -> None:
        self._model = model

    def complete(self, stage: str, scenario: Scenario) -> str:
        return self._model.invoke(stage_prompt(stage, scenario))
