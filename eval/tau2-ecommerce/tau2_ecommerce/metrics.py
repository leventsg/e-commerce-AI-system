"""Evaluation metrics for the ecommerce tau2 adapter."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Iterable


@dataclass(frozen=True)
class TaskTrialResult:
    task_id: str
    trial_index: int
    reward: float | None


@dataclass(frozen=True)
class PassAtK:
    passed: int
    total: int

    @property
    def rate(self) -> float:
        if self.total == 0:
            return 0.0
        return self.passed / self.total


@dataclass(frozen=True)
class EvalSummary:
    task_count: int
    trial_count: int
    passed_trial_count: int
    pass_at_k: dict[int, PassAtK | None]
    pass_all_k: dict[int, PassAtK | None]

    @property
    def trial_pass_rate(self) -> float:
        if self.trial_count == 0:
            return 0.0
        return self.passed_trial_count / self.trial_count


def reward_passed(reward: float | None) -> bool:
    return reward == 1 or reward == 1.0


def build_eval_summary(
    results: Iterable[TaskTrialResult],
    requested_pass_ks: Iterable[int] = (1, 3),
) -> EvalSummary:
    result_list = list(results)
    by_task: dict[str, list[TaskTrialResult]] = {}
    for result in result_list:
        by_task.setdefault(result.task_id, []).append(result)

    pass_at_k: dict[int, PassAtK | None] = {}
    pass_all_k: dict[int, PassAtK | None] = {}
    for k in requested_pass_ks:
        if any(len(task_results) < k for task_results in by_task.values()):
            pass_at_k[k] = None
            pass_all_k[k] = None
            continue

        passed = 0
        all_passed = 0
        for task_results in by_task.values():
            ordered = sorted(task_results, key=lambda item: item.trial_index)
            if any(reward_passed(item.reward) for item in ordered[:k]):
                passed += 1
            if all(reward_passed(item.reward) for item in ordered[:k]):
                all_passed += 1
        pass_at_k[k] = PassAtK(passed=passed, total=len(by_task))
        pass_all_k[k] = PassAtK(passed=all_passed, total=len(by_task))

    return EvalSummary(
        task_count=len(by_task),
        trial_count=len(result_list),
        passed_trial_count=sum(1 for result in result_list if reward_passed(result.reward)),
        pass_at_k=pass_at_k,
        pass_all_k=pass_all_k,
    )


def _format_rate(count: int, total: int) -> str:
    rate = 0.0 if total == 0 else count / total
    return f"{count}/{total} = {rate:.2%}"


def format_eval_summary(
    results: Iterable[TaskTrialResult],
    requested_pass_ks: Iterable[int] = (1, 3),
) -> str:
    summary = build_eval_summary(results, requested_pass_ks=requested_pass_ks)
    lines = [
        "Summary:",
        f"Tasks: {summary.task_count}",
        f"Trials: {summary.trial_count}",
        f"Trial pass rate: {_format_rate(summary.passed_trial_count, summary.trial_count)}",
    ]
    for k, value in summary.pass_at_k.items():
        if value is None:
            lines.append(f"any_of_{k}: N/A (--num-trials >= {k} required)")
            lines.append(f"all_of_{k}: N/A (--num-trials >= {k} required)")
        else:
            lines.append(f"any_of_{k}: {_format_rate(value.passed, value.total)}")
            all_value = summary.pass_all_k[k]
            lines.append(f"all_of_{k}: {_format_rate(all_value.passed, all_value.total)}")
    return "\n".join(lines)
