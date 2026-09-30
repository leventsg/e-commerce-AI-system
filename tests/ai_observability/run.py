"""Overlay internal observability tests without moving tests into production packages."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(__file__).resolve().parents[2]
targets = {"observability": "services/aiagent/internal/observability", "eino": "services/aiagent/internal/eino"}
mapping = {str(root / target / source.name): str(source)
           for folder, target in targets.items()
           for source in (Path(__file__).parent / folder).glob("*_test.go")}
mapping.update({str(root / "services/aiagent/internal/eino" / source.name): str(source)
                for source in (root / "tests/ai_business_agents/eino").glob("*_test.go")})
with tempfile.TemporaryDirectory(prefix="ai-observability-") as temp:
    overlay = Path(temp) / "overlay.json"
    overlay.write_text(json.dumps({"Replace": mapping}), encoding="utf-8")
    sys.exit(subprocess.run(["go", "test", "-count=1", "-tags=ai_observability,ai_business_agents",
                            f"-overlay={overlay}", *sys.argv[1:],
                            *("./" + p for p in targets.values())], cwd=root).returncode)
