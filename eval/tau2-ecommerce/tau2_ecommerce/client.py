"""HTTP client for the Go AI customer service SSE endpoint."""

from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any
from uuid import uuid4

from .sse import SseEvent, parse_sse_lines


@dataclass
class ChatResult:
    conversation_id: str
    assistant_text: str
    events: list[dict[str, Any]] = field(default_factory=list)
    confirmations: list[dict[str, Any]] = field(default_factory=list)
    raw_lines: list[str] = field(default_factory=list)


class AiCustomerServiceClient:
    def __init__(
        self,
        base_url: str | None = None,
        auth_token: str | None = None,
        refresh_token: str | None = None,
        chat_path: str = "/douyin/ai/chat",
        timeout_seconds: float = 60,
        source: str = "tau2",
    ) -> None:
        self.base_url = (base_url or os.getenv("AI_CS_BASE_URL") or "http://localhost:8007").rstrip("/")
        self.auth_token = auth_token if auth_token is not None else os.getenv("AI_CS_AUTH_TOKEN", "")
        self.refresh_token = refresh_token if refresh_token is not None else os.getenv("AI_CS_REFRESH_TOKEN", "")
        self.chat_path = chat_path
        self.timeout_seconds = timeout_seconds
        self.source = source

    def send_user_message(
        self,
        content: str,
        conversation_id: str | None = None,
        metadata: dict[str, Any] | None = None,
    ) -> ChatResult:
        request_metadata = {"source": self.source}
        if metadata:
            request_metadata.update(metadata)
        payload = {
            "type": "user_message",
            "client_message_id": f"tau2_msg_{uuid4().hex}",
            "content": content,
            "metadata": request_metadata,
        }
        if conversation_id:
            payload["conversation_id"] = conversation_id
        return self._post_chat(payload)

    def confirm_action(
        self,
        conversation_id: str,
        confirmation_id: str,
        approved: bool = True,
    ) -> ChatResult:
        payload = {
            "type": "confirm_action",
            "conversation_id": conversation_id,
            "confirmation_id": confirmation_id,
            "approved": approved,
        }
        return self._post_chat(payload)

    def _post_chat(self, payload: dict[str, Any]) -> ChatResult:
        return self._post_chat_once(payload, allow_refresh=True)

    def _post_chat_once(self, payload: dict[str, Any], *, allow_refresh: bool) -> ChatResult:
        url = self.base_url + self.chat_path
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers = {
            "Content-Type": "application/json",
            "Accept": "text/event-stream",
        }
        if self.auth_token:
            headers["Access-Token"] = self.auth_token
            headers["Authorization"] = f"Bearer {self.auth_token}"
        if self.refresh_token:
            headers["Cookie"] = f"Refresh-Token={self.refresh_token}"
        request = urllib.request.Request(url, data=body, headers=headers, method="POST")
        try:
            with urllib.request.urlopen(request, timeout=self.timeout_seconds) as response:
                raw_lines = [line.decode("utf-8", errors="replace") for line in response.readlines()]
                content_type = response.headers.get("Content-Type", "")
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"AI customer service HTTP {exc.code}: {detail}") from exc
        except urllib.error.URLError as exc:
            raise RuntimeError(f"AI customer service request failed: {exc}") from exc

        if "text/event-stream" not in content_type.lower():
            preview = "".join(raw_lines).strip()
            if allow_refresh and "application/json" in content_type.lower() and self._refresh_tokens_from_response(preview):
                return self._post_chat_once(payload, allow_refresh=False)
            if len(preview) > 500:
                preview = preview[:500] + "..."
            raise RuntimeError(
                f"AI customer service non-SSE response: content_type={content_type!r} body={preview!r}"
            )

        return self._events_to_result(payload.get("conversation_id", ""), parse_sse_lines(raw_lines), raw_lines=raw_lines)

    def _refresh_tokens_from_response(self, body: str) -> bool:
        try:
            payload = json.loads(body)
        except json.JSONDecodeError:
            return False
        if not isinstance(payload, dict):
            return False
        data = payload.get("data")
        if not isinstance(data, dict):
            return False
        access_token = data.get("access_token")
        refresh_token = data.get("refresh_token")
        if not access_token or not refresh_token:
            return False
        self.auth_token = str(access_token)
        self.refresh_token = str(refresh_token)
        os.environ["AI_CS_AUTH_TOKEN"] = self.auth_token
        os.environ["AI_CS_REFRESH_TOKEN"] = self.refresh_token
        return True

    @staticmethod
    def _events_to_result(
        conversation_id: str,
        sse_events: list[SseEvent],
        raw_lines: list[str] | None = None,
    ) -> ChatResult:
        assistant_text_parts: list[str] = []
        assistant_delta_parts: list[str] = []
        events: list[dict[str, Any]] = []
        confirmations: list[dict[str, Any]] = []
        resolved_conversation_id = conversation_id
        for sse_event in sse_events:
            data = sse_event.data
            if not isinstance(data, dict):
                continue
            events.append(data)
            if data.get("conversation_id"):
                resolved_conversation_id = str(data["conversation_id"])
            event_type = data.get("type") or sse_event.event
            if event_type == "assistant_message" and data.get("content"):
                assistant_text_parts.append(str(data["content"]))
            elif event_type == "assistant_delta" and data.get("content"):
                assistant_delta_parts.append(str(data["content"]))
            elif event_type == "error" and data.get("content"):
                assistant_text_parts.append(str(data["content"]))
            elif event_type == "confirmation_required":
                confirmations.append(data)
        assistant_text = "\n".join(part for part in assistant_text_parts if part).strip()
        if not assistant_text:
            assistant_text = "".join(assistant_delta_parts).strip()
        return ChatResult(
            conversation_id=resolved_conversation_id,
            assistant_text=assistant_text,
            events=events,
            confirmations=confirmations,
            raw_lines=raw_lines or [],
        )


def backoff_sleep(attempt: int) -> None:
    time.sleep(min(0.5 * (2**attempt), 5.0))
