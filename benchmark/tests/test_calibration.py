"""Synthetic compatibility checks; reviewer data exists only in temporary files."""

import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from scripts import calibrate_evaluation_judge as calibration


class CalibrationTests(unittest.TestCase):
    def test_export_and_score_legacy_and_enveloped_evaluator_results(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            golden = root / "questions.json"
            golden.write_bytes(calibration.json_bytes({"queries": [{
                "id": "q", "query": "question", "type": "factoid",
                "faithfulness_label": "fully_supported", "expected_answer": "answer",
                "required_claims": ["answer"],
            }]}))
            native = {"report_schema_version": 2, "runs": 3,
                      "provenance": {"golden_sha256": hashlib.sha256(golden.read_bytes()).hexdigest()},
                      "generation": {"answer_model": "answer-model", "judge_model": "judge-model", "per_query": [{
                          "id": "q", "query": "question", "type": "factoid", "answer": "answer",
                          "admitted_context": "source evidence", "answer_status": "success", "judge_status": "success",
                          **{metric: 0.8 for metric in calibration.METRICS},
                      }]}}
            forms = [native, {"schema_version": 1, "tool": "evaluator", "runs": [{"results": native}]}]
            for index, report in enumerate(forms):
                with self.subTest(enveloped=bool(index)):
                    path = root / f"source-{index}.json"
                    path.write_bytes(calibration.json_bytes(report))
                    output = root / f"review-{index}"
                    manifest = calibration.export(path, golden, output, count=1)
                    self.assertEqual(manifest["source_report_sha256"], hashlib.sha256(path.read_bytes()).hexdigest())
                    reviews = [output / "reviewer-a.json", output / "reviewer-b.json"]
                    self.assertEqual(reviews[0].read_bytes(), reviews[1].read_bytes())
                    packet = calibration.read_json(reviews[0])
                    self.assertIsNone(packet["cases"][0]["human_scores"]["faithfulness"])
                    with self.assertRaisesRegex(ValueError, "reviewer IDs"):
                        calibration.score(output / "manifest.json", reviews, min_cases=1)
                    for reviewer_index, review in enumerate(reviews):
                        packet = calibration.read_json(review)
                        packet["reviewer"] = {"id": f"synthetic-test-{reviewer_index}", "human": True,
                                              "independent": True, "verification_ref": "synthetic unit test",
                                              "reviewed_at": "2026-10-05T00:00:00Z"}
                        packet["cases"][0]["human_scores"] = {metric: 0.8 for metric in calibration.METRICS}
                        packet["cases"][0]["rationale"] = "Synthetic fixture for serializer compatibility."
                        review.write_bytes(calibration.json_bytes(packet))
                    result = calibration.score(output / "manifest.json", reviews, min_cases=1)
                    self.assertEqual(result["status"], "reviewed_within_declared_thresholds")
                    self.assertFalse(result["release_gate"])
                    with self.assertRaisesRegex(ValueError, "already exist"):
                        calibration.export(path, golden, output, count=1)
                    modified = copy.deepcopy(report)
                    modified["new_metadata"] = True
                    path.write_bytes(calibration.json_bytes(modified))
                    with self.assertRaisesRegex(ValueError, "source report changed"):
                        calibration.score(output / "manifest.json", reviews, min_cases=1)

    def test_rejects_other_tools_missing_results_and_multiple_runs(self):
        for source in [{"schema_version": 1, "tool": "locust", "runs": [{"results": {}}]},
                       {"schema_version": 1, "tool": "evaluator", "runs": 3},
                       {"schema_version": 1, "tool": "evaluator", "runs": [{"results": None}]},
                       {"schema_version": 1, "tool": "evaluator", "runs": [{"results": {}}] * 2}]:
            with self.assertRaises(ValueError):
                calibration.evaluator_payload(source)


if __name__ == "__main__":
    unittest.main()
