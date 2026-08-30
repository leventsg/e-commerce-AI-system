import json
import pathlib
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from types import SimpleNamespace


ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "eval" / "tau2-ecommerce"))

from tau2_ecommerce.client import AiCustomerServiceClient
from tau2_ecommerce.agent import EcommerceGoAgent, EcommerceGoAgentState, create_ecommerce_go_agent
from tau2_ecommerce.sse import parse_sse_text


class SseParserTest(unittest.TestCase):
    def test_client_defaults_to_local_ai_gateway_port(self):
        client = AiCustomerServiceClient(auth_token="")

        self.assertEqual(client.base_url, "http://localhost:8007")

    def test_client_sends_go_gateway_auth_headers(self):
        captured = {}

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                captured["access_token"] = self.headers.get("Access-Token")
                captured["authorization"] = self.headers.get("Authorization")
                captured["cookie"] = self.headers.get("Cookie")
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()
                self.wfile.write(
                    b'event: assistant_message\n'
                    b'data: {"type":"assistant_message","content":"ok"}\n\n'
                )

            def log_message(self, format, *args):
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = AiCustomerServiceClient(
                base_url=f"http://127.0.0.1:{server.server_port}",
                auth_token="access-token",
                refresh_token="refresh-token",
            )
            result = client.send_user_message("hello", "conv-1")
        finally:
            server.shutdown()
            thread.join(timeout=5)
            server.server_close()

        self.assertEqual(result.assistant_text, "ok")
        self.assertEqual(captured["access_token"], "access-token")
        self.assertIn("Bearer access-token", captured["authorization"])
        self.assertIn("Refresh-Token=refresh-token", captured["cookie"])

    def test_client_omits_conversation_id_for_new_conversation(self):
        captured = {}

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                captured["body"] = json.loads(self.rfile.read(length).decode("utf-8"))
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()
                self.wfile.write(
                    b'event: assistant_message\n'
                    b'data: {"type":"assistant_message","conversation_id":"conv_server","content":"ok"}\n\n'
                )

            def log_message(self, format, *args):
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = AiCustomerServiceClient(base_url=f"http://127.0.0.1:{server.server_port}")
            result = client.send_user_message("hello")
        finally:
            server.shutdown()
            thread.join(timeout=5)
            server.server_close()

        self.assertNotIn("conversation_id", captured["body"])
        self.assertEqual(result.conversation_id, "conv_server")

    def test_client_reports_non_sse_response_body(self):
        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"code":401,"msg":"auth blank"}')

            def log_message(self, format, *args):
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = AiCustomerServiceClient(base_url=f"http://127.0.0.1:{server.server_port}")
            with self.assertRaisesRegex(RuntimeError, "non-SSE response"):
                client.send_user_message("hello", "conv-1")
        finally:
            server.shutdown()
            thread.join(timeout=5)
            server.server_close()

    def test_parse_json_events(self):
        text = (
            "event: assistant_message\n"
            "id: msg_1\n"
            "data: {\"type\":\"assistant_message\",\"content\":\"ok\"}\n"
            "\n"
            ": ping\n"
            "\n"
            "event: confirmation_required\n"
            "data: {\"type\":\"confirmation_required\",\"confirmation_id\":\"c1\"}\n"
            "\n"
        )

        events = parse_sse_text(text)

        self.assertEqual(len(events), 2)
        self.assertEqual(events[0].event, "assistant_message")
        self.assertEqual(events[0].event_id, "msg_1")
        self.assertEqual(events[0].data["content"], "ok")
        self.assertEqual(events[1].data["confirmation_id"], "c1")

    def test_client_maps_assistant_and_confirmation_events(self):
        events = parse_sse_text(
            "event: confirmation_required\n"
            "data: {\"type\":\"confirmation_required\",\"confirmation_id\":\"c1\",\"summary\":\"confirm\"}\n"
            "\n"
            "event: assistant_message\n"
            "data: {\"type\":\"assistant_message\",\"content\":\"done\"}\n"
            "\n"
        )

        result = AiCustomerServiceClient._events_to_result("conv_1", events)

        self.assertEqual(result.conversation_id, "conv_1")
        self.assertEqual(result.assistant_text, "done")
        self.assertEqual(result.confirmations[0]["confirmation_id"], "c1")

    def test_client_uses_server_conversation_id_from_events(self):
        events = parse_sse_text(
            "event: assistant_message\n"
            "data: {\"type\":\"assistant_message\",\"conversation_id\":\"conv_server\",\"content\":\"done\"}\n"
            "\n"
        )

        result = AiCustomerServiceClient._events_to_result("", events)

        self.assertEqual(result.conversation_id, "conv_server")

    def test_client_uses_assistant_deltas_when_final_message_is_missing(self):
        events = parse_sse_text(
            "event: assistant_delta\n"
            "data: {\"type\":\"assistant_delta\",\"content\":\"订单\"}\n"
            "\n"
            "event: assistant_delta\n"
            "data: {\"type\":\"assistant_delta\",\"content\":\"已发货\"}\n"
            "\n"
            "event: done\n"
            "data: {\"type\":\"done\",\"done\":true}\n"
            "\n"
        )

        result = AiCustomerServiceClient._events_to_result("conv_1", events)

        self.assertEqual(result.assistant_text, "订单已发货")

    def test_agent_auto_approves_confirmation(self):
        class FakeClient:
            def send_user_message(self, content, conversation_id, metadata=None):
                self.content = content
                self.metadata = metadata
                return AiCustomerServiceClient._events_to_result(
                    conversation_id,
                    parse_sse_text(
                        "event: confirmation_required\n"
                        "data: {\"type\":\"confirmation_required\",\"confirmation_id\":\"c1\"}\n\n"
                    ),
                )

            def confirm_action(self, conversation_id, confirmation_id, approved=True):
                self.confirmed = (conversation_id, confirmation_id, approved)
                return AiCustomerServiceClient._events_to_result(
                    conversation_id,
                    parse_sse_text(
                        "event: assistant_message\n"
                        "data: {\"type\":\"assistant_message\",\"content\":\"created\"}\n\n"
                    ),
                )

        agent = EcommerceGoAgent(base_url="http://unused")
        agent.client = FakeClient()

        message, state = agent.generate_next_message(
            {"content": "create order"},
            EcommerceGoAgentState(conversation_id="conv_1"),
        )

        self.assertEqual(getattr(message, "content"), "created")
        self.assertEqual(agent.client.confirmed, ("conv_1", "c1", True))
        self.assertEqual(state.turn_index, 1)

    def test_agent_auto_rejects_confirmation(self):
        class FakeClient:
            def send_user_message(self, content, conversation_id, metadata=None):
                return AiCustomerServiceClient._events_to_result(
                    conversation_id,
                    parse_sse_text(
                        "event: confirmation_required\n"
                        "data: {\"type\":\"confirmation_required\",\"confirmation_id\":\"c1\"}\n\n"
                    ),
                )

            def confirm_action(self, conversation_id, confirmation_id, approved=True):
                self.confirmed = (conversation_id, confirmation_id, approved)
                return AiCustomerServiceClient._events_to_result(
                    conversation_id,
                    parse_sse_text(
                        "event: assistant_message\n"
                        "data: {\"type\":\"assistant_message\",\"content\":\"cancelled\"}\n\n"
                    ),
                )

        agent = EcommerceGoAgent(base_url="http://unused", auto_confirm="reject")
        agent.client = FakeClient()

        message, state = agent.generate_next_message(
            {"content": "create order"},
            EcommerceGoAgentState(conversation_id="conv_1"),
        )

        self.assertEqual(getattr(message, "content"), "cancelled")
        self.assertEqual(agent.client.confirmed, ("conv_1", "c1", False))
        self.assertEqual(state.turn_index, 1)

    def test_agent_can_leave_confirmation_pending(self):
        class FakeClient:
            def send_user_message(self, content, conversation_id, metadata=None):
                return AiCustomerServiceClient._events_to_result(
                    conversation_id,
                    parse_sse_text(
                        "event: confirmation_required\n"
                        "data: {\"type\":\"confirmation_required\",\"confirmation_id\":\"c1\",\"summary\":\"confirm create\"}\n\n"
                    ),
                )

            def confirm_action(self, conversation_id, confirmation_id, approved=True):
                raise AssertionError("confirm_action should not be called")

        agent = EcommerceGoAgent(base_url="http://unused", auto_confirm="none")
        agent.client = FakeClient()

        message, state = agent.generate_next_message(
            {"content": "create order"},
            EcommerceGoAgentState(conversation_id="conv_1"),
        )

        self.assertIn("confirmation_required", getattr(message, "content"))
        self.assertEqual(state.confirmations[0]["confirmation_id"], "c1")
        self.assertEqual(state.turn_index, 1)

    def test_agent_factory_reads_auto_confirm_from_llm_args(self):
        agent = create_ecommerce_go_agent(
            [],
            "",
            llm_args={"auto_confirm": "reject"},
        )

        self.assertEqual(agent.auto_confirm, "reject")

    def test_agent_adopts_server_conversation_id(self):
        class FakeClient:
            def send_user_message(self, content, conversation_id, metadata=None):
                self.sent_conversation_id = conversation_id
                return AiCustomerServiceClient._events_to_result(
                    "",
                    parse_sse_text(
                        "event: assistant_message\n"
                        "data: {\"type\":\"assistant_message\",\"conversation_id\":\"conv_server\",\"content\":\"ok\"}\n\n"
                    ),
                )

        agent = EcommerceGoAgent(base_url="http://unused")
        agent.client = FakeClient()

        message, state = agent.generate_next_message({"content": "hello"}, EcommerceGoAgentState())

        self.assertIsNone(agent.client.sent_conversation_id)
        self.assertEqual(getattr(message, "content"), "ok")
        self.assertEqual(state.conversation_id, "conv_server")

    def test_agent_reuses_initial_conversation_id_for_first_task_turn(self):
        class FakeClient:
            def send_user_message(self, content, conversation_id, metadata=None):
                self.sent_conversation_id = conversation_id
                return AiCustomerServiceClient._events_to_result(
                    conversation_id,
                    parse_sse_text(
                        "event: assistant_message\n"
                        "data: {\"type\":\"assistant_message\",\"conversation_id\":\"conv_shared\",\"content\":\"ok\"}\n\n"
                    ),
                )

        agent = EcommerceGoAgent(base_url="http://unused", initial_conversation_id="conv_shared")
        agent.client = FakeClient()

        message, state = agent.generate_next_message({"content": "hello"}, agent.get_init_state())

        self.assertEqual(agent.client.sent_conversation_id, "conv_shared")
        self.assertEqual(getattr(message, "content"), "ok")
        self.assertEqual(state.conversation_id, "conv_shared")

    def test_agent_can_force_new_conversation_for_task(self):
        class FakeClient:
            def send_user_message(self, content, conversation_id, metadata=None):
                self.sent_conversation_id = conversation_id
                return AiCustomerServiceClient._events_to_result(
                    "",
                    parse_sse_text(
                        "event: assistant_message\n"
                        "data: {\"type\":\"assistant_message\",\"conversation_id\":\"conv_new\",\"content\":\"ok\"}\n\n"
                    ),
                )

        task = SimpleNamespace(id="task_needs_new_conversation")
        agent = EcommerceGoAgent(
            base_url="http://unused",
            initial_conversation_id="conv_shared",
            new_conversation_task_ids={"task_needs_new_conversation"},
            task=task,
        )
        agent.client = FakeClient()

        message, state = agent.generate_next_message({"content": "hello"}, agent.get_init_state())

        self.assertIsNone(agent.client.sent_conversation_id)
        self.assertEqual(getattr(message, "content"), "ok")
        self.assertEqual(state.conversation_id, "conv_new")

    def test_agent_can_force_new_conversation_from_task_metadata(self):
        task = SimpleNamespace(
            id="task_needs_new_conversation",
            initial_state=SimpleNamespace(
                initialization_data=SimpleNamespace(
                    agent_data={"conversation_scope": "new"}
                )
            ),
        )
        agent = EcommerceGoAgent(
            base_url="http://unused",
            initial_conversation_id="conv_shared",
            task=task,
        )

        state = agent.get_init_state()

        self.assertEqual(state.conversation_id, "")

    def test_client_refreshes_tokens_and_retries_expired_request(self):
        captured = {"bodies": [], "access_tokens": [], "cookies": []}

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                captured["bodies"].append(json.loads(self.rfile.read(length).decode("utf-8")))
                captured["access_tokens"].append(self.headers.get("Access-Token"))
                captured["cookies"].append(self.headers.get("Cookie"))
                if len(captured["bodies"]) == 1:
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(
                        b'{'
                        b'"code":10004,'
                        b'"msg":"\\u4ee4\\u724c\\u7eed\\u671f\\u6210\\u529f",'
                        b'"data":{"access_token":"access-new","refresh_token":"refresh-new"}'
                        b'}'
                    )
                    return
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()
                self.wfile.write(
                    b'event: assistant_message\n'
                    b'data: {"type":"assistant_message","conversation_id":"conv_1","content":"ok"}\n\n'
                )

            def log_message(self, format, *args):
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = AiCustomerServiceClient(
                base_url=f"http://127.0.0.1:{server.server_port}",
                auth_token="access-old",
                refresh_token="refresh-old",
            )
            result = client.send_user_message("hello", "conv-1")
        finally:
            server.shutdown()
            thread.join(timeout=5)
            server.server_close()

        self.assertEqual(result.assistant_text, "ok")
        self.assertEqual(client.auth_token, "access-new")
        self.assertEqual(client.refresh_token, "refresh-new")
        self.assertEqual(captured["access_tokens"], ["access-old", "access-new"])
        self.assertIn("Refresh-Token=refresh-new", captured["cookies"][1])

    def test_client_merges_task_metadata_into_user_message_payload(self):
        captured = {}

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                captured["body"] = json.loads(self.rfile.read(length).decode("utf-8"))
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()
                self.wfile.write(
                    b'event: assistant_message\n'
                    b'data: {"type":"assistant_message","conversation_id":"conv_1","content":"ok"}\n\n'
                )

            def log_message(self, format, *args):
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = AiCustomerServiceClient(base_url=f"http://127.0.0.1:{server.server_port}")
            client.send_user_message(
                "hello",
                "conv-1",
                metadata={
                    "scenario_id": "bench_timeout_before_cart_add",
                    "failure_injection": {"tool": "cart_add", "point": "before_execute"},
                },
            )
        finally:
            server.shutdown()
            thread.join(timeout=5)
            server.server_close()

        self.assertEqual(captured["body"]["metadata"]["source"], "tau2")
        self.assertEqual(captured["body"]["metadata"]["scenario_id"], "bench_timeout_before_cart_add")
        self.assertEqual(captured["body"]["metadata"]["failure_injection"]["point"], "before_execute")

    def test_agent_attaches_task_metadata_and_sse_trace_to_assistant_message(self):
        class FakeClient:
            def send_user_message(self, content, conversation_id, metadata=None):
                self.metadata = metadata
                return AiCustomerServiceClient._events_to_result(
                    "conv_1",
                    parse_sse_text(
                        "event: tool_result\n"
                        "data: {\"type\":\"tool_result\",\"tool\":\"order_get\",\"status\":\"success\"}\n\n"
                        "event: assistant_message\n"
                        "data: {\"type\":\"assistant_message\",\"conversation_id\":\"conv_1\",\"content\":\"done\"}\n\n"
                    ),
                )

        task = SimpleNamespace(
            id="bench_timeout_before_cart_add",
            initial_state=SimpleNamespace(
                initialization_data=SimpleNamespace(
                    agent_data={
                        "suite": "benchmark",
                        "category": "tool_reliability",
                        "conversation_scope": "shared",
                        "failure_injection": {"tool": "cart_add", "point": "before_execute"},
                    }
                )
            ),
        )
        agent = EcommerceGoAgent(base_url="http://unused", task=task)
        agent.client = FakeClient()

        message, state = agent.generate_next_message({"content": "hello"}, EcommerceGoAgentState())

        self.assertEqual(agent.client.metadata["scenario_id"], "bench_timeout_before_cart_add")
        self.assertEqual(agent.client.metadata["failure_injection"]["point"], "before_execute")
        raw_data = getattr(message, "raw_data", None)
        self.assertIsNotNone(raw_data)
        self.assertEqual(raw_data["conversation_id"], "conv_1")
        self.assertEqual(raw_data["tau2_ecommerce_events"][0]["tool"], "order_get")
        self.assertEqual(state.last_events[0]["tool"], "order_get")


if __name__ == "__main__":
    unittest.main()
