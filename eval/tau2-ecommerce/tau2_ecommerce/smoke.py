"""Manual smoke runner for the adapter without requiring tau2."""

from __future__ import annotations

import sys

from .agent import EcommerceGoAgent


def main() -> int:
    message = " ".join(sys.argv[1:]).strip() or "帮我查一下我的订单"
    agent = EcommerceGoAgent()
    assistant_message, state = agent.generate_next_message(message)
    print(getattr(assistant_message, "content", str(assistant_message)))
    print(f"\nconversation_id={state.conversation_id}")
    print(f"events={len(state.last_events)} confirmations={len(state.confirmations)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
