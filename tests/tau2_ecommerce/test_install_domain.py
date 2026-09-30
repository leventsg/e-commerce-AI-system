import json
import importlib.util
import pathlib
import py_compile
import re
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[2]
INSTALL_SCRIPT = ROOT / "eval" / "tau2-ecommerce" / "scripts" / "install_domain.py"
DOMAIN_DIR = ROOT / "eval" / "tau2-ecommerce" / "domains" / "ecommerce"
TASKS_PATH = DOMAIN_DIR / "tasks.json"
SPLITS_PATH = DOMAIN_DIR / "split_tasks.json"
CHINESE_RE = re.compile(r"[\u4e00-\u9fff]")


def load_install_module():
    spec = importlib.util.spec_from_file_location("install_domain", INSTALL_SCRIPT)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def load_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


class InstallDomainTest(unittest.TestCase):
    def test_install_creates_tau2_domain_agent_and_registry_entries(self):
        module = load_install_module()
        with tempfile.TemporaryDirectory() as tmp:
            tau2_root = pathlib.Path(tmp)
            registry = tau2_root / "src" / "tau2" / "registry.py"
            registry.parent.mkdir(parents=True)
            registry.write_text(
                "\n".join(
                    [
                        "from tau2.domains.mock.environment import get_environment as mock_domain_get_environment",
                        "try:",
                        "    registry = Registry()",
                        "    registry.register_domain(mock_domain_get_environment, \"mock\")",
                        "    logger.debug(\"done\")",
                        "except Exception as e:",
                        "    raise",
                    ]
                ),
                encoding="utf-8",
            )

            module.install(tau2_root)
            module.install(tau2_root)

            self.assertTrue((tau2_root / "data" / "tau2" / "domains" / "ecommerce" / "tasks.json").exists())
            self.assertTrue((tau2_root / "data" / "tau2" / "domains" / "ecommerce" / "split_tasks.json").exists())
            self.assertTrue((tau2_root / "src" / "tau2" / "domains" / "ecommerce" / "environment.py").exists())
            environment_text = (
                tau2_root / "src" / "tau2" / "domains" / "ecommerce" / "environment.py"
            ).read_text(encoding="utf-8")
            self.assertIn("def load_json_utf8", environment_text)
            self.assertIn('encoding="utf-8"', environment_text)
            data_model_text = (
                tau2_root / "src" / "tau2" / "domains" / "ecommerce" / "data_model.py"
            ).read_text(encoding="utf-8")
            self.assertIn("notes: str", data_model_text)
            self.assertIn("cart_items", data_model_text)
            self.assertIn("addresses", data_model_text)
            self.assertIn("suite: str", data_model_text)
            self.assertIn("expected_state: dict[str, Any]", data_model_text)
            self.assertIn("failure_injection: dict[str, Any]", data_model_text)
            self.assertIn("depends_on: list[str]", data_model_text)
            self.assertIn('extra="allow"', data_model_text)
            self.assertIn("user_tools = EcommerceTools", environment_text)
            self.assertIn("user_tools=user_tools", environment_text)
            example = tau2_root / "examples" / "agents" / "ecommerce_go_agent.py"
            self.assertTrue(example.exists())
            example_text = example.read_text(encoding="utf-8")
            self.assertIn("--direct-smoke", example_text)
            self.assertIn("OPENAI_API_KEY", example_text)
            self.assertIn("AI_CS_REFRESH_TOKEN", example_text)
            self.assertIn("TAU2_ECOMMERCE_ADAPTER_PATH", example_text)
            self.assertIn("_add_adapter_path()", example_text)
            self.assertIn("--task-id", example_text)
            self.assertIn("--split", example_text)
            self.assertIn("task_split_name", example_text)
            self.assertIn("resolve_task_selection", example_text)
            self.assertIn("--auto-confirm", example_text)
            self.assertIn("llm_args_agent", example_text)
            self.assertIn("resolve_auto_confirm", example_text)
            self.assertIn("REJECT_CONFIRMATION_TASK_IDS", example_text)
            self.assertIn("--new-conversation-task-id", example_text)
            self.assertIn("new_conversation_task_ids", example_text)
            self.assertIn("format_task_progress", example_text)
            self.assertIn("expand_task_dependencies", example_text)
            self.assertIn("task_dependency_ids", example_text)
            self.assertIn("[task {unit_index}/{total_units}]", example_text)
            self.assertIn("conversation={conversation_scope}", example_text)
            self.assertIn("role={task_role}", example_text)
            self.assertIn("dependency_failed", example_text)
            self.assertIn('if task_role == "target"', example_text)
            self.assertIn("[progress] completed=", example_text)
            self.assertIn("default=\"auto\"", example_text)
            self.assertIn("--num-trials", example_text)
            self.assertIn("format_eval_summary", example_text)
            self.assertIn("any_of_1", example_text)
            self.assertIn("all_of_3", example_text)
            self.assertIn("bench_pronoun_cancel_latest_unpaid_order", example_text)
            py_compile.compile(str(example), doraise=True)

            patched = registry.read_text(encoding="utf-8")
            self.assertEqual(patched.count("registry.register_domain(ecommerce_domain_get_environment, \"ecommerce\")"), 1)
            self.assertIn("registry.register_agent_factory(create_ecommerce_go_agent, \"ecommerce_go_agent\")", patched)

    def test_ecommerce_dataset_has_goal_based_benchmark_suite(self):
        tasks = load_json(TASKS_PATH)
        task_ids = [task["id"] for task in tasks]

        self.assertGreaterEqual(len(tasks), 24)
        self.assertEqual(len(task_ids), len(set(task_ids)))
        self.assertIn("bench_pronoun_cancel_latest_unpaid_order", task_ids)
        self.assertIn("bench_timeout_after_order_create_unknown_status", task_ids)
        self.assertIn("bench_saga_inventory_reserved_order_failed", task_ids)
        self.assertIn("bench_cross_user_order_injection", task_ids)
        self.assertNotIn("ecommerce_cancel_order_confirmation_001", task_ids)

        scripted_phrases = [
            "if the assistant lists",
            "if the assistant shows",
            "pick one",
            "choose one product shown",
            "first ask",
            "then ask",
        ]
        vague_phrases = [
            "useful and affordable product",
            "practical low-cost product",
            "suitable product",
        ]
        user_id_assertions = 0
        categories = set()
        for task in tasks:
            metadata = task["initial_state"]["initialization_data"]["agent_data"]
            categories.add(metadata["category"])
            self.assertIn(metadata["suite"], {"regression", "benchmark"})
            self.assertIn(metadata["conversation_scope"], {"shared", "new"})
            self.assertTrue(metadata["expected_actions"], task["id"])
            self.assertIn("forbidden_actions", metadata)
            self.assertTrue(metadata["evaluation_dimensions"], task["id"])
            self.assertTrue(metadata.get("expected_state"), task["id"])
            criteria = task["evaluation_criteria"]
            self.assertTrue(criteria["actions"], task["id"])
            instructions = task["user_scenario"]["instructions"]["task_instructions"].lower()
            for phrase in scripted_phrases + vague_phrases:
                self.assertNotIn(phrase, instructions, task["id"])
            assertions = json.dumps(criteria.get("nl_assertions", []), ensure_ascii=False).lower()
            if "user_id" in assertions or "user id" in assertions:
                user_id_assertions += 1

        self.assertLessEqual(user_id_assertions, 4)
        for required_category in [
            "context_task_state",
            "multi_agent",
            "tool_reliability",
            "transaction_consistency",
            "rag_tool",
            "hitl_safety",
        ]:
            self.assertIn(required_category, categories)

    def test_failure_and_transaction_tasks_have_machine_readable_expectations(self):
        tasks = load_json(TASKS_PATH)
        by_id = {task["id"]: task for task in tasks}

        reliability_ids = [
            "bench_timeout_before_cart_add",
            "bench_timeout_after_order_create_unknown_status",
            "bench_idempotent_retry_checkout_prepare",
            "bench_non_retryable_coupon_business_error",
        ]
        for task_id in reliability_ids:
            metadata = by_id[task_id]["initial_state"]["initialization_data"]["agent_data"]
            self.assertTrue(metadata.get("failure_injection"), task_id)
            self.assertIn("failure_injection", metadata["evaluation_dimensions"])

        transaction_ids = [
            "bench_saga_inventory_reserved_order_failed",
            "bench_compensation_failure_handoff",
        ]
        for task_id in transaction_ids:
            metadata = by_id[task_id]["initial_state"]["initialization_data"]["agent_data"]
            self.assertIn("compensation", metadata["expected_state"], task_id)
            self.assertTrue(metadata["forbidden_actions"], task_id)

    def test_user_facing_task_text_is_chinese_but_machine_fields_stay_stable(self):
        tasks = load_json(TASKS_PATH)

        for task in tasks:
            instructions = task["user_scenario"]["instructions"]
            user_text_fields = [
                task["user_scenario"]["persona"],
                instructions["reason_for_call"],
                instructions["known_info"],
                instructions["unknown_info"],
                instructions["task_instructions"],
                *task["evaluation_criteria"]["nl_assertions"],
            ]
            for value in user_text_fields:
                self.assertRegex(value, CHINESE_RE, task["id"])

            metadata = task["initial_state"]["initialization_data"]["agent_data"]
            self.assertRegex(task["id"], r"^(reg|bench)_")
            self.assertIn(metadata["suite"], {"regression", "benchmark"})
            self.assertRegex(metadata["category"], r"^[a-z_]+$")
            self.assertIn(metadata["conversation_scope"], {"shared", "new"})

    def test_dependent_tasks_are_explicitly_ordered_by_previous_results(self):
        tasks = load_json(TASKS_PATH)
        by_id = {task["id"]: task for task in tasks}
        splits = load_json(SPLITS_PATH)

        pronoun = by_id["bench_pronoun_cancel_latest_unpaid_order"]
        metadata = pronoun["initial_state"]["initialization_data"]["agent_data"]
        self.assertEqual(metadata["depends_on"], ["reg_order_create_requires_confirmation"])
        self.assertLess(
            splits["all"].index("reg_order_create_requires_confirmation"),
            splits["all"].index("bench_pronoun_cancel_latest_unpaid_order"),
        )
        task_text = json.dumps(pronoun["user_scenario"], ensure_ascii=False)
        self.assertIn("刚才", task_text)
        self.assertNotIn("记得最近下过一笔台灯订单", task_text)

    def test_ecommerce_splits_cover_every_task(self):
        tasks = load_json(TASKS_PATH)
        splits = load_json(SPLITS_PATH)
        task_ids = {task["id"] for task in tasks}

        for split_name in [
            "smoke",
            "regression",
            "benchmark",
            "read",
            "write_low",
            "confirmation",
            "context",
            "multi_agent",
            "reliability",
            "transaction",
            "rag_tool",
            "safety",
            "all",
        ]:
            self.assertIn(split_name, splits)
            self.assertTrue(splits[split_name], split_name)

        self.assertEqual(set(splits["all"]), task_ids)
        self.assertIn("bench_confirm_after_parameter_change_invalidates_confirmation", splits["confirmation"])
        self.assertIn("bench_cross_user_order_injection", splits["safety"])
        self.assertTrue(set(splits["benchmark"]).issubset(task_ids))
        self.assertTrue(set(splits["regression"]).issubset(task_ids))
        for split_name, ids in splits.items():
            unknown = set(ids) - task_ids
            self.assertFalse(unknown, f"{split_name} contains unknown tasks: {sorted(unknown)}")


if __name__ == "__main__":
    unittest.main()
