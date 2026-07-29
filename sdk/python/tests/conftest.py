from __future__ import annotations

import pytest

from agentshark.integrations.langchain import attachments


@pytest.fixture(autouse=True)
def clear_attachments():
    yield
    # Weak-referenceable agents release on collection; this also isolates tests
    # that intentionally use non-weak-referenceable stand-ins.
    attachments._attachments.clear()
