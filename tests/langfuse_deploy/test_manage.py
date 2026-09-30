import importlib.util
import io
import pathlib
import tempfile
import unittest
from unittest.mock import patch


ROOT = pathlib.Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("langfuse_manage", ROOT / "construct/langfuse/manage.py")
manage = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(manage)


class LocalDeploymentTest(unittest.TestCase):
    def test_init_creates_distinct_secrets_and_never_overwrites(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / ".env"
            manage.initialize(path)
            original = path.read_bytes()
            values = manage.read_env(path)
            self.assertEqual(len(values["ENCRYPTION_KEY"]), 64)
            self.assertNotEqual(values["LANGFUSE_DEV_SECRET_KEY"], values["LANGFUSE_EVAL_SECRET_KEY"])
            self.assertTrue(values["LANGFUSE_DEV_PUBLIC_KEY"].startswith("pk-lf-"))
            with self.assertRaises(FileExistsError):
                manage.initialize(path)
            self.assertEqual(original, path.read_bytes())

    def test_bootstrap_provisions_two_projects_and_restores_normal_compose(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            manage.initialize(root / ".env")
            seen = []
            def run(command, **kwargs):
                seen.append((command, kwargs))
                if ".bootstrap-eval.json" in str(command):
                    self.assertIn("ecommerce-eval", (root / ".bootstrap-eval.json").read_text())
            with patch.object(manage.subprocess, "run", side_effect=run), patch.object(manage, "check_projects") as check:
                manage.start(root)
            self.assertEqual(len(seen), 4)  # validate, first project, second, restore
            self.assertFalse((root / ".bootstrap-eval.json").exists())
            check.assert_called_once()

    def test_failed_second_project_restores_normal_compose_and_cleans_secret_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            manage.initialize(root / ".env")
            def run(command, **kwargs):
                if ".bootstrap-eval.json" in str(command):
                    raise RuntimeError("simulated startup failure")
            with patch.object(manage.subprocess, "run", side_effect=run):
                with self.assertRaisesRegex(RuntimeError, "startup failure"):
                    manage.start(root)
            self.assertFalse((root / ".bootstrap-eval.json").exists())

    def test_example_has_no_populated_passwords(self):
        values = manage.read_env(ROOT / "construct/langfuse/.env.example")
        for name, value in values.items():
            if any(word in name for word in ("PASSWORD", "SECRET", "SALT", "ENCRYPTION", "PUBLIC_KEY")):
                self.assertEqual(value, "", name)

    def test_project_check_rejects_keys_for_wrong_project(self):
        values = {f"LANGFUSE_{project}_{kind}_KEY": "test" for project in ("DEV", "EVAL") for kind in ("PUBLIC", "SECRET")}
        with patch.object(manage.urllib.request, "urlopen", return_value=io.BytesIO(b'{"data":[{"id":"unrelated"}]}')):
            with self.assertRaisesRegex(RuntimeError, "ecommerce-dev"):
                manage.check_projects(values)


if __name__ == "__main__":
    unittest.main()
