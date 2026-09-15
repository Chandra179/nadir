#!/usr/bin/env python3
import importlib.util
import json
import pathlib
import sys
import tempfile
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("benchmark_load.py")
SPEC = importlib.util.spec_from_file_location("benchmark_load", MODULE_PATH)
benchmark_load = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules["benchmark_load"] = benchmark_load
SPEC.loader.exec_module(benchmark_load)


class LoadBenchmarkTest(unittest.TestCase):
    def test_percentile_uses_nearest_rank(self):
        self.assertEqual(benchmark_load.percentile([3, 1, 2, 4], 0.50), 2)
        self.assertEqual(benchmark_load.percentile([3, 1, 2, 4], 0.95), 4)
        self.assertIsNone(benchmark_load.percentile([], 0.99))

    def test_metric_delta_includes_admission_gauges(self):
        before = {
            "operations": [{"operation": "admission.retrieval", "outcome": "acquired", "count": 2, "duration_ms_sum": 5}],
            "gauges": {"admission.retrieval.active": 0},
        }
        after = {
            "operations": [{"operation": "admission.retrieval", "outcome": "acquired", "count": 5, "duration_ms_sum": 11}],
            "gauges": {"admission.retrieval.active": 1, "admission.retrieval.max_concurrent": 4},
        }
        delta = benchmark_load.metric_delta(before, after)
        self.assertEqual(delta["operations"][0]["count"], 3)
        self.assertEqual(delta["admission_gauges"]["admission.retrieval.active"], 1)

    def test_multipart_body_is_parseable_shape(self):
        content_type, body = benchmark_load.multipart_body("files", "sample.md", b"hello")
        boundary = content_type.split("boundary=", 1)[1].encode()
        self.assertIn(boundary, body)
        self.assertIn(b'filename="sample.md"', body)
        self.assertTrue(body.endswith(b"--\r\n"))

    def test_report_can_be_serialized(self):
        sample = benchmark_load.Sample(0, 200, True, 1.2, None)
        report = benchmark_load.summarize([sample], 0.0, {"operations": [], "admission_gauges": {}})
        self.assertEqual(json.loads(json.dumps(report))["successes"], 1)

    def test_json_output_path_is_writable(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "load.json"
            path.write_text('{"schema_version": 1}\n', encoding="utf-8")
            self.assertTrue(path.exists())


if __name__ == "__main__":
    unittest.main()
