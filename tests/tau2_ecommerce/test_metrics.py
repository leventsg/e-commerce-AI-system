import pathlib
import sys
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "eval" / "tau2-ecommerce"))

from tau2_ecommerce.metrics import TaskTrialResult, build_eval_summary, format_eval_summary


class MetricsTest(unittest.TestCase):
    def test_build_eval_summary_reports_trial_rate_and_pass_at_k(self):
        results = [
            TaskTrialResult(task_id="task_a", trial_index=1, reward=0.0),
            TaskTrialResult(task_id="task_a", trial_index=2, reward=1.0),
            TaskTrialResult(task_id="task_a", trial_index=3, reward=0.0),
            TaskTrialResult(task_id="task_b", trial_index=1, reward=1.0),
            TaskTrialResult(task_id="task_b", trial_index=2, reward=1.0),
            TaskTrialResult(task_id="task_b", trial_index=3, reward=1.0),
            TaskTrialResult(task_id="task_c", trial_index=1, reward=None),
            TaskTrialResult(task_id="task_c", trial_index=2, reward=0.0),
            TaskTrialResult(task_id="task_c", trial_index=3, reward=0.0),
        ]

        summary = build_eval_summary(results, requested_pass_ks=(1, 3))

        self.assertEqual(summary.task_count, 3)
        self.assertEqual(summary.trial_count, 9)
        self.assertEqual(summary.passed_trial_count, 4)
        self.assertEqual(summary.pass_at_k[1].passed, 1)
        self.assertEqual(summary.pass_at_k[1].total, 3)
        self.assertAlmostEqual(summary.pass_at_k[1].rate, 1 / 3)
        self.assertEqual(summary.pass_at_k[3].passed, 2)
        self.assertEqual(summary.pass_at_k[3].total, 3)
        self.assertAlmostEqual(summary.pass_at_k[3].rate, 2 / 3)

    def test_format_eval_summary_marks_unavailable_pass_k(self):
        results = [
            TaskTrialResult(task_id="task_a", trial_index=1, reward=1.0),
            TaskTrialResult(task_id="task_b", trial_index=1, reward=0.0),
        ]

        text = format_eval_summary(results, requested_pass_ks=(1, 3))

        self.assertIn("Summary:", text)
        self.assertIn("any_of_1: 1/2 = 50.00%", text)
        self.assertIn("any_of_3: N/A (--num-trials >= 3 required)", text)


if __name__ == "__main__":
    unittest.main()
