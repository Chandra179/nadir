import copy
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

from benchmark.report import operation_deltas, save, validate_report


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


def metrics(*operations):
    return {"generated_at": "now", "operations": [
        {"operation": name, "outcome": outcome, "count": count,
         "duration_ms_sum": total, "duration_ms_max": peak}
        for name, outcome, count, total, peak in operations]}


class OperationDeltaTests(unittest.TestCase):
    def test_reports_work_done_between_snapshots(self):
        before = metrics(("retrieval", "success", 2, 200.0, 120.0), ("chat", "success", 1, 5000.0, 5000.0))
        after = metrics(("retrieval", "success", 10, 1000.0, 130.0), ("chat", "success", 1, 5000.0, 5000.0),
                        ("ollama.generate.load", "success", 3, 6000.0, 4000.0))
        rows = {row["operation"]: row for row in operation_deltas(before, after)}
        self.assertEqual(set(rows), {"retrieval", "ollama.generate.load"})
        self.assertEqual(rows["retrieval"], {"operation": "retrieval", "outcome": "success", "count": 8,
                                             "duration_ms_sum": 800.0, "duration_ms_avg": 100.0,
                                             "lifetime_duration_ms_max": 130.0})
        self.assertEqual(rows["ollama.generate.load"]["count"], 3)
        self.assertEqual(rows["ollama.generate.load"]["duration_ms_avg"], 2000.0)

    def test_unavailable_or_reset_snapshots_yield_no_misleading_rows(self):
        good = metrics(("retrieval", "success", 5, 500.0, 100.0))
        self.assertEqual(operation_deltas({"unavailable": "refused"}, good), [])
        self.assertEqual(operation_deltas(good, {"unavailable": "refused"}), [])
        self.assertEqual(operation_deltas(None, good), [])
        restarted = metrics(("retrieval", "success", 1, 90.0, 90.0))
        self.assertEqual(operation_deltas(good, restarted), [])


if __name__ == "__main__":
    unittest.main()
