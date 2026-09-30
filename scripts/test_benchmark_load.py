#!/usr/bin/env python3
import importlib.util
import json
import pathlib
import sys
import tempfile
import unittest
from unittest.mock import patch


MODULE_PATH = pathlib.Path(__file__).with_name("benchmark_load.py")
SPEC = importlib.util.spec_from_file_location("benchmark_load", MODULE_PATH)
benchmark_load = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules["benchmark_load"] = benchmark_load
SPEC.loader.exec_module(benchmark_load)


class LoadBenchmarkTest(unittest.TestCase):
    def test_stream_records_first_content_before_completion_and_preserves_multiline_tokens(self):
        class Response:
            status = 200
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def __iter__(self):
                yield from [b"event: token\n", b"data: hello\n", b"data: world\n", b"\n", b"event: done\n", b"data: 1\n", b"\n"]
        with patch.object(benchmark_load.urllib.request, "urlopen", return_value=Response()), patch.object(benchmark_load.time, "monotonic", side_effect=[1, 1.025]):
            result = benchmark_load.read_stream_result("http://test/events", 1)
        self.assertEqual(result.answer, "hello\nworld")
        self.assertAlmostEqual(result.first_token_ms, 25)

    def test_stream_does_not_count_generation_errors_or_replay_gaps_as_success(self):
        class Response:
            status = 200
            def __init__(self, event): self.event = event
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def __iter__(self):
                yield from [f"event: {self.event}\n".encode(), b"data: problem\n", b"\n"]
        for event in ["generror", "resync", "token"]:
            with self.subTest(event=event), patch.object(benchmark_load.urllib.request, "urlopen", return_value=Response(event)):
                with self.assertRaises(ValueError):
                    benchmark_load.read_stream_result("http://test/events", 1)

    def test_mixed_workload_reports_retrieval_latency_separately(self):
        def operation(index):
            kind = "chat_streams" if index % 2 == 0 else "long_retrieval"
            return benchmark_load.OperationResult(200, b"{}", 5 if index % 2 == 0 else None, kind)
        with patch.object(benchmark_load, "fetch_metrics", return_value={}):
            report = benchmark_load.run_workload("mixed_chat_retrieval", 4, 2, 1, operation, {}, "http://test")
        self.assertEqual(report["by_workload"]["chat_streams"]["requests"], 2)
        self.assertEqual(report["by_workload"]["long_retrieval"]["first_token_latency_ms"]["measured"], 0)

    def test_percentile_uses_nearest_rank(self):
        self.assertEqual(benchmark_load.percentile([3, 1, 2, 4], 0.50), 2)
        self.assertEqual(benchmark_load.percentile([3, 1, 2, 4], 0.95), 4)
        self.assertIsNone(benchmark_load.percentile([], 0.99))

    def test_metric_delta_includes_admission_gauges(self):
        before = {
            "operations": [{"operation": "admission.indexing", "outcome": "acquired", "count": 2, "duration_ms_sum": 5}],
            "gauges": {"admission.indexing.active": 0},
        }
        after = {
            "operations": [{"operation": "admission.indexing", "outcome": "acquired", "count": 5, "duration_ms_sum": 11}],
            "gauges": {"admission.indexing.active": 1, "admission.indexing.max_concurrent": 4},
        }
        delta = benchmark_load.metric_delta(before, after)
        self.assertEqual(delta["operations"][0]["count"], 3)
        self.assertEqual(delta["admission_gauges"]["admission.indexing.active"], 1)

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
