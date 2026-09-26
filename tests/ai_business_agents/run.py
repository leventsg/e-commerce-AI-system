"""Run AI business-agent internal tests while keeping sources under tests/."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(__file__).resolve().parents[2]
targets = {
    "eino": "services/aiagent/internal/eino",
    "memory": "services/aiagent/internal/memory",
    "confirmation": "services/aiagent/internal/confirmation",
}
mapping = {
    str(root / target / source.name): str(source)
    for folder, target in targets.items()
    for source in (root / "tests/ai_business_agents" / folder).glob("*_test.go")
}
with tempfile.TemporaryDirectory(prefix="ai-business-agents-") as temp:
    overlay = Path(temp) / "overlay.json"
    overlay.write_text(json.dumps({"Replace": mapping}), encoding="utf-8")
    result = subprocess.run([
        "go", "test", "-count=1", "-tags=ai_business_agents",
        f"-overlay={overlay}", *sys.argv[1:],
        *("./" + path for path in targets.values()),
    ], cwd=root)
    sys.exit(result.returncode)
