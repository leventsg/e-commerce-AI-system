"""Bounded JSONL transport to the isolated Go runner, never to business RPC."""
from __future__ import annotations

import json
import os
from pathlib import Path
import queue
import subprocess
import threading
from typing import Any

from .client import ChatResult


class SimulationClient:
    def __init__(self, command: list[str] | None = None, *, timeout_seconds=120):
        self.command = command or json.loads(os.getenv("AI_EVAL_RUNNER_COMMAND", '["go","run","./services/aiagent/cmd/evalrunner"]'))
        self.timeout = timeout_seconds
        self.trial_id = ""
        self.process = None
        self.responses = queue.Queue(maxsize=2)

    def _start(self):
        if self.process is not None:
            return
        root = Path(__file__).resolve().parents[3]
        self.process = subprocess.Popen(self.command, cwd=root, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=None, text=True, encoding="utf-8", bufsize=1)
        def read():
            try:
                for line in self.process.stdout:
                    try:
                        self.responses.put(json.loads(line), timeout=self.timeout)
                    except json.JSONDecodeError:
                        self.responses.put(RuntimeError("runner wrote non-JSON stdout"), timeout=self.timeout)
                        break
            finally:
                try:
                    self.responses.put(EOFError("runner exited"), timeout=1)
                except queue.Full:
                    pass
        threading.Thread(target=read, daemon=True).start()

    def request(self, payload: dict) -> dict:
        self._start()
        self.process.stdin.write(json.dumps(payload, ensure_ascii=False) + "\n")
        self.process.stdin.flush()
        try:
            response = self.responses.get(timeout=self.timeout)
        except queue.Empty as exc:
            self.close()
            raise TimeoutError("Go evaluation runner response timed out") from exc
        if isinstance(response, Exception):
            raise response
        if response.get("error"):
            raise RuntimeError(str(response["error"]))
        return response

    def schemas(self):
        result = self.request({"op": "schema"})
        return result.get("schemas", result.get("tools", {}))

    def initialize(self, payload):
        self.trial_id = payload["trial_id"]
        return self.request(payload)

    @staticmethod
    def result(response):
        events = response.get("events") or []
        return ChatResult(conversation_id=response.get("conversation_id", ""), assistant_text=response.get("assistant_text", ""),
                          events=events, confirmations=[e for e in events if e.get("type") == "confirmation_required"],
                          trajectory=response.get("trajectory") or [], state=response.get("state") or {}, trace_id=response.get("trace_id", ""))

    def send_user_message(self, content, conversation_id=None, metadata=None):
        # Metadata, scoring rubrics and expected outcomes are deliberately not forwarded.
        return self.result(self.request({"op": "chat", "trial_id": self.trial_id, "content": content}))

    def confirm_action(self, conversation_id, confirmation_id, approved=False):
        return self.result(self.request({"op": "confirm", "trial_id": self.trial_id, "confirmation_id": confirmation_id, "approved": approved}))

    def end(self):
        return self.request({"op": "end", "trial_id": self.trial_id})

    def close(self):
        if self.process is not None:
            self.process.stdin.close()
            try:
                # The Go runner allows five seconds for the observability flush.
                # Leave enough time for that shutdown before forcefully terminating.
                self.process.wait(timeout=7)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=3)
            self.process.stdout.close()
            self.process = None

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()
