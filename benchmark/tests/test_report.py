import copy
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

from benchmark.report import save, validate_report


ROOT = Path(__file__).resolve().parents[2]
CONTRACT = json.loads((ROOT / "eval/run-report-contract.json").read_text())


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


if __name__ == "__main__":
    unittest.main()
