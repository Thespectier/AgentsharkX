"""One internal ASGI process for the LLM and MCP fixtures."""

from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI

from agentshark_demo.fixtures.llm import router as llm_router
from agentshark_demo.fixtures.mcp import mcp_fixture

mcp_app = mcp_fixture.streamable_http_app()


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    _ = app
    async with mcp_fixture.session_manager.run():
        yield


app = FastAPI(
    title="Agentshark Demo Fixtures",
    docs_url=None,
    redoc_url=None,
    openapi_url=None,
    lifespan=lifespan,
)
app.include_router(llm_router)
app.mount("/", mcp_app)


def main() -> None:
    uvicorn.run(app, host="0.0.0.0", port=39200, log_level="info")
