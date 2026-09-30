"""Replay durable results through Langfuse v4 SDK/OTLP, with stable observation/score IDs."""
from __future__ import annotations

import argparse
from contextvars import ContextVar
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import time

from .contract import content_hash

_identity = ContextVar("ecommerce_export_identity", default="")


def identities(trial_id):
    return content_hash(["trace", trial_id])[:32], content_hash(["span", trial_id])[:16]


def export_experiment(output, tasks, manifest, results):
    from langfuse import Langfuse, LangfuseOtelSpanAttributes as A, propagate_attributes
    from opentelemetry import trace
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.id_generator import RandomIdGenerator

    class StableIDs(RandomIdGenerator):
        def generate_span_id(self):
            value = _identity.get()
            return int(identities(value)[1], 16) if value else super().generate_span_id()

    output = Path(output)
    journal_path = output / "langfuse-upload.json"
    journal = json.loads(journal_path.read_text()) if journal_path.exists() else {"completed": [], "pending": [], "errors": {}}
    # Never pick up a production project key by accident.
    keys = {name: os.getenv("AI_EVAL_LANGFUSE_" + name, "") for name in ("HOST", "PUBLIC_KEY", "SECRET_KEY")}
    if not all(keys.values()):
        raise RuntimeError("AI_EVAL_LANGFUSE_HOST/PUBLIC_KEY/SECRET_KEY are required; local results retained")
    provider = TracerProvider(id_generator=StableIDs())
    client = Langfuse(base_url=keys["HOST"], public_key=keys["PUBLIC_KEY"], secret_key=keys["SECRET_KEY"],
                      environment="evaluation", tracer_provider=provider, timeout=10)
    def save():
        temporary = journal_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(journal, indent=2), encoding="utf-8")
        temporary.replace(journal_path)
    try:
        dataset_name = "ecommerce-simulation/" + manifest["dataset_hash"][:16]
        dataset = client.create_dataset(name=dataset_name, metadata={"content_hash": manifest["dataset_hash"]})
        items = {}
        for task in tasks:
            item_id = content_hash([dataset_name, task["id"]])[:32]
            item = client.create_dataset_item(dataset_name=dataset_name, id=item_id,
                    input={k: task[k] for k in ("goal", "fixture", "turns") if k in task},
                    expected_output=task.get("expect", {}), metadata={"task_id": task["id"], "content_hash": content_hash(task)})
            items[task["id"]] = item.id
        for result in results:
            trial = result["trial_id"]
            if trial in journal["completed"]:
                continue
            if trial not in journal["pending"]:
                journal["pending"].append(trial)
            save()
            token = _identity.set(trial)
            trace_id, span_id = identities(trial)
            try:
                with propagate_attributes(session_id=result.get("conversation_id", trial), metadata={"trial_id": trial}, environment="evaluation"):
                    with client.start_as_current_observation(name="ecommerce.task-trial", trace_context={"trace_id": trace_id},
                          input={"task_id": result["task_id"]}, output=result.get("turns", []),
                          metadata={**manifest, "request_trace_ids": result.get("trace_ids", []), "trial_id": trial,
                                    "efficiency": result.get("efficiency", {}), "evaluation_error": result.get("evaluation_error")}) as span:
                        trace.get_current_span().set_attributes({
                            A.EXPERIMENT_ID: manifest["experiment_id"], A.EXPERIMENT_NAME: "ecommerce-" + manifest["experiment_id"],
                            A.EXPERIMENT_DATASET_ID: dataset.id, A.EXPERIMENT_ITEM_ID: items[result["task_id"]],
                            A.EXPERIMENT_ITEM_ROOT_OBSERVATION_ID: span.id,
                            A.EXPERIMENT_METADATA: json.dumps(manifest),
                            A.EXPERIMENT_ITEM_METADATA: json.dumps({"trial_index": result.get("trial_index"), "seed": result.get("seed")}),
                        })
                        if span.id != span_id:
                            raise RuntimeError("SDK did not preserve deterministic observation identity")
                client.flush()
                # Flush is best effort in the SDK. Verify ingestion before acknowledging.
                found = False
                for attempt in range(4):
                    response = client.api.observations.get_many(filter=json.dumps([
                        {"type": "string", "column": "traceId", "operator": "=", "value": trace_id}]), limit=100)
                    if any(item.id == span_id for item in response.data):
                        found = True
                        break
                    time.sleep(0.5 * (attempt + 1))
                if not found:
                    raise RuntimeError("observation ingestion not acknowledged")
                for name, score in result.get("scores", {}).items():
                    value = score.get("value")
                    # Missing scores remain categorical evidence, never a numeric zero/pass.
                    client.api.scores.create(id=content_hash([trial, name, score.get("evaluator")])[:32],
                        trace_id=trace_id, observation_id=span_id, name=name if value is not None else name + ".status",
                        value=value if value is not None else score.get("status", "unscorable"),
                        data_type="NUMERIC" if value is not None else "CATEGORICAL", environment="evaluation",
                        comment=score.get("reason", ""), metadata={"evidence": score.get("evidence", []), "evaluator": score.get("evaluator")})
                journal["completed"].append(trial)
                journal["pending"].remove(trial)
                journal["errors"].pop(trial, None)
            except Exception as exc:
                journal["errors"][trial] = type(exc).__name__  # provider bodies may contain credentials
            finally:
                _identity.reset(token)
                save()
    finally:
        client.shutdown()
        provider.shutdown()
    if journal["pending"]:
        raise RuntimeError(f"{len(journal['pending'])} trials await Langfuse upload; retry from local journal")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    manifest = json.loads((args.output / "manifest.json").read_text(encoding="utf-8"))
    tasks = json.loads((args.output / "tasks.json").read_text(encoding="utf-8"))
    results = [json.loads(line) for line in (args.output / "trials.jsonl").read_text(encoding="utf-8").splitlines()]
    export_experiment(args.output, tasks, manifest, results)


if __name__ == "__main__":
    main()
