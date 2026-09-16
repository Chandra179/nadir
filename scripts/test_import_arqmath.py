import json
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

sys.path.insert(0, str(Path(__file__).parent))

import import_arqmath


class ImportArqMathTest(unittest.TestCase):
    def test_sha256_verification_rejects_wrong_artifact(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "artifact.bin"
            path.write_bytes(b"pinned artifact")
            expected = import_arqmath.sha256_file(path)
            self.assertEqual(import_arqmath.verify_sha256(path, expected), expected)
            downloaded = Path(directory) / "downloaded.bin"
            import_arqmath.download_verified(path.as_uri(), downloaded, expected)
            self.assertEqual(downloaded.read_bytes(), b"pinned artifact")
            with self.assertRaisesRegex(ValueError, "SHA-256 mismatch"):
                import_arqmath.verify_sha256(path, "0" * 64)

    def test_checked_in_arqmath_candidate_has_120_queries_and_three_editions(self):
        path = Path(__file__).parents[1] / "test/evaluation/arqmath/arqmath-review-pack.json"
        pack = json.loads(path.read_text(encoding="utf-8"))
        sources = json.loads((path.parent / "SOURCES.json").read_text(encoding="utf-8"))
        import_arqmath.validate_review_pack(pack)
        report = json.loads((path.parent / "arqmath-review-report.json").read_text(encoding="utf-8"))
        queries = pack["queries"]
        self.assertEqual(len(queries), 120)
        self.assertEqual(len({query["id"] for query in queries}), 120)
        self.assertEqual({year: sum(query["source_year"] == year for query in queries) for year in ("2020", "2021", "2022")}, {"2020": 40, "2021": 40, "2022": 40})
        self.assertFalse(pack["metadata"]["release_gate"])
        self.assertEqual(report["query_count"], 120)
        self.assertEqual(report["retrieval_evaluation"]["status"], "not_run")
        self.assertEqual(len(pack["metadata"]["source"]["artifact_sha256"]), 6)
        source_manifest = {artifact["name"]: artifact.get("sha256") for artifact in sources["artifacts"]}
        self.assertEqual(pack["metadata"]["source"]["artifact_sha256"], {name: digest for name, digest in source_manifest.items() if digest})
        self.assertTrue((path.parent / pack["metadata"]["source"]["license_notice_path"]).is_file())
        self.assertTrue(all(query["candidate_documents"] for query in queries))

    def test_html_to_text_removes_markup_and_preserves_math_text(self):
        value = "<p>How do I solve <span class=\"math-container\">$x^2$</span>?</p><p>Use <strong>factoring</strong>.</p>"
        self.assertEqual(import_arqmath.html_to_text(value), "How do I solve $x^2$?\nUse factoring.")

    def test_select_topics_returns_deterministic_forty_per_edition(self):
        topic_sets = []
        qrel_sets = []
        for edition in range(3):
            topics = [
                {
                    "source_id": f"A.{edition * 100 + index}",
                    "edition": f"arqmath-{edition + 1}",
                    "year": str(2020 + edition),
                    "query": f"query {edition}-{index}",
                    "tags": [],
                }
                for index in range(1, 43)
            ]
            topic_sets.append(topics)
            qrel_sets.append({topic["source_id"]: [{"document_id": str(index), "grade": 2}] for index, topic in enumerate(topics, 1)})
        selected = import_arqmath.select_topics(topic_sets, qrel_sets)
        self.assertEqual(len(selected), 120)
        self.assertEqual([selected[index]["source_year"] for index in (0, 39, 40, 79, 80, 119)], ["2020", "2020", "2021", "2021", "2022", "2022"])
        self.assertEqual(len({query["id"] for query in selected}), 120)

    def test_parse_qrels_rejects_duplicate_query_document_rows(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "qrels.tsv"
            path.write_text("A.1 0 42 2\nA.1 0 42 1\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "duplicate"):
                import_arqmath.parse_qrels(path)

    def test_write_corpus_normalizes_only_answer_posts_when_requested(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            posts = root / "Posts.xml"
            posts.write_text(
                "<posts>"
                '<row Id="1" PostTypeId="1" Body="&lt;p&gt;question&lt;/p&gt;" />'
                '<row Id="2" PostTypeId="2" ParentId="1" Body="&lt;p&gt;The &lt;strong&gt;answer&lt;/strong&gt; is $42$.&lt;/p&gt;" />'
                "</posts>",
                encoding="utf-8",
            )
            corpus = import_arqmath.write_corpus(posts, root / "corpus", include_all_answers=False, document_ids={"2"})
            document = root / "corpus/posts/2.md"
            self.assertTrue(document.is_file())
            self.assertFalse((root / "corpus/posts/1.md").exists())
            self.assertEqual(corpus["document_count"], 1)
            manifest = (root / "corpus/manifest.jsonl").read_text(encoding="utf-8")
            self.assertIn('"source_id": "2"', manifest)

    def test_review_label_must_be_in_fixed_candidate_pool(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "posts").mkdir()
            (root / "posts/42.md").write_text("Source post ID: 42\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "outside the fixed candidate pool"):
                import_arqmath.map_labels([{"document_id": "42", "grade": 2}], root, {"99"})

    def test_review_labels_must_cover_every_candidate_exactly_once(self):
        row = {
            "relevant": [{"document_id": "42", "grade": 2}],
            "distractors": [],
        }
        with self.assertRaisesRegex(ValueError, "label every candidate"):
            import_arqmath.validate_complete_label_set(row, {"42", "99"}, "review")

    def test_merge_reviews_requires_both_reviewers_for_every_query(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            corpus = root / "corpus/posts"
            corpus.mkdir(parents=True)
            (corpus / "42.md").write_text("# answer\n\nSource post ID: 42\n\nThe answer is 42.\n", encoding="utf-8")
            manifest = root / "corpus/manifest.jsonl"
            manifest.write_text('{"path":"posts/42.md","source_id":"42"}\n', encoding="utf-8")
            pack = root / "review-pack.json"
            pack.write_text(
                json.dumps({
                    "schema_version": 1,
                    "metadata": {
                        "release_gate": False,
                        "source": {
                            "name": "ARQMath public evaluation collection",
                            "artifact_sha256": {"Posts.V1.3.zip": "c" * 64},
                        },
                        "corpus": {
                            "representative": True,
                            "manifest_path": "corpus/manifest.jsonl",
                            "manifest_sha256": import_arqmath.sha256_file(manifest),
                        },
                    },
                    "queries": [{
                        "id": "arqmath-2022-A.1",
                        "query": "What is the answer?",
                        "tags": [],
                        "candidate_documents": [{"document_id": "42", "grade": 3}],
                    }],
                }),
                encoding="utf-8",
            )
            reviewer_paths = []
            for reviewer_id in ("expert-a", "expert-b"):
                reviewer = root / f"{reviewer_id}.json"
                reviewer.write_text(json.dumps({
                    "reviewer": {
                        "id": reviewer_id,
                        "role": "math expert",
                        "human": True,
                        "independent": True,
                        "verification_ref": f"reviews/{reviewer_id}.md",
                    },
                    "judgments": [{
                        "query_id": "arqmath-2022-A.1",
                        "relevant": [{"document_id": "42", "grade": 3}],
                        "distractors": [],
                    }],
                }), encoding="utf-8")
                reviewer_paths.append(reviewer)
            adjudication = root / "adjudication.json"
            adjudication.write_text(json.dumps({
                "queries": [{
                    "query_id": "arqmath-2022-A.1",
                    "type": "factoid",
                    "expected_answer": "42",
                    "required_claims": ["the answer is 42"],
                    "faithfulness_label": "fully_supported",
                    "relevant": [{"document_id": "42", "grade": 3}],
                    "distractors": [],
                }],
            }), encoding="utf-8")
            privacy = root / "privacy.json"
            privacy.write_text(json.dumps({
                "status": "approved",
                "reviewer": "privacy-owner",
                "reviewed_at": "2026-09-16T00:00:00Z",
                "evidence_path": "reviews/privacy.md",
                "evidence_sha256": "b" * 64,
            }), encoding="utf-8")
            output = root / "golden.json"
            import_arqmath.merge_reviews(pack, reviewer_paths, adjudication, privacy, output)
            golden = json.loads(output.read_text(encoding="utf-8"))
            self.assertTrue(golden["metadata"]["release_gate"])
            self.assertEqual(len(golden["queries"][0]["judgments"]), 2)
            self.assertEqual(golden["queries"][0]["relevant"][0]["file"], "posts/42.md")

    def test_merge_reviews_rejects_incomplete_reviewer(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            pack = root / "pack.json"
            pack.write_text(json.dumps({"metadata": {"release_gate": False}, "queries": [{"id": "q1"}]}), encoding="utf-8")
            reviewer = root / "reviewer.json"
            reviewer.write_text(json.dumps({
                "reviewer": {"id": "a", "role": "math expert", "human": True, "independent": True, "verification_ref": "a"},
                "judgments": [],
            }), encoding="utf-8")
            reviewer_b = root / "reviewer-b.json"
            reviewer_b.write_text(json.dumps({
                "reviewer": {"id": "b", "role": "math expert", "human": True, "independent": True, "verification_ref": "b"},
                "judgments": [],
            }), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "every selected query"):
                import_arqmath.merge_reviews(pack, [reviewer, reviewer_b], root / "adjudication.json", root / "privacy.json", root / "out.json")


if __name__ == "__main__":
    unittest.main()
