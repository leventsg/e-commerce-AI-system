"""tau2-compatible agent adapter for the Go AI customer service."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from .client import AiCustomerServiceClient, ChatResult

try:
    from tau2.agent.base_agent import HalfDuplexAgent
    from tau2.data_model.message import AssistantMessage
except Exception:  # pragma: no cover - exercised when tau2 is not installed.
    HalfDuplexAgent = object
    AssistantMessage = None


@dataclass
class SimpleAssistantMessage:
    role: str
    content: str
    raw_data: dict[str, Any] | None = None


@dataclass
class EcommerceGoAgentState:
    conversation_id: str = ""
    turn_index: int = 0
    last_events: list[dict[str, Any]] = field(default_factory=list)
    confirmations: list[dict[str, Any]] = field(default_factory=list)


def _message_content(message: Any) -> str:
    if isinstance(message, str):
        return message
    content = getattr(message, "content", None)
    if isinstance(content, str):
        return content
    if isinstance(message, dict) and isinstance(message.get("content"), str):
        return str(message["content"])
    return str(message)


def _assistant_message(content: str, raw_data: dict[str, Any] | None = None) -> Any:
    if AssistantMessage is not None:
        try:
            return AssistantMessage.text(content, raw_data=raw_data)
        except Exception:
            return AssistantMessage(role="assistant", content=content, raw_data=raw_data)
    return SimpleAssistantMessage(role="assistant", content=content, raw_data=raw_data)


def _task_id(task: Any) -> str:
    return str(getattr(task, "id", "") or "")


def _task_requests_new_conversation(task: Any) -> bool:
    initial_state = getattr(task, "initial_state", None)
    initialization_data = getattr(initial_state, "initialization_data", None)
    agent_data = getattr(initialization_data, "agent_data", None)
    if isinstance(agent_data, dict) and agent_data.get("conversation_scope") == "new":
        return True
    description = getattr(task, "description", None)
    notes = str(getattr(description, "notes", "") or "").lower()
    return "conversation_scope=new" in notes or "new conversation" in notes or "fresh conversation" in notes


class EcommerceGoAgent(HalfDuplexAgent):
    """Adapter that lets tau2 evaluate the existing Go customer-service API.

    The adapter deliberately does not import or call Go business code. It sends
    each tau2 user turn to `/douyin/ai/chat`, consumes the SSE stream, and
    returns the final assistant message back to tau2.
    """

    def __init__(
        self,
        tools: list[Any] | None = None,
        domain_policy: str | None = None,
        *,
        base_url: str | None = None,
        auth_token: str | None = None,
        refresh_token: str | None = None,
        auto_confirm: str = "wait",
        mode: str = "http",
        runner_command: list[str] | None = None,
        seed: int = 0,
        initial_conversation_id: str | None = None,
        new_conversation_task_ids: set[str] | list[str] | tuple[str, ...] | None = None,
        task: Any = None,
        timeout_seconds: float = 60,
        **_: Any,
    ) -> None:
        try:
            super().__init__(tools=tools or [], domain_policy=domain_policy or "")
        except TypeError:
            self.tools = tools or []
            self.domain_policy = domain_policy or ""
        self.auto_confirm = auto_confirm
        self.client = AiCustomerServiceClient(
            base_url=base_url,
            auth_token=auth_token,
            refresh_token=refresh_token,
            timeout_seconds=timeout_seconds,
        )
        self.task = task
        self.mode = mode
        self.seed = seed
        if mode == "simulation":
            from .simulation import SimulationClient
            self.client = SimulationClient(runner_command, timeout_seconds=timeout_seconds)
        elif mode != "http":
            raise ValueError("mode must be http or simulation")
        self.initial_conversation_id = initial_conversation_id or ""
        self.new_conversation_task_ids = set(new_conversation_task_ids or [])

    def get_init_state(self, message_history: list[Any] | None = None) -> EcommerceGoAgentState:
        conversation_id = ""
        if not self._should_start_new_conversation():
            conversation_id = self.initial_conversation_id
        if self.mode == "simulation":
            from .contract import runner_init
            from uuid import uuid4
            task = self.task if isinstance(self.task, dict) else {}
            if not task:
                initial = getattr(getattr(self.task, "initial_state", None), "initialization_data", None)
                data = getattr(initial, "agent_data", {}) or {}
                task = {"fixture": data.get("fixture", data.get("fixtures", {})), "failures": data.get("failures", [])}
            result = self.client.initialize(runner_init(task, uuid4().hex, self.seed))
            conversation_id = result.get("conversation_id", "")
        return EcommerceGoAgentState(conversation_id=conversation_id, turn_index=len(message_history or []))

    def generate_next_message(
        self,
        message: Any,
        state: EcommerceGoAgentState | None = None,
        **_: Any,
    ) -> tuple[Any, EcommerceGoAgentState]:
        state = state or self.get_init_state()
        result = self.client.send_user_message(
            _message_content(message),
            state.conversation_id or None,
            metadata=self._task_runtime_metadata(),
        )
        if result.confirmations and self.auto_confirm in {"approve", "reject"}:
            approved = self.auto_confirm == "approve"
            result = self._handle_confirmation(result, approved=approved)

        if result.conversation_id:
            state.conversation_id = result.conversation_id
        state.turn_index += 1
        state.last_events = result.events
        state.confirmations = result.confirmations
        content = result.assistant_text or self._fallback_content(result)
        raw_data = {
            "conversation_id": state.conversation_id,
            "tau2_ecommerce_events": result.events,
            "tau2_ecommerce_confirmations": result.confirmations,
            "tau2_ecommerce_trajectory": result.trajectory,
            "tau2_ecommerce_state": result.state,
            "trace_id": result.trace_id,
        }
        return _assistant_message(content, raw_data=raw_data), state

    def _task_runtime_metadata(self) -> dict[str, Any] | None:
        task_id = _task_id(self.task)
        metadata: dict[str, Any] = {}
        if task_id:
            metadata["scenario_id"] = task_id
        return metadata or None

    def _should_start_new_conversation(self) -> bool:
        task_id = _task_id(self.task)
        return task_id in self.new_conversation_task_ids or _task_requests_new_conversation(self.task)

    def _handle_confirmation(self, result: ChatResult, approved: bool) -> ChatResult:
        current = result
        for confirmation in result.confirmations:
            confirmation_id = confirmation.get("confirmation_id")
            if not confirmation_id:
                continue
            resumed = self.client.confirm_action(
                conversation_id=result.conversation_id,
                confirmation_id=str(confirmation_id),
                approved=approved,
            )
            current = ChatResult(
                conversation_id=resumed.conversation_id or current.conversation_id,
                assistant_text=resumed.assistant_text or current.assistant_text,
                events=current.events + resumed.events,
                confirmations=resumed.confirmations,
                raw_lines=current.raw_lines + resumed.raw_lines,
                trajectory=list({entry.get("id", str(i)): entry for i, entry in enumerate(current.trajectory + resumed.trajectory)}.values()),
                state=resumed.state or current.state,
                trace_id=resumed.trace_id or current.trace_id,
            )
        return current

    @staticmethod
    def _fallback_content(result: ChatResult) -> str:
        if result.confirmations:
            first = result.confirmations[0]
            return "confirmation_required: " + str(first.get("summary") or first.get("action") or "")
        return "No final assistant message was returned by the customer service API."


def create_ecommerce_go_agent(tools: list[Any], domain_policy: str, **kwargs: Any) -> EcommerceGoAgent:
    """Factory used by tau2 registry."""

    llm_args = kwargs.get("llm_args") or {}
    auto_confirm = kwargs.get("auto_confirm") or llm_args.get("auto_confirm", "wait")
    new_conversation_task_ids = kwargs.get("new_conversation_task_ids") or llm_args.get("new_conversation_task_ids")
    initial_conversation_id = kwargs.get("initial_conversation_id") or llm_args.get("initial_conversation_id")
    return EcommerceGoAgent(
        tools=tools,
        domain_policy=domain_policy,
        base_url=kwargs.get("base_url"),
        auth_token=kwargs.get("auth_token"),
        refresh_token=kwargs.get("refresh_token"),
        auto_confirm=auto_confirm,
        initial_conversation_id=initial_conversation_id,
        new_conversation_task_ids=new_conversation_task_ids,
        task=kwargs.get("task"),
        mode=kwargs.get("mode", llm_args.get("mode", "http")),
        runner_command=kwargs.get("runner_command", llm_args.get("runner_command")),
        seed=kwargs.get("seed", llm_args.get("seed", 0)),
        timeout_seconds=kwargs.get("timeout_seconds", 60),
    )
