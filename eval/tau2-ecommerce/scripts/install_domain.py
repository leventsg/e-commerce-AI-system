"""Install the ecommerce tau2 adapter into a local tau2-bench checkout."""

from __future__ import annotations

import argparse
import shutil
from pathlib import Path

DOMAIN_NAME = "ecommerce"
AGENT_NAME = "ecommerce_go_agent"


DATA_MODEL_SOURCE = '''"""Data model for the ecommerce evaluation domain."""

from typing import Any

from pydantic import ConfigDict, Field

from tau2.domains.ecommerce.utils import ECOMMERCE_DB_PATH
from tau2.environment.db import DB


class EcommerceDB(DB):
    """Seed snapshot used to describe the ecommerce sandbox state."""

    model_config = ConfigDict(extra="allow")

    notes: str = ""
    users: list[dict[str, Any]] = Field(default_factory=list)
    products: list[dict[str, Any]] = Field(default_factory=list)
    coupons: list[dict[str, Any]] = Field(default_factory=list)
    orders: list[dict[str, Any]] = Field(default_factory=list)
    cart_items: list[dict[str, Any]] = Field(default_factory=list)
    addresses: list[dict[str, Any]] = Field(default_factory=list)
    suite: str = ""
    category: str = ""
    conversation_scope: str = ""
    fixtures: dict[str, Any] = Field(default_factory=dict)
    expected_state: dict[str, Any] = Field(default_factory=dict)
    expected_actions: list[dict[str, Any]] = Field(default_factory=list)
    forbidden_actions: list[dict[str, Any]] = Field(default_factory=list)
    evaluation_dimensions: list[str] = Field(default_factory=list)
    failure_injection: dict[str, Any] = Field(default_factory=dict)
    depends_on: list[str] = Field(default_factory=list)

    def get_statistics(self) -> dict[str, Any]:
        return {
            "num_users": len(self.users),
            "num_products": len(self.products),
            "num_coupons": len(self.coupons),
            "num_orders": len(self.orders),
            "num_cart_items": len(self.cart_items),
            "num_addresses": len(self.addresses),
            "suite": self.suite,
            "category": self.category,
            "conversation_scope": self.conversation_scope,
            "num_expected_actions": len(self.expected_actions),
            "num_forbidden_actions": len(self.forbidden_actions),
            "has_failure_injection": bool(self.failure_injection),
            "num_dependencies": len(self.depends_on),
        }


def get_db() -> EcommerceDB:
    return EcommerceDB.load(ECOMMERCE_DB_PATH)
'''


TOOLS_SOURCE = '''"""Tools for the ecommerce evaluation domain.

The system under test is the Go AI customer-service API. These tau2 tools are
kept intentionally small: they expose seed-state inspection for tau2 metadata,
while business behavior remains outside tau2 and is exercised through the
agent adapter.
"""

import json
from typing import Any

from tau2.domains.ecommerce.data_model import EcommerceDB
from tau2.environment.toolkit import ToolKitBase, ToolType, is_tool


class EcommerceTools(ToolKitBase):
    """Minimal read-only toolkit for the ecommerce domain."""

    db: EcommerceDB

    def __init__(self, db: EcommerceDB) -> None:
        super().__init__(db)

    @is_tool(ToolType.READ)
    def list_seed_products(self) -> str:
        """
        List product fixtures available in the ecommerce evaluation seed.

        Returns:
            A compact JSON string containing product ids, names, prices, and stock.
        """
        return json.dumps(self.db.products, ensure_ascii=False, sort_keys=True)

    @is_tool(ToolType.READ)
    def list_seed_orders(self) -> str:
        """
        List order fixtures available in the ecommerce evaluation seed.

        Returns:
            A compact JSON string containing seed order ids and statuses.
        """
        return json.dumps(self.db.orders, ensure_ascii=False, sort_keys=True)

    @is_tool(ToolType.GENERIC, mutates_state=False)
    def transfer_to_human_agents(self, summary: str) -> str:
        """
        Transfer the conversation to a human agent when the issue cannot be solved.

        Args:
            summary: A concise summary of the customer issue.

        Returns:
            A transfer acknowledgement.
        """
        return "Transfer successful"
'''


