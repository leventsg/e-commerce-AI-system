import pathlib
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "eval/tau2-ecommerce"))
from tau2_ecommerce.experiment import revision


class RevisionTest(unittest.TestCase):
    def test_git_output_uses_utf8_on_windows(self):
        with patch("tau2_ecommerce.experiment.subprocess.check_output", return_value="中文变更") as git:
            result = revision()
        self.assertEqual(result["commit"], "中文变更")
        for call in git.call_args_list:
            self.assertEqual(call.kwargs.get("encoding"), "utf-8")
