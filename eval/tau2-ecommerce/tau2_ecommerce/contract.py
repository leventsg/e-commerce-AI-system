"""Versioned, scorer-only task contract; no expectations cross the runner boundary."""
from __future__ import annotations

import hashlib
import json
from typing import Any

AGENTS = {"product_discovery_agent", "shopping_decision_agent", "order_fulfillment_agent", "after_sales_agent", "general_agent"}
HIGH_RISK = {"order_create", "order_cancel", "cart_delete"}


class DatasetError(ValueError):
    pass


def content_hash(value: Any) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()


def validate_task(task: dict, schemas: dict) -> None:
    """Validate names against the running Go binary, never a stale Python tool list."""
    prefix = str(task.get("id", "<missing-id>"))
    def fail(message):
        raise DatasetError(f"{prefix}: {message}")
    if not task.get("id") or not task.get("goal") or not task.get("turns"):
        fail("id, goal and nonempty turns are required")
    if task.get("confirmation", "wait") not in {"approve", "reject", "wait"}:
        fail("confirmation must be approve, reject or wait")
    expect = task.get("expect", {})
    def validate_expected(value, spec, label):
        if isinstance(value, dict) and "$ref" in value:
            parts = value["$ref"].split(".", 1)
            if len(parts) != 2 or parts[0] not in schemas:
                fail(f"invalid dynamic reference at {label}")
            return
        typ = spec.get("type")
        valid = {"string": isinstance(value, str), "integer": isinstance(value, int) and not isinstance(value, bool),
                 "number": isinstance(value, (float, int)) and not isinstance(value, bool), "boolean": isinstance(value, bool),
                 "object": isinstance(value, dict), "array": isinstance(value, list)}
        if typ and not valid.get(typ, False):
            fail(f"invalid expected type at {label}")
        if typ == "object":
            for key, item in value.items():
                if key not in spec.get("properties", {}):
                    fail(f"unknown parameter {label}.{key}")
                validate_expected(item, spec["properties"][key], label + "." + key)
        if typ == "array":
            for item in value:
                validate_expected(item, spec.get("items", {}), label + "[]")
    for agent in expect.get("allowed_agents", []):
        if agent not in AGENTS:
            fail(f"unknown agent {agent}")
    tool_names = set(expect.get("required_tools", []) + expect.get("optional_tools", []) + expect.get("forbidden_tools", []) + expect.get("successful_tools", []))
    for group in expect.get("any_of_tools", []):
        if not group:
            fail("any_of_tools groups must not be empty")
        tool_names.update(group)
    for rule in expect.get("arguments", []):
        name = rule.get("tool")
        tool_names.add(name)
        schema = schemas.get(name, {})
        fields = schema.get("properties", schema)
        for parameter in rule.get("equals", {}):
            if parameter not in fields:
                fail(f"unknown parameter {name}.{parameter}")
            validate_expected(rule["equals"][parameter], fields[parameter], f"{name}.{parameter}")
    for dependency in expect.get("dependencies", []):
        tool_names.update([dependency["before"], dependency["after"]])
    for failure in task.get("failures", []):
        tool_names.add(failure.get("tool"))
        if failure.get("kind") not in {"timeout", "business_failure", "outcome_unknown", "audit_failure"} or not isinstance(failure.get("call"), int) or failure["call"] < 1:
            fail("invalid failure injection")
    for assertion in expect.get("result_assertions", []):
        tool_names.add(assertion["ref"].split(".", 1)[0])
    unknown = tool_names - schemas.keys()
    if unknown:
        fail(f"unknown tools {sorted(str(x) for x in unknown)}")
    for turn in task["turns"]:
        if not isinstance(turn.get("content"), str) or not turn["content"].strip():
            fail("turn.content must be nonempty text")
        if turn.get("confirmation", task.get("confirmation", "wait")) not in {"approve", "reject", "wait"}:
            fail("invalid turn confirmation policy")


def runner_init(task: dict, trial_id: str, seed: int) -> dict:
    # This allowlist is the boundary between environment setup and grading.
    return {"op": "init", "trial_id": trial_id, "seed": seed,
            "fixture": task.get("fixture", {}), "failures": task.get("failures", [])}