UTILS_SOURCE = '''"""Paths for the ecommerce evaluation domain."""

from tau2.utils.utils import DATA_DIR

ECOMMERCE_DATA_DIR = DATA_DIR / "tau2" / "domains" / "ecommerce"
ECOMMERCE_DB_PATH = ECOMMERCE_DATA_DIR / "db.json"
ECOMMERCE_POLICY_PATH = ECOMMERCE_DATA_DIR / "policy.md"
ECOMMERCE_TASK_SET_PATH = ECOMMERCE_DATA_DIR / "tasks.json"
'''


ENVIRONMENT_SOURCE = '''"""Environment loader for the ecommerce evaluation domain."""

import json
from pathlib import Path
from typing import Optional

from tau2.data_model.tasks import Task
from tau2.domains.ecommerce.data_model import EcommerceDB
from tau2.domains.ecommerce.tools import EcommerceTools
from tau2.domains.ecommerce.utils import (
    ECOMMERCE_DB_PATH,
    ECOMMERCE_POLICY_PATH,
    ECOMMERCE_TASK_SET_PATH,
)
from tau2.environment.environment import Environment


def load_json_utf8(path: Path):
    with open(path, "r", encoding="utf-8") as fp:
        return json.load(fp)


def get_environment(db: Optional[EcommerceDB] = None, solo_mode: bool = False) -> Environment:
    if solo_mode:
        raise ValueError("Ecommerce domain does not support solo mode")
    if db is None:
        db = EcommerceDB.load(ECOMMERCE_DB_PATH)
    tools = EcommerceTools(db)
    user_tools = EcommerceTools(db.model_copy(deep=True))
    with open(ECOMMERCE_POLICY_PATH, "r", encoding="utf-8") as fp:
        policy = fp.read()
    return Environment(domain_name="ecommerce", policy=policy, tools=tools, user_tools=user_tools)


def get_tasks(task_split_name: Optional[str] = "base") -> list[Task]:
    tasks = [Task.model_validate(task) for task in load_json_utf8(ECOMMERCE_TASK_SET_PATH)]
    if task_split_name is None:
        return tasks
    task_splits = get_tasks_split()
    if task_split_name not in task_splits:
        raise ValueError(
            f"Invalid task split name: {task_split_name}. Valid splits are: {task_splits.keys()}"
        )
    return [task for task in tasks if task.id in task_splits[task_split_name]]


def get_tasks_split() -> dict[str, list[str]]:
    split_file = (
        Path(ECOMMERCE_TASK_SET_PATH).parent
        / f"split_{Path(ECOMMERCE_TASK_SET_PATH).stem}.json"
    )
    return load_json_utf8(split_file)
'''


AGENT_WRAPPER_SOURCE = '''"""tau2 registry wrapper for the ecommerce Go customer-service adapter."""

from __future__ import annotations

import os
import sys
from pathlib import Path


def _add_adapter_path() -> None:
    configured = os.getenv("TAU2_ECOMMERCE_ADAPTER_PATH")
    candidates = []
    if configured:
        candidates.append(Path(configured))
    here = Path(__file__).resolve()
    candidates.extend(parent / "eval" / "tau2-ecommerce" for parent in here.parents)
    for candidate in candidates:
        if (candidate / "tau2_ecommerce").exists():
            path = str(candidate)
            if path not in sys.path:
                sys.path.insert(0, path)
            return
    raise RuntimeError(
        "Cannot find eval/tau2-ecommerce. Set TAU2_ECOMMERCE_ADAPTER_PATH to the adapter directory."
    )


_add_adapter_path()

from tau2_ecommerce.agent import EcommerceGoAgent, create_ecommerce_go_agent

__all__ = ["EcommerceGoAgent", "create_ecommerce_go_agent"]
'''


