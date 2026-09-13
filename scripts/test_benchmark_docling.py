import unittest
from io import BytesIO
from unittest.mock import patch
from urllib.error import HTTPError

from benchmark_docling import MemorySampler, convert, parse_memory_bytes, percentile


class BenchmarkDoclingTest(unittest.TestCase):
    def test_percentile_uses_nearest_rank(self):
        self.assertEqual(percentile([30, 10, 20, 40], 0.50), 20)
        self.assertEqual(percentile([30, 10, 20, 40], 0.95), 40)
        self.assertIsNone(percentile([], 0.50))

    def test_parse_memory_bytes_supports_docker_units(self):
        self.assertEqual(parse_memory_bytes("12.5MiB"), 12 * 1024 * 1024 + 524288)
        self.assertEqual(parse_memory_bytes("1GB"), 1000**3)
        self.assertIsNone(parse_memory_bytes("unknown"))

    def test_convert_records_http_failure_without_raising(self):
        class FailingResponse:
            def __enter__(self):
                raise HTTPError(
                    "http://docling/convert",
                    503,
                    "Service Unavailable",
                    {},
                    BytesIO(b"converter unavailable"),
                )

            def __exit__(self, exc_type, exc_value, traceback):
                return False

        with patch("benchmark_docling.urllib.request.urlopen", return_value=FailingResponse()):
            with MemorySampler(None, 0.1) as sampler:
                sample = convert(
                    "http://docling/convert",
                    "broken.pdf",
                    b"pdf",
                    1,
                    1024,
                    sampler,
                    "broken.pdf",
                    1,
                )

        self.assertFalse(sample.success)
        self.assertEqual(sample.status, 503)
        self.assertIn("converter unavailable", sample.error)


if __name__ == "__main__":
    unittest.main()
