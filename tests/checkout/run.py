"""Run internal-package tests using an overlay; all test sources stay in tests/."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(__file__).resolve().parents[2]
targets = {
    "logic": "services/checkout/internal/logic",
    "ai": "services/aiagent/internal/tools/business/checkout",
    "api": "apis/checkout/internal/logic",
    "executor": "services/aiagent/internal/tools",
}
mapping = {
    str(root / target / source.name): str(source)
    for folder, target in targets.items()
    for source in (root / "tests/checkout" / folder).glob("*_test.go")
}
with tempfile.TemporaryDirectory(prefix="checkout-tests-") as temp:
    overlay = Path(temp) / "overlay.json"
    overlay.write_text(json.dumps({"Replace": mapping}), encoding="utf-8")
    result = subprocess.run([
        "go", "test", "-count=1", "-tags=checkout_internal_tests",
        f"-overlay={overlay}", *sys.argv[1:], "./tests/checkout",
        *("./" + path for path in targets.values()),
    ], cwd=root)
    sys.exit(result.returncode)
