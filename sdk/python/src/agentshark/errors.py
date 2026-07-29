"""Stable public SDK exceptions."""


class AgentSharkError(Exception):
    """Base class for Agentshark SDK failures."""


class ConfigurationError(AgentSharkError, ValueError):
    """The runtime environment or an explicit identity is invalid."""


AgentSharkConfigurationError = ConfigurationError


class RuntimeClosedError(AgentSharkError):
    """An operation was attempted after the runtime closed."""


class ConcurrentTaskError(AgentSharkError):
    """One mutable AgentGuard runtime was used by concurrent tasks."""


class AgentAlreadyAttachedError(AgentSharkError):
    """A LangChain object is already owned by another runtime."""


class AgentGuardUnavailableError(AgentSharkError):
    """AgentGuard could not initialize under the closed failure policy."""


class AgentGuardAttachError(AgentSharkError):
    """AgentGuard could not attach to a framework object."""


class MCPInstrumentationError(AgentSharkError):
    """An MCP tool could not be marked without changing its call path."""
