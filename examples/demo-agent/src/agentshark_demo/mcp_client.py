"""Streamable HTTP MCP client for the fixed security fixture."""

from __future__ import annotations

import asyncio
import json
from typing import Any

from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client
from mcp.types import TextContent

from agentshark_demo.errors import DemoMCPError, DemoMCPTimeout


class MCPFixtureClient:
    def __init__(self, url: str) -> None:
        self._url = url

    def asset_lookup(self, hostname: str) -> dict[str, Any]:
        return asyncio.run(self._call("asset_lookup", {"hostname": hostname}))

    def threat_intel_lookup(self, indicator: str, scenario: str) -> dict[str, Any]:
        return asyncio.run(
            self._call(
                "threat_intel_lookup",
                {"indicator": indicator, "scenario": scenario},
            )
        )

    async def _call(self, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
        try:
            async with streamable_http_client(self._url) as (read, write, _):
                async with ClientSession(read, write) as session:
                    await session.initialize()
                    result = await session.call_tool(name, arguments=arguments)
        except Exception as exc:
            raise DemoMCPError(DemoMCPError.code) from exc

        rendered = _result_text(result.content)
        if result.isError:
            if "DEMO_MCP_TIMEOUT" in rendered:
                raise DemoMCPTimeout(DemoMCPTimeout.code)
            raise DemoMCPError(DemoMCPError.code)
        structured = result.structuredContent
        if isinstance(structured, dict):
            return structured
        try:
            parsed = json.loads(rendered)
        except json.JSONDecodeError as exc:
            raise DemoMCPError(DemoMCPError.code) from exc
        if not isinstance(parsed, dict):
            raise DemoMCPError(DemoMCPError.code)
        return parsed


def _result_text(content: list[Any]) -> str:
    return "\n".join(item.text for item in content if isinstance(item, TextContent))
