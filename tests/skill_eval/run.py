"""Skill 专项评测与 Self-Evolving 功能测试入口。

通过 Go overlay 把 tests/skill_eval/evolution 下的内部测试注入到
services/aiagent/internal/evolution 包运行，验证数据集、候选校验、A/B 执行、
加载证据与结论判定；不改动生产包内文件。
"""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(__file__).resolve().parents[2]
targets = {
    "evolution": "services/aiagent/internal/evolution",
    "eino": "services/aiagent/internal/eino",
    "observability": "services/aiagent/internal/observability",
}

mapping = {}
# skill_eval 内部测试 -> evolution 包
for source in (Path(__file__).parent / "evolution").glob("*_test.go"):
    mapping[str(root / "services/aiagent/internal/evolution" / source.name)] = str(source)
# 复用 ai_business_agents 与 ai_observability 的 overlay 测试（覆盖 skill 加载/脱敏）
for source in (root / "tests/ai_business_agents/eino").glob("*_test.go"):
    mapping[str(root / "services/aiagent/internal/eino" / source.name)] = str(source)
for source in (root / "tests/ai_observability/observability").glob("*_test.go"):
    mapping[str(root / "services/aiagent/internal/observability" / source.name)] = str(source)

with tempfile.TemporaryDirectory(prefix="skill-eval-") as temp:
    overlay = Path(temp) / "overlay.json"
    overlay.write_text(json.dumps({"Replace": mapping}), encoding="utf-8")
    packages = ["./" + p for p in targets.values()]
    cmd = ["go", "test", "-vet=off", "-count=1", "-tags=ai_skill_evolution,ai_business_agents,ai_observability",
           f"-overlay={overlay}", *sys.argv[1:], *packages]
    sys.exit(subprocess.run(cmd, cwd=root).returncode)
