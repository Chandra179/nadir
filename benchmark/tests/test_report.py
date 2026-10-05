import copy
import hashlib
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

from benchmark.report import save, validate_report


ROOT = Path(__file__).resolve().parents[2]
CONTRACT = json.loads((ROOT / "test/run-report-contract.json").read_text())


def example(tool="locust", status="completed"):
    return {"schema_version": 1, "tool": tool, "runs": [{
        "run_id": "run", "run_type": "retrieval", "status": status,
        "started_at": None, "ended_at": None, "git_revision": None,
        "inputs": {}, "provenance": {}, "summary": {}, "results": None,
        "errors": [], "artifacts": [],
    }]}


class ReportTests(unittest.TestCase):
    def test_shared_contract_and_atomic_save(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "report.json"
            for tool in CONTRACT["tools"]:
                for status in CONTRACT["statuses"]:
                    report = example(tool, status)
                    validate_report(report)
                    self.assertEqual(report["schema_version"], CONTRACT["schema_version"])
                    self.assertLessEqual(set(CONTRACT["required_envelope_fields"]), report.keys())
                    self.assertLessEqual(set(CONTRACT["required_run_fields"]), report["runs"][0].keys())
                    save(SimpleNamespace(benchmark_report_path=path, benchmark_report=report))
                    self.assertEqual(json.loads(path.read_text()), report)
            self.assertEqual(list(Path(directory).iterdir()), [path])

    def test_invalid_record_does_not_replace_existing_report(self):
        invalid = []
        for field in CONTRACT["required_run_fields"]:
            report = example()
            del report["runs"][0][field]
            invalid.append(report)
        for field, value in [("status", "passed"), ("inputs", []), ("summary", []),
                             ("provenance", []), ("results", []), ("errors", [1]),
                             ("artifacts", None), ("started_at", 1)]:
            report = example()
            report["runs"][0][field] = value
            invalid.append(report)
        duplicated = example()
        duplicated["runs"].append(copy.deepcopy(duplicated["runs"][0]))
        invalid.append(duplicated)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "report.json"
            path.write_text("unchanged")
            for report in invalid:
                with self.assertRaises(ValueError):
                    save(SimpleNamespace(benchmark_report_path=path, benchmark_report=report))
                self.assertEqual(path.read_text(), "unchanged")

    def test_catalog_contains_only_indexed_generated_results(self):
        manifest = json.loads((ROOT / "test/evaluation/reports/manifest.json").read_text())
        originals = [item["original_path"] for item in manifest["artifacts"]]
        self.assertEqual(len(originals), len(set(originals)))
        retained = [item for item in manifest["artifacts"] if item["path"]]
        reports = [item for item in retained if item["artifact_type"] == "evaluation-report"]
        actual = set((ROOT / "test/evaluation/reports").rglob("*.json"))
        actual.remove(ROOT / "test/evaluation/reports/manifest.json")
        self.assertEqual(actual, {ROOT / item["path"] for item in reports})
        self.assertEqual(len(reports), manifest["summary"]["retained_reports"])
        self.assertEqual(sum(item["bytes"] for item in retained), manifest["summary"]["retained_bytes"])
        fixture_hashes = {hashlib.sha256(path.read_bytes()).hexdigest()
                          for path in (ROOT / "test/evaluation").rglob("*.json")
                          if "reports" not in path.relative_to(ROOT).parts}
        for item in retained:
            with self.subTest(path=item["path"]):
                path = ROOT / item["path"]
                data = path.read_bytes()
                self.assertEqual(hashlib.sha256(data).hexdigest(), item["sha256"])
                self.assertEqual(len(data), item["bytes"])
                if item not in reports:
                    continue
                self.assertRegex(item["path"], r"^test/evaluation/reports/\d{4}-\d{2}-\d{2}/[a-z0-9/-]+\.json$")
                report = json.loads(data)
                validate_report(report)
                self.assertEqual(report["tool"], "evaluator")
                self.assertEqual(len(report["runs"]), 1)
                run = report["runs"][0]
                if "normalization" in run:
                    self.assertEqual(run["normalization"]["original_sha256"], item["original_sha256"])
                    self.assertIsNone(run["started_at"])
                    self.assertIsNone(run["ended_at"])
                    self.assertIsNone(run["git_revision"])
                else:
                    self.assertEqual(run["provenance"]["producer"]["entrypoint"], "cmd/evaluator")
                self.assertEqual(run["results"]["report_schema_version"], 2)
                self.assertIn(run["results"]["provenance"]["golden_sha256"], fixture_hashes)
                self.assertEqual(run["provenance"]["measurement"], run["results"]["provenance"])


if __name__ == "__main__":
    unittest.main()
