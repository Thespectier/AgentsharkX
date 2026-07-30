"""Strict OpenAI-compatible chat completion fixture."""

from __future__ import annotations

import re
from typing import Literal

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, ConfigDict, Field

from agentshark_demo.scenarios import FIXTURE_VERSION

DEMO_MODEL = "agentshark-demo-model-v1"
_STAGE_PATTERN = re.compile(r"\[STAGE:(demo\.(?:plan|analyze|peer_review))\]")
_COMPLETIONS = {
    "demo.plan": "Investigate the fixed demo host, then compare deterministic evidence.",
    "demo.analyze": "The fixture evidence has been classified without external access.",
    "demo.peer_review": "Peer review confirms the deterministic risk classification.",
}
_USAGE = {
    "demo.plan": (31, 13),
    "demo.analyze": (37, 12),
    "demo.peer_review": (35, 11),
}

router = APIRouter()


class ChatMessage(BaseModel):
    model_config = ConfigDict(extra="forbid")

    role: Literal["user"]
    content: str = Field(min_length=1, max_length=2_048)


class ChatRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    model: str
    messages: list[ChatMessage] = Field(min_length=1, max_length=1)
    stream: bool = False
    temperature: int = Field(default=0, ge=0, le=0)


@router.get("/healthz")
def health() -> dict[str, str]:
    return {"status": "healthy", "fixtureVersion": FIXTURE_VERSION}


@router.get("/v1/models")
def models() -> dict[str, object]:
    return {
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


@router.post("/v1/chat/completions")
def chat_completions(request: ChatRequest) -> dict[str, object]:
    if request.model != DEMO_MODEL or request.stream:
        raise HTTPException(status_code=400, detail="unsupported deterministic request")
    matches = _STAGE_PATTERN.findall(request.messages[0].content)
    if len(matches) != 1:
        raise HTTPException(status_code=400, detail="unknown deterministic stage")
    stage = matches[0]
    prompt_tokens, completion_tokens = _USAGE[stage]
    return {
        "id": f"chatcmpl-agentshark-{stage.removeprefix('demo.').replace('_', '-')}",
        "object": "chat.completion",
        "created": 1_784_688_000,
        "model": DEMO_MODEL,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": _COMPLETIONS[stage]},
                "finish_reason": "stop",
            }
        ],
        "usage": {
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
            "total_tokens": prompt_tokens + completion_tokens,
        },
    }
