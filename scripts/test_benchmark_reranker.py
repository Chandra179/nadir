import json
import unittest
from io import BytesIO
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch
from urllib.error import HTTPError

from benchmark_reranker import (
    QueryCase,
    ResourceSampler,
    Candidate,
    load_dataset,
    parse_memory_bytes,
    percentile,
    ranking_metrics,
    rerank,
)


class BenchmarkRerankerTest(unittest.TestCase):
    def test_dataset_requires_positive_relevance(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "dataset.json"
            path.write_text(
                json.dumps(
                    {
                        "queries": [
                            {
                                "id": "q1",
                                "query": "question",
                                "candidates": [{"text": "passage", "relevance": 0}],
                            }
                        ]
                    }
                ),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(ValueError, "positive relevance"):
                load_dataset(path)

    def test_ranking_metrics_are_stable_for_score_ties(self):
        query = QueryCase(
            "q1",
            "question",
            (
                Candidate("first", 0),
                Candidate("second", 2),
                Candidate("third", 1),
            ),
        )
        metrics = ranking_metrics(query, [0.5, 0.5, 0.1], 2)
        self.assertEqual(metrics["first_relevant_rank"], 2)
        self.assertEqual(metrics["relevant_found_at_k"], 1)
        self.assertEqual(metrics["hit_rate_at_k"], 1)
        self.assertEqual(metrics["recall_at_k"], 0.5)
        self.assertGreater(metrics["ndcg_at_k"], 0)

    def test_rerank_records_scores_and_http_failures(self):
        query = QueryCase("q1", "question", (Candidate("one", 1), Candidate("two", 0)))

        class SuccessfulResponse:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, exc_type, exc_value, traceback):
                return False

            def read(self, limit):
                return b'{"scores":[0.8,0.1]}'

        with patch("benchmark_reranker.urllib.request.urlopen", return_value=SuccessfulResponse()):
            with ResourceSampler(None, None, 0.1) as sampler:
                sample = rerank("http://reranker/rerank", query, 1, 1024, sampler, 1)
        self.assertTrue(sample.success)
        self.assertEqual(sample.scores, [0.8, 0.1])

        class FailingResponse:
            def __enter__(self):
                raise HTTPError(
                    "http://reranker/rerank",
                    429,
                    "Too Many Requests",
                    {},
                    BytesIO(b"capacity busy"),
                )

            def __exit__(self, exc_type, exc_value, traceback):
                return False

        with patch("benchmark_reranker.urllib.request.urlopen", return_value=FailingResponse()):
            with ResourceSampler(None, None, 0.1) as sampler:
                sample = rerank("http://reranker/rerank", query, 1, 1024, sampler, 1)
        self.assertFalse(sample.success)
        self.assertEqual(sample.status, 429)
        self.assertEqual(sample.error, "HTTP 429")

    def test_health_check_rejects_not_ready_payload(self):
        class NotReadyResponse:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, exc_type, exc_value, traceback):
                return False

            def read(self, limit):
                return b'{"status":"not_ready"}'

        from benchmark_reranker import health_check

        with patch("benchmark_reranker.urllib.request.urlopen", return_value=NotReadyResponse()):
            with self.assertRaisesRegex(ValueError, "not ready"):
                health_check("http://reranker/health", 1)

    def test_helpers_parse_memory_and_percentiles(self):
        self.assertEqual(parse_memory_bytes("12.5MiB"), 12 * 1024 * 1024 + 524288)
        self.assertIsNone(parse_memory_bytes("unknown"))
        self.assertEqual(percentile([30, 10, 20, 40], 0.50), 20)
        self.assertEqual(percentile([30, 10, 20, 40], 0.95), 40)


if __name__ == "__main__":
    unittest.main()
