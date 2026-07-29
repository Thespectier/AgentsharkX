"""Object-level LangChain attachment ownership."""

from __future__ import annotations

import threading
import weakref
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any

from agentshark.errors import AgentAlreadyAttachedError


@dataclass(slots=True)
class _Attachment:
    runtime_id: str
    weak_agent: weakref.ReferenceType[Any] | None
    strong_agent: Any | None
    ready: threading.Event = field(default_factory=threading.Event)
    error: BaseException | None = None

    def agent(self) -> Any | None:
        return self.weak_agent() if self.weak_agent is not None else self.strong_agent


_ATTACHMENTS: dict[int, _Attachment] = {}
_ATTACHMENT_LOCK = threading.RLock()


def attach_once(
    agent: Any,
    runtime_id: str,
    attach: Callable[[Any], Any],
) -> Any:
    """Attach exactly once and keep ownership stable for the object's lifetime."""

    key = id(agent)
    owner = False
    with _ATTACHMENT_LOCK:
        existing = _ATTACHMENTS.get(key)
        if existing is not None and existing.agent() is not agent:
            _ATTACHMENTS.pop(key, None)
            existing = None
        if existing is not None:
            if existing.runtime_id != runtime_id:
                raise AgentAlreadyAttachedError(
                    "this LangChain object is already attached to another AgentShark runtime"
                )
            attachment = existing
        else:
            def remove_dead_attachment(
                reference: weakref.ReferenceType[Any],
                object_id: int = key,
            ) -> None:
                _remove_dead_attachment(object_id, reference)

            try:
                weak_agent = weakref.ref(agent, remove_dead_attachment)
                strong_agent = None
            except TypeError:
                weak_agent = None
                strong_agent = agent
            attachment = _Attachment(runtime_id, weak_agent, strong_agent)
            _ATTACHMENTS[key] = attachment
            owner = True

    if not owner:
        attachment.ready.wait()
        if attachment.error is not None:
            raise attachment.error
        return agent

    try:
        result = attach(agent)
    except BaseException as exc:
        with _ATTACHMENT_LOCK:
            attachment.error = exc
            current = _ATTACHMENTS.get(key)
            if current is attachment:
                _ATTACHMENTS.pop(key, None)
        attachment.ready.set()
        raise
    else:
        attachment.ready.set()
        return result


def _remove_dead_attachment(object_id: int, reference: weakref.ReferenceType[Any]) -> None:
    with _ATTACHMENT_LOCK:
        current = _ATTACHMENTS.get(object_id)
        if current is not None and current.weak_agent is reference:
            _ATTACHMENTS.pop(object_id, None)


class _AttachmentRegistryView:
    """Test-visible registry without exposing it from the package root."""

    _attachments = _ATTACHMENTS


attachments = _AttachmentRegistryView()
