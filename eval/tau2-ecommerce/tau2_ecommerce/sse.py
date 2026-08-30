"""Small Server-Sent Events parser used by the tau2 adapter."""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Iterable


@dataclass(frozen=True)
class SseEvent:
    event: str | None
    data: dict[str, Any] | str | None
    event_id: str | None = None


def parse_sse_lines(lines: Iterable[str]) -> list[SseEvent]:
    events: list[SseEvent] = []
    event_name: str | None = None
    event_id: str | None = None
    data_parts: list[str] = []

    def flush() -> None:
        nonlocal event_name, event_id, data_parts
        if event_name is None and event_id is None and not data_parts:
            return
        raw_data = "\n".join(data_parts) if data_parts else None
        parsed: dict[str, Any] | str | None = raw_data
        if raw_data:
            try:
                parsed = json.loads(raw_data)
            except json.JSONDecodeError:
                parsed = raw_data
        events.append(SseEvent(event=event_name, data=parsed, event_id=event_id))
        event_name = None
        event_id = None
        data_parts = []

    for raw_line in lines:
        line = raw_line.rstrip("\r\n")
        if line == "":
            flush()
            continue
        if line.startswith(":"):
            continue
        if ":" in line:
            field, value = line.split(":", 1)
            if value.startswith(" "):
                value = value[1:]
        else:
            field, value = line, ""
        if field == "event":
            event_name = value
        elif field == "id":
            event_id = value
        elif field == "data":
            data_parts.append(value)

    flush()
    return events


def parse_sse_text(text: str) -> list[SseEvent]:
    return parse_sse_lines(text.splitlines())