EXAMPLE_SOURCE = '''#!/usr/bin/env python3
"""Run or smoke-test the ecommerce Go customer-service adapter.

Before running, start the Go API service and set:
    AI_CS_BASE_URL=http://localhost:8007
    AI_CS_AUTH_TOKEN=<test-user-token>
    AI_CS_REFRESH_TOKEN=<test-refresh-token>

Then from tau2-bench:
    py -3 examples/agents/ecommerce_go_agent.py --direct-smoke

Full tau2 evaluation also needs an LLM key because tau2's user simulator
and NL evaluator are LLM-based. Set OPENAI_API_KEY or DEEPSEEK_API_KEY:
    py -3 examples/agents/ecommerce_go_agent.py
    py -3 examples/agents/ecommerce_go_agent.py --task-id all --split all
    py -3 examples/agents/ecommerce_go_agent.py --task-id all --split all --num-trials 3
    py -3 examples/agents/ecommerce_go_agent.py --task-id bench_pronoun_cancel_latest_unpaid_order
    py -3 examples/agents/ecommerce_go_agent.py --task-id reg_order_cancel_reject_confirmation --auto-confirm reject
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path


def _add_adapter_path() -> None:
    configured = os.getenv("TAU2_ECOMMERCE_ADAPTER_PATH")
    candidates = []
    if configured:
        candidates.append(Path(configured))
    here = Path(__file__).resolve()
    candidates.extend(parent / "eval" / "tau2-ecommerce" for parent in here.parents)
    for candidate in candidates:
        if (candidate / "tau2_ecommerce").exists():
            path = str(candidate)
            if path not in sys.path:
                sys.path.insert(0, path)
            return
    raise SystemExit(
        "Cannot find eval/tau2-ecommerce. Set TAU2_ECOMMERCE_ADAPTER_PATH "
        "to the adapter directory before running this script."
    )


_add_adapter_path()


REJECT_CONFIRMATION_TASK_IDS = {
    "reg_order_cancel_reject_confirmation",
    "bench_reject_confirmation_no_side_effect",
}


def resolve_auto_confirm(auto_confirm: str, task_id: str | None = None) -> str:
    if auto_confirm != "auto":
        return auto_confirm
    if task_id in REJECT_CONFIRMATION_TASK_IDS:
        return "reject"
    return "approve"


def print_event_diagnostics(state) -> None:
    print(f"events={len(state.last_events)} confirmations={len(state.confirmations)}")
    for idx, event in enumerate(state.last_events[:20], start=1):
        event_type = event.get("type")
        content = str(event.get("content") or "")
        if len(content) > 160:
            content = content[:160] + "..."
        keys = ",".join(sorted(event.keys()))
        print(f"event[{idx}] type={event_type!r} keys={keys} content={content!r}")


def run_direct_smoke(message: str, dump_events: bool, auto_confirm: str) -> int:
    from tau2_ecommerce.agent import EcommerceGoAgent

    agent = EcommerceGoAgent(auto_confirm=resolve_auto_confirm(auto_confirm))
    if dump_events:
        print(f"base_url={agent.client.base_url} path={agent.client.chat_path}")
    assistant_message, state = agent.generate_next_message(message)
    print(getattr(assistant_message, "content", str(assistant_message)))
    print(f"conversation_id={state.conversation_id}")
    if dump_events:
        print_event_diagnostics(state)
    else:
        print(f"events={len(state.last_events)} confirmations={len(state.confirmations)}")
    return 0


def resolve_eval_model() -> str:
    configured = os.getenv("TAU2_EVAL_LLM")
    if configured:
        return configured
    if os.getenv("DEEPSEEK_API_KEY"):
        return "deepseek/deepseek-chat"
    if os.getenv("OPENAI_API_KEY") or os.getenv("OPENAI_ADMIN_KEY"):
        return "gpt-4.1-2025-04-14"
    raise SystemExit(
        "Missing LLM credentials. Full tau2 evaluation uses an LLM user simulator "
        "and NL assertions. Set OPENAI_API_KEY or DEEPSEEK_API_KEY, or run with "
        "--direct-smoke to only test the Go /douyin/ai/chat adapter path."
    )


def require_customer_service_credentials() -> None:
    missing = [
        name
        for name in ("AI_CS_AUTH_TOKEN", "AI_CS_REFRESH_TOKEN")
        if not os.getenv(name)
    ]
    if missing:
        raise SystemExit(
            "Missing customer-service credentials: "
            + ", ".join(missing)
            + ". Export them in this terminal before running full tau2 evaluation."
        )


def resolve_task_selection(task_id: str, split: str | None) -> list:
    from tau2.runner import get_tasks

    if task_id != "all":
        return get_tasks("ecommerce", task_ids=[task_id])
    if split:
        return get_tasks("ecommerce", task_split_name=split)
    return get_tasks("ecommerce", task_split_name="all")


def task_agent_metadata(task) -> dict:
    initial_state = getattr(task, "initial_state", None)
    initialization_data = getattr(initial_state, "initialization_data", None)
    agent_data = getattr(initialization_data, "agent_data", None)
    return agent_data if isinstance(agent_data, dict) else {}


def task_conversation_scope(task, new_conversation_task_ids: set[str]) -> str:
    metadata_scope = task_agent_metadata(task).get("conversation_scope")
    if metadata_scope in {"new", "shared"}:
        return metadata_scope
    return "new" if task.id in new_conversation_task_ids else "shared"


def task_dependency_ids(task) -> list[str]:
    dependencies = task_agent_metadata(task).get("depends_on", [])
    if dependencies is None:
        return []
    if not isinstance(dependencies, list):
        raise ValueError(f"Task {task.id} has invalid depends_on metadata: {dependencies!r}")
    return [str(task_id) for task_id in dependencies]


def expand_task_dependencies(selected_tasks: list) -> list:
    from tau2.runner import get_tasks

    all_tasks = {task.id: task for task in get_tasks("ecommerce", task_split_name=None)}
    expanded = []
    added = set()
    visiting = []

    def visit(task):
        if task.id in visiting:
            cycle = " -> ".join([*visiting, task.id])
            raise ValueError(f"Task dependency cycle detected: {cycle}")
        if task.id in added:
            return
        visiting.append(task.id)
        for dependency_id in task_dependency_ids(task):
            if dependency_id not in all_tasks:
                raise ValueError(f"Task {task.id} depends on unknown task {dependency_id}")
            visit(all_tasks[dependency_id])
        visiting.pop()
        expanded.append(task)
        added.add(task.id)

    for task in selected_tasks:
        visit(task)
    return expanded


def format_task_progress(
    unit_index: int,
    total_units: int,
    task_id: str,
    trial_index: int,
    num_trials: int,
    seed: int,
    auto_confirm: str,
    conversation_scope: str,
    task_role: str,
) -> str:
    return (
        f"[task {unit_index}/{total_units}] {task_id} "
        f"trial {trial_index}/{num_trials} seed={seed} "
        f"auto_confirm={auto_confirm} conversation={conversation_scope} role={task_role}"
    )


def run_tau2_eval(
    task_id: str,
    split: str | None,
    auto_confirm: str,
    new_conversation_task_ids: list[str],
    max_steps: int,
    seed: int,
    num_trials: int,
) -> int:
    require_customer_service_credentials()
    eval_model = resolve_eval_model()
    from tau2.data_model.simulation import TextRunConfig
    import tau2.evaluator.evaluator_nl_assertions as nl_assertions
    from tau2.runner import run_single_task
    from tau2_ecommerce.metrics import TaskTrialResult, build_eval_summary, format_eval_summary

    nl_assertions.DEFAULT_LLM_NL_ASSERTIONS = eval_model
    selected_tasks = resolve_task_selection(task_id, split)
    selected_task_ids = {task.id for task in selected_tasks}
    tasks = expand_task_dependencies(selected_tasks)
    trial_results = []
    total_units = len(tasks) * num_trials
    completed_units = 0
    dependency_failed = False
    new_conversation_task_id_set = set(new_conversation_task_ids)
    for task_index, task in enumerate(tasks, start=1):
        task_auto_confirm = resolve_auto_confirm(auto_confirm, task.id)
        for trial_index in range(1, num_trials + 1):
            trial_seed = seed + trial_index - 1
            unit_index = (task_index - 1) * num_trials + trial_index
            conversation_scope = task_conversation_scope(task, new_conversation_task_id_set)
            task_role = "target" if task.id in selected_task_ids else "dependency"
            print(
                format_task_progress(
                    unit_index,
                    total_units,
                    task.id,
                    trial_index,
                    num_trials,
                    trial_seed,
                    task_auto_confirm,
                    conversation_scope,
                    task_role,
                ),
                flush=True,
            )
            config = TextRunConfig(
                domain="ecommerce",
                agent="ecommerce_go_agent",
                user="user_simulator",
                llm_args_agent={
                    "auto_confirm": task_auto_confirm,
                    "new_conversation_task_ids": list(new_conversation_task_id_set),
                },
                llm_user=eval_model,
                num_trials=1,
                max_concurrency=1,
                max_steps=max_steps,
            )
            result = run_single_task(config, task, seed=trial_seed)
            reward = result.reward_info.reward if result.reward_info else None
            if task_role == "target":
                trial_results.append(
                    TaskTrialResult(
                        task_id=result.task_id,
                        trial_index=trial_index,
                        reward=reward,
                    )
                )
            elif reward != 1.0:
                dependency_failed = True
            print(f"Task: {result.task_id}")
            print(f"Trial: {trial_index}/{num_trials}")
            print(f"Seed: {trial_seed}")
            print(f"Reward: {reward if reward is not None else 'N/A'}")
            completed_units += 1
            passed_units = sum(1 for item in trial_results if item.reward == 1.0)
            failed_units = len(trial_results) - passed_units
            pending_units = total_units - completed_units
            print(
                f"[progress] completed={completed_units}/{total_units} "
                f"target_passed={passed_units} target_failed={failed_units} "
                f"dependency_failed={dependency_failed} pending={pending_units}",
                flush=True,
            )

    print(format_eval_summary(trial_results, requested_pass_ks=(1, 3)))
    summary = build_eval_summary(trial_results, requested_pass_ks=(num_trials,))
    pass_at_requested_trials = summary.pass_at_k.get(num_trials)
    if dependency_failed or pass_at_requested_trials is None:
        return 1
    return 0 if pass_at_requested_trials.passed == pass_at_requested_trials.total else 1


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--direct-smoke",
        action="store_true",
        help="Call the Go customer-service adapter directly without tau2's LLM user simulator.",
    )
    parser.add_argument(
        "--message",
        default="Please check my latest order status.",
        help="Message used by --direct-smoke.",
    )
    parser.add_argument(
        "--dump-events",
        action="store_true",
        help="Print parsed SSE event types and keys in --direct-smoke mode.",
    )
    parser.add_argument(
        "--task-id",
        default="reg_read_latest_order_status",
        help="Task id for full tau2 evaluation, or 'all' to run a split.",
    )
    parser.add_argument(
        "--split",
        default=None,
        help="Task split for --task-id all, such as smoke, regression, benchmark, reliability, transaction, rag_tool, safety, or all.",
    )
    parser.add_argument(
        "--auto-confirm",
        choices=["auto", "approve", "reject", "none"],
        default="auto",
        help="How the adapter responds to confirmation_required events. auto rejects reject-confirmation tasks and approves the rest.",
    )
    parser.add_argument(
        "--new-conversation-task-id",
        action="append",
        default=[],
        help="Task id that should start a fresh Go AI conversation instead of reusing the shared conversation.",
    )
    parser.add_argument("--max-steps", type=int, default=12, help="Maximum tau2 simulator steps per task.")
    parser.add_argument("--seed", type=int, default=42, help="Seed passed to tau2 run_single_task.")
    parser.add_argument(
        "--num-trials",
        type=int,
        default=1,
        help="Trials to run per task. Summary reports pass^1, and pass^3 when this is at least 3.",
    )
    args = parser.parse_args()
    if args.num_trials < 1:
        parser.error("--num-trials must be at least 1")
    try:
        raise SystemExit(
            run_direct_smoke(args.message, args.dump_events, args.auto_confirm)
            if args.direct_smoke
            else run_tau2_eval(
                args.task_id,
                args.split,
                args.auto_confirm,
                args.new_conversation_task_id,
                args.max_steps,
                args.seed,
                args.num_trials,
            )
        )
    except RuntimeError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
'''

