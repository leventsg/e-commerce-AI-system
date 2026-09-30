"""Run isolated real-model/simulated-tool trials and persist evidence before upload."""
from __future__ import annotations

import argparse
import csv
import json
import os
from pathlib import Path
import subprocess
import time
from uuid import uuid4

from .contract import content_hash, runner_init, validate_task
from .judge import judge_trial
from .metrics import TaskTrialResult, format_eval_summary
from .scoring import DIMENSIONS, VERSION, efficiency, score_trial
from .simulation import SimulationClient


def revision():
    root = Path(__file__).resolve().parents[3]
    def git(*args):
        return subprocess.check_output(["git", *args], cwd=root, encoding="utf-8", errors="replace").strip()
    try:
        return {"commit": git("rev-parse", "HEAD"), "dirty": bool(git("status", "--porcelain")),
                "diff_hash": content_hash(git("diff", "--", "services/aiagent", "eval/tau2-ecommerce"))}
    except (OSError, subprocess.CalledProcessError):
        return {"commit": "unknown"}


def run_trial(task, *, trial_id, seed, command=None, timeout=120, semantic_judge=False, experiment_id=""):
    started = time.monotonic()
    turns, events, trajectory, trace_ids = [], [], [], []
    with SimulationClient(command, timeout_seconds=timeout) as client:
        validate_task(task, client.schemas())
        payload=runner_init(task,trial_id,seed)
        payload.update(experiment_id=experiment_id, root_trace_id=content_hash(["trace",trial_id])[:32])
        init = client.initialize(payload)
        conversation_id = init.get("conversation_id", "")
        for turn in task["turns"]:
            result = client.send_user_message(turn["content"], conversation_id)
            turns.append({"user": turn["content"], "assistant": result.assistant_text})
            events.extend(result.events)
            trajectory = result.trajectory
            if result.trace_id:
                trace_ids.append(result.trace_id)
            decision = turn.get("confirmation", task.get("confirmation", "wait"))
            # Each turn chooses its own policy; no implicit approval of newly generated confirmations.
            if decision in {"approve", "reject"}:
                for confirmation in result.confirmations:
                    resumed = client.confirm_action(conversation_id, confirmation["confirmation_id"], decision == "approve")
                    events.extend(resumed.events)
                    trajectory = resumed.trajectory
                    turns.append({"confirmation": decision, "assistant": resumed.assistant_text})
                    if resumed.trace_id:
                        trace_ids.append(resumed.trace_id)
        final = client.end()
        trajectory = final.get("trajectory", trajectory)
        state = final.get("state", {})
    scores = score_trial(task, trajectory, state, truncated=final.get("truncated", False))
    if semantic_judge and not final.get("truncated"):
        scores.update(judge_trial(task, trajectory, turns))
    business = scores["task_success"].value
    safe = scores["safety_compliance"].value
    qualified = None if business is None or safe is None else business == 1 and safe == 1
    return {"task_id": task["id"], "trial_id": trial_id, "seed": seed, "conversation_id": conversation_id,
            "trace_ids": list(dict.fromkeys(trace_ids)), "turns": turns, "events": events, "trajectory": trajectory,
            "state": state, "scores": {k: v.to_dict() for k, v in scores.items()}, "business_success": business,
            "safety_compliance": safe, "qualified": qualified, "score_coverage": sum(s.value is not None for s in scores.values()) / len(DIMENSIONS),
            "efficiency": dict(efficiency(trajectory), system_duration_ms=(time.monotonic() - started) * 1000)}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tasks", type=Path, default=Path(__file__).resolve().parents[1] / "domains/ecommerce/simulation_tasks.json")
    parser.add_argument("--output", type=Path, default=Path("eval-results"))
    parser.add_argument("--trials", type=int, default=3)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--task-id", action="append")
    parser.add_argument("--judge", action="store_true")
    parser.add_argument("--langfuse", action="store_true")
    parser.add_argument("--runner-command", help="JSON array, e.g. [\"./evalrunner.exe\"]")
    parser.add_argument("--preflight", action="store_true")
    args = parser.parse_args(argv)
    if args.trials < 1:
        parser.error("--trials must be positive")
    tasks = json.loads(args.tasks.read_text(encoding="utf-8"))
    if args.task_id:
        tasks = [t for t in tasks if t["id"] in args.task_id]
    if not tasks:
        parser.error("no tasks selected")
    command = json.loads(args.runner_command) if args.runner_command else None
    with SimulationClient(command) as client:
        schemas = client.schemas()
        for task in tasks:
            validate_task(task, schemas)
    if args.preflight:
        print(f"Validated {len(tasks)} tasks against {len(schemas)} real tool schemas")
        return 0
    experiment_id = uuid4().hex
    output = args.output / experiment_id
    output.mkdir(parents=True)
    prompt_root = Path(__file__).resolve().parents[3] / "services/aiagent/internal/prompts"
    prompt_hash = content_hash({str(p.relative_to(prompt_root)): p.read_text(encoding="utf-8") for p in sorted(prompt_root.rglob("*")) if p.is_file() and p.suffix in {".md", ".txt", ".go"}})
    manifest = {"experiment_id": experiment_id, "dataset_hash": content_hash(tasks), "code": revision(), "prompt_hash": prompt_hash,
                "model": os.getenv("AI_EVAL_MODEL", ""), "judge_model": os.getenv("AI_EVAL_JUDGE_MODEL", ""),
                "user_simulator": "scripted-user-v1", "evaluator": VERSION, "trials": args.trials, "seed": args.seed}
    (output / "tasks.json").write_text(json.dumps(tasks,ensure_ascii=False,indent=2),encoding="utf-8")
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    results, rewards = [], []
    with (output / "trials.jsonl").open("w", encoding="utf-8") as log:
        for task in tasks:
            for index in range(args.trials):
                trial_id = content_hash([experiment_id, task["id"], index])[:32]
                try:
                    result = run_trial(task, trial_id=trial_id, seed=args.seed + index, command=command, semantic_judge=args.judge, experiment_id=experiment_id)
                except Exception as exc:
                    result = {"task_id": task["id"], "trial_id": trial_id, "qualified": None, "evaluation_error": f"{type(exc).__name__}: {exc}"}
                result["trial_index"] = index
                log.write(json.dumps(result, ensure_ascii=False) + "\n")
                log.flush()
                results.append(result)
                rewards.append(TaskTrialResult(task["id"], index, result.get("qualified")))
                print(f"{task['id']} trial {index + 1}: {result.get('qualified')} {result.get('evaluation_error', '')}", flush=True)
    summary = {"manifest": manifest, "trials": [{k: v for k, v in r.items() if k not in {"trajectory", "events", "turns", "state"}} for r in results],
               "rates": format_eval_summary(rewards)}
    (output / "summary.json").write_text(json.dumps(summary, indent=2, ensure_ascii=False), encoding="utf-8")
    with (output / "summary.csv").open("w", newline="", encoding="utf-8") as stream:
        writer = csv.DictWriter(stream, fieldnames=["task_id", "trial_index", "qualified", *DIMENSIONS, "score_coverage", "evaluation_error"])
        writer.writeheader()
        for r in results:
            writer.writerow({**{k: r.get(k) for k in ["task_id", "trial_index", "qualified", "score_coverage", "evaluation_error"]}, **{d: r.get("scores", {}).get(d, {}).get("value") for d in DIMENSIONS}})
    if args.langfuse:
        from .langfuse_export import export_experiment
        export_experiment(output, tasks, manifest, results)
    print(summary["rates"])
    print(output)
    return int(any("evaluation_error" in r for r in results))


if __name__ == "__main__":
    raise SystemExit(main())
