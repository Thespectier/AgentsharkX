"""Stable finite Demo error types."""


class DemoError(RuntimeError):
    code = "DEMO_FAILED"


class DemoLLMError(DemoError):
    code = "DEMO_LLM_UNAVAILABLE"


class DemoMCPError(DemoError):
    code = "DEMO_MCP_UNAVAILABLE"


class DemoMCPTimeout(DemoMCPError):
    code = "DEMO_MCP_TIMEOUT"


class DemoCancelled(DemoError):
    code = "DEMO_CANCELLED"
