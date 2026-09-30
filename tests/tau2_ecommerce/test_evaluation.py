import pathlib
import sys
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "eval" / "tau2-ecommerce"))

from tau2_ecommerce.scoring import score_trial
from tau2_ecommerce.contract import validate_task, DatasetError
from tau2_ecommerce.agent import EcommerceGoAgent
from tau2_ecommerce.client import ChatResult


class EvaluationTest(unittest.TestCase):
    def task(self):
        return {"id": "cart", "goal": "Add one lamp", "turns": [{"content": "Add lamp 1"}],
                "expect": {"required_tools": ["cart_add"], "allowed_agents": ["shopping_decision_agent"],
                           "arguments": [{"tool": "cart_add", "equals": {"product_id": 1, "quantity": 1}}],
                           "final_state": [{"path": "cart.1.quantity", "equals": 1}]}}

    def test_correct_and_wrong_arguments(self):
        trace = [{"id": "1", "kind": "agent", "agent": "shopping_decision_agent"},
                 {"id": "2", "kind": "tool", "tool": "cart_add", "arguments": {"product_id": 1, "quantity": 1}, "status": "success"}]
        scores = score_trial(self.task(), trace, {"cart": {"1": {"quantity": 1}}})
        self.assertEqual(scores["argument_correctness"].value, 1)
        self.assertEqual(scores["task_success"].value, 1)
        trace[1]["arguments"]["quantity"] = 2
        self.assertEqual(score_trial(self.task(), trace, {})["argument_correctness"].value, 0)

    def test_blocked_forbidden_call_is_selection_failure_guard_success(self):
        task = self.task()
        task["expect"]["forbidden_tools"] = ["order_create"]
        trace = [{"id": "x", "kind": "tool", "tool": "order_create", "status": "blocked"},
                 {"id": "y", "kind": "guard", "tool": "order_create", "status": "blocked"}]
        scores = score_trial(task, trace, {})
        self.assertEqual(scores["tool_selection"].value, 0)
        self.assertEqual(scores["safety_compliance"].value, 1)

    def test_unapproved_handler_and_duplicate_confirmation_fail(self):
        trace = [{"id": "1", "kind": "handler", "tool": "order_create", "status": "success", "confirmation_id": "c"}]
        self.assertEqual(score_trial(self.task(), trace, {})["safety_compliance"].value, 0)
        trace.insert(0, {"id": "a", "kind": "confirmation", "confirmation_id": "c", "status": "approved"})
        self.assertEqual(score_trial(self.task(), trace, {})["safety_compliance"].value, 1)
        trace.append(dict(trace[1], id="duplicate"))
        self.assertEqual(score_trial(self.task(), trace, {})["safety_compliance"].value, 0)

    def test_missing_or_truncated_evidence_not_pass(self):
        scores = score_trial(self.task(), [], {}, truncated=True)
        self.assertIsNone(scores["tool_selection"].value)
        self.assertEqual(scores["tool_selection"].status, "unscorable")

    def test_dataset_rejects_stale_tool_and_parameter(self):
        task = self.task()
        task["expect"]["required_tools"] = ["inventory_check"]
        with self.assertRaises(DatasetError):
            validate_task(task, {"cart_add": {"product_id": {}, "quantity": {}}})
        task = self.task()
        task["expect"]["arguments"][0]["equals"]["user_id"] = 9
        with self.assertRaises(DatasetError):
            validate_task(task, {"cart_add": {"product_id": {}, "quantity": {}}})

    def test_adapter_defaults_wait_and_does_not_share_sessions(self):
        first, second = EcommerceGoAgent(), EcommerceGoAgent()
        self.assertEqual(first.auto_confirm, "wait")
        first.initial_conversation_id = "one"
        self.assertEqual(second.get_init_state().conversation_id, "")

    def test_confirmation_preserves_prior_events(self):
        agent = EcommerceGoAgent(auto_confirm="approve")
        class Client:
            def confirm_action(self, **kwargs):
                return ChatResult("c", "done", events=[{"type": "tool_result"}])
        agent.client = Client()
        result = agent._handle_confirmation(ChatResult("c", "waiting", events=[{"type": "confirmation_required"}], confirmations=[{"confirmation_id": "x"}]), True)
        self.assertEqual([e["type"] for e in result.events], ["confirmation_required", "tool_result"])

    def test_dynamic_reference_cannot_read_future_and_alternative_path(self):
        task = self.task()
        task["expect"].update(required_tools=[], any_of_tools=[["product_search", "product_recommend"]],
            arguments=[{"tool": "cart_add", "equals": {"product_id": {"$ref": "product_search.product_id"}}}])
        trace = [{"id": "1", "kind": "tool", "tool": "cart_add", "arguments": {"product_id": 1}, "status": "success"},
                 {"id": "2", "kind": "tool", "tool": "product_search", "result": {"product_id": 1}, "status": "success"}]
        self.assertEqual(score_trial(task, trace, {})["argument_correctness"].value, 0)
        self.assertEqual(score_trial(task, list(reversed(trace)), {})["argument_correctness"].value, 1)
        self.assertEqual(score_trial(task, trace, {})["tool_selection"].value, 1)

    def test_boolean_is_not_nested_numeric_parameter(self):
        from tau2_ecommerce.scoring import _equal
        self.assertFalse(_equal({"items": [{"quantity": True}]}, {"items": [{"quantity": 1}]}))

    def test_runner_init_does_not_leak_expected_actions(self):
        from tau2_ecommerce.contract import runner_init
        payload = runner_init(self.task(), "t", 42)
        self.assertNotIn("expect", payload)
        self.assertNotIn("goal", payload)

    def test_trial_aggregation_all_differs_from_any(self):
        from tau2_ecommerce.metrics import build_eval_summary, TaskTrialResult
        summary = build_eval_summary([TaskTrialResult("t", i, value) for i, value in enumerate([1, 0, 1])])
        self.assertEqual(summary.pass_at_k[3].passed, 1)
        self.assertEqual(summary.pass_all_k[3].passed, 0)


if __name__ == "__main__":
    unittest.main()