IMPORT_BLOCK = """
from tau2.domains.ecommerce.environment import (
    get_environment as ecommerce_domain_get_environment,
)
from tau2.domains.ecommerce.environment import get_tasks as ecommerce_domain_get_tasks
from tau2.domains.ecommerce.environment import (
    get_tasks_split as ecommerce_domain_get_tasks_split,
)
from tau2.agent.ecommerce_go_agent import create_ecommerce_go_agent
"""

REGISTRY_BLOCK = """
    registry.register_agent_factory(create_ecommerce_go_agent, "ecommerce_go_agent")
    registry.register_domain(ecommerce_domain_get_environment, "ecommerce")
    registry.register_tasks(
        ecommerce_domain_get_tasks,
        "ecommerce",
        get_task_splits=ecommerce_domain_get_tasks_split,
    )
"""


def copytree(src: Path, dst: Path) -> None:
    if dst.exists():
        shutil.rmtree(dst)
    shutil.copytree(src, dst)


def write_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8", newline="\n")


def patch_registry(registry_path: Path) -> None:
    text = registry_path.read_text(encoding="utf-8")
    if "ecommerce_domain_get_environment" not in text:
        marker = "from tau2.environment.environment import Environment\n"
        if marker in text:
            text = text.replace(marker, marker + IMPORT_BLOCK, 1)
        elif "\ntry:\n" in text:
            text = text.replace("\ntry:\n", IMPORT_BLOCK + "\ntry:\n", 1)
        else:
            raise RuntimeError(f"Cannot find import insertion point in {registry_path}")

    text = text.replace(REGISTRY_BLOCK + "\n", "")
    text = text.replace(REGISTRY_BLOCK, "")
    if 'registry.register_domain(ecommerce_domain_get_environment, "ecommerce")' not in text:
        final_marker = '    logger.debug(\n        f"Default components registered successfully.'
        final_index = text.find(final_marker)
        if final_index >= 0:
            text = text[:final_index] + REGISTRY_BLOCK + "\n" + text[final_index:]
        elif "\nexcept Exception as e:\n" in text:
            text = text.replace("\nexcept Exception as e:\n", "\n" + REGISTRY_BLOCK + "except Exception as e:\n", 1)
        else:
            raise RuntimeError(f"Cannot find registry insertion point in {registry_path}")

    registry_path.write_text(text, encoding="utf-8", newline="\n")


