"""Local Langfuse provisioning. Uses only Python's standard library and Docker CLI."""
import argparse
import base64
import json
import os
from pathlib import Path
import secrets
import subprocess
import urllib.request


ROOT = Path(__file__).resolve().parent


def read_env(path):
    return dict(line.split("=", 1) for line in path.read_text(encoding="utf-8").splitlines()
                if line and not line.startswith("#") and "=" in line)


def initialize(path):
    values = {name: secrets.token_hex(32) for name in (
        "POSTGRES_PASSWORD", "CLICKHOUSE_PASSWORD", "REDIS_PASSWORD", "MINIO_ROOT_PASSWORD",
        "SALT", "ENCRYPTION_KEY", "NEXTAUTH_SECRET", "LANGFUSE_ADMIN_PASSWORD")}
    values.update(MINIO_ROOT_USER="langfuse", LANGFUSE_ADMIN_EMAIL="admin@localhost.local")
    for project in ("DEV", "EVAL"):
        values[f"LANGFUSE_{project}_PUBLIC_KEY"] = "pk-lf-" + secrets.token_hex(16)
        values[f"LANGFUSE_{project}_SECRET_KEY"] = "sk-lf-" + secrets.token_hex(32)
    # Exclusive creation is deliberate: regenerating credentials breaks existing volumes.
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as file:
        file.write("# Local credentials. Do not commit or share.\n")
        file.writelines(f"{name}={value}\n" for name, value in values.items())


def compose(root, *args):
    # Environment values from this local file win over a possibly unrelated shell setup.
    env = {**os.environ, **read_env(root / ".env")}
    subprocess.run(["docker", "compose", "--env-file", str(root / ".env"),
                    "-f", str(root / "compose.yaml"), *args], cwd=root, env=env, check=True)


def check_projects(values):
    for project in ("DEV", "EVAL"):
        credentials = f"{values[f'LANGFUSE_{project}_PUBLIC_KEY']}:{values[f'LANGFUSE_{project}_SECRET_KEY']}"
        request = urllib.request.Request("http://localhost:3001/api/public/projects", headers={
            "Authorization": "Basic " + base64.b64encode(credentials.encode()).decode()})
        with urllib.request.urlopen(request, timeout=15) as response:
            data = json.load(response)
        expected = "ecommerce-" + project.lower()
        if not any(item.get("id") == expected for item in data.get("data", [])):
            raise RuntimeError(f"Project verification failed: {expected}")


def start(root=ROOT):
    values = read_env(root / ".env")
    compose(root, "config", "--quiet")
    compose(root, "up", "-d", "--wait", "--wait-timeout", "300")
    # Headless initialization creates one project per process start. Reuse the same
    # organization/user and start web once with the second project, then restore dev.
    override = root / ".bootstrap-eval.json"
    content = {"services": {"langfuse-web": {"environment": {
        "LANGFUSE_INIT_PROJECT_ID": "ecommerce-eval",
        "LANGFUSE_INIT_PROJECT_NAME": "Simulation Evaluation",
        "LANGFUSE_INIT_PROJECT_PUBLIC_KEY": "${LANGFUSE_EVAL_PUBLIC_KEY}",
        "LANGFUSE_INIT_PROJECT_SECRET_KEY": "${LANGFUSE_EVAL_SECRET_KEY}",
    }}}}
    # The override references env variables and contains no actual credentials.
    override.write_text(json.dumps(content), encoding="utf-8")
    try:
        compose(root, "-f", str(override), "up", "-d", "--wait", "--wait-timeout", "300", "langfuse-web")
    finally:
        override.unlink(missing_ok=True)
        compose(root, "up", "-d", "--wait", "--wait-timeout", "300", "langfuse-web")
    check_projects(values)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("init", "start", "check", "status", "stop"))
    args = parser.parse_args()
    if args.action == "init":
        initialize(ROOT / ".env")
        print("Created construct/langfuse/.env. Credentials are only in that local file.")
    elif args.action == "start":
        start()
        print("Both projects verified. Open http://localhost:3001")
    elif args.action == "check":
        compose(ROOT, "config", "--quiet")
        check_projects(read_env(ROOT / ".env"))
        print("Both project API keys verified.")
    elif args.action == "status":
        compose(ROOT, "ps")
    elif args.action == "stop":
        compose(ROOT, "stop")  # Persist all data volumes.


if __name__ == "__main__":
    main()
