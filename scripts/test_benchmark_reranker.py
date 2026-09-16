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
    validate_release_gate,
)


class BenchmarkRerankerTest(unittest.TestCase):
    def test_golden_fixture_resolves_relevant_and_distractor_passages(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "calculus.md").write_text(
                "# Calculus\n\nPower Rule\nIf f(x) = x^n, then f'(x) = n · x^(n-1)\n",
                encoding="utf-8",
            )
            (root / "other.md").write_text("An unrelated passage\n", encoding="utf-8")
            path = root / "golden.json"
            path.write_text(
                json.dumps(
                    {
                        "schema_version": 2,
                        "metadata": {"dataset": "expert-authored-synthetic-user-intent"},
                        "queries": [
                            {
                                "id": "q1",
                                "query": "power rule",
                                "relevant": [{"file": "calculus.md", "contains": "power rule"}],
                                "distractors": [{"file": "other.md"}],
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            metadata, queries = load_dataset(path, corpus_dir=root)
        self.assertEqual(metadata["metadata"]["benchmark_adapter"], "golden-relevance-annotations")
        self.assertEqual(len(queries), 1)
        self.assertEqual(len(queries[0].candidates), 2)
        self.assertEqual(sorted(candidate.relevance for candidate in queries[0].candidates), [0, 2])

    def test_golden_fixture_requires_corpus_directory(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "golden.json"
            path.write_text(
                json.dumps(
                    {
                        "queries": [
                            {
                                "id": "q1",
                                "query": "question",
                                "relevant": [{"file": "a.md"}],
                                "distractors": [{"file": "b.md"}],
                            }
                        ]
                    }
                ),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(ValueError, "corpus-dir"):
                load_dataset(path)

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

    def test_release_gate_rejects_synthetic_fixture(self):
        with self.assertRaisesRegex(ValueError, "consented production dataset"):
            validate_release_gate(
                {
                    "metadata": {
                        "dataset": "expert-authored-synthetic-user-intent",
                        "provenance": "repository fixture",
                        "consent": "not-applicable-no-production-user-data",
                        "judgment": "expert-authored",
                        "release_gate": True,
                    },
                    "queries": [{}] * 100,
                }
            )

    def test_release_gate_accepts_complete_production_metadata(self):
        queries = [
            {
                "id": f"q-{index}",
                "expected_answer": "answer",
                "required_claims": ["claim"],
                "faithfulness_label": "fully_supported",
                "candidates": [{"text": "passage", "relevance": 2}],
                "judgments": [
                    {"annotator_id": "expert-a", "relevant": [{"file": "doc.md"}]},
                    {"annotator_id": "expert-b", "relevant": [{"file": "doc.md"}]},
                ],
                "adjudication": {
                    "method": "independent labels reviewed and adjudicated",
                    "reviewer_ids": ["expert-a", "expert-b"],
                },
            }
            for index in range(100)
        ]
        validate_release_gate(
            {
                "metadata": {
                    "dataset": "production-user-query-sample-2026-q3",
                    "provenance": "consent-safe telemetry export",
                    "consent": "user opt-in and retention policy",
                    "judgment": "two expert reviewers with adjudication",
                    "release_gate": True,
                    "judgment_artifact_path": "reviews/judgments.jsonl",
                    "judgment_artifact_sha256": "0" * 64,
                    "source": {
                        "name": "ARQMath public evaluation collection",
                        "homepage": "https://www.cs.rit.edu/~dprl/ARQMath/",
                        "license": "CC BY-SA; non-commercial snapshot terms",
                        "usage": "non-commercial evaluation with attribution",
                        "snapshot": "ARQMath-3",
                        "attribution": "ARQMath and Math Stack Exchange contributors",
                        "license_notice_path": "LICENSE-NOTICE.md",
                        "artifact_sha256": {"Posts.V1.3.zip": "0" * 64, "topics.xml": "0" * 64},
                    },
                    "corpus": {
                        "id": "production-corpus-2026-q3",
                        "documents": ["doc.md"],
                        "document_count": 1,
                        "representative": True,
                        "manifest_path": "manifests/corpus.jsonl",
                        "manifest_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
                    },
                    "privacy_review": {
                        "status": "approved",
                        "reviewer": "privacy-owner@example.test",
                        "reviewed_at": "2026-09-16T00:00:00Z",
                        "evidence_path": "reviews/privacy.md",
                        "evidence_sha256": "1" * 64,
                    },
                    "annotators": [
                        {"id": "expert-a", "role": "domain expert", "human": True, "independent": True, "verification_ref": "reviews/expert-a.md"},
                        {"id": "expert-b", "role": "domain expert", "human": True, "independent": True, "verification_ref": "reviews/expert-b.md"},
                    ],
                },
                "schema_version": 3,
                "queries": queries,
            }
        )

    def test_release_gate_requires_source_and_review_artifacts(self):
        raw = {
            "schema_version": 3,
            "metadata": {
                "dataset": "arqmath-release",
                "provenance": "public source",
                "consent": "licensed public data",
                "judgment": "two human reviewers",
                "release_gate": True,
            },
            "queries": [{
                "id": "q1",
                "expected_answer": "answer",
                "required_claims": ["claim"],
                "faithfulness_label": "fully_supported",
                "candidates": [{"text": "passage", "relevance": 2}],
                "judgments": [
                    {"annotator_id": "a", "relevant": [{"file": "doc.md"}]},
                    {"annotator_id": "b", "relevant": [{"file": "doc.md"}]},
                ],
                "adjudication": {"method": "review", "reviewer_ids": ["a", "b"]},
            }] * 100,
        }
        with self.assertRaisesRegex(ValueError, "source"):
            validate_release_gate(raw)

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