def install(tau2_root: Path) -> None:
    here = Path(__file__).resolve().parents[1]
    tau2_root = tau2_root.resolve()
    if not tau2_root.exists():
        raise SystemExit(f"tau2 root does not exist: {tau2_root}")
    if not (tau2_root / "src" / "tau2" / "registry.py").exists():
        raise SystemExit(f"not a tau2-bench checkout: {tau2_root}")

    src_domain = here / "domains" / DOMAIN_NAME
    dst_data = tau2_root / "data" / "tau2" / "domains" / DOMAIN_NAME
    copytree(src_domain, dst_data)

    domain_dir = tau2_root / "src" / "tau2" / "domains" / DOMAIN_NAME
    write_file(domain_dir / "__init__.py", "")
    write_file(domain_dir / "data_model.py", DATA_MODEL_SOURCE)
    write_file(domain_dir / "tools.py", TOOLS_SOURCE)
    write_file(domain_dir / "utils.py", UTILS_SOURCE)
    write_file(domain_dir / "environment.py", ENVIRONMENT_SOURCE)

    agent_module = tau2_root / "src" / "tau2" / "agent" / f"{AGENT_NAME}.py"
    write_file(agent_module, AGENT_WRAPPER_SOURCE)

    agents_dir = tau2_root / "examples" / "agents"
    write_file(agents_dir / f"{AGENT_NAME}.py", EXAMPLE_SOURCE)

    patch_registry(tau2_root / "src" / "tau2" / "registry.py")

    print(f"Installed ecommerce domain seed to {dst_data}")
    print(f"Installed ecommerce domain source to {domain_dir}")
    print(f"Installed ecommerce agent wrapper to {agent_module}")
    print(f"Installed ecommerce example to {agents_dir / f'{AGENT_NAME}.py'}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tau2-root", default="tau2-bench", help="Path to local tau2-bench checkout")
    args = parser.parse_args()

    install(Path(args.tau2_root))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
