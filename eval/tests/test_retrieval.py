import json
from pathlib import Path
import tempfile
import types
import unittest

import httpx

from eval.api import ApiClient
from eval.retrieval import (CUTOFFS, aggregate, check_sources, collect, finish, load_golden, match_rank,
                            normalize, ranked, run_command, score_question, summarize)
from eval.runner import Run

ROOT = Path(__file__).resolve().parents[2]


def row(identifier="q1", kind="fact", relevant=None, distractors=None, reviewed=True, question="question?"):
    relevant = [{"file": "a.md", "snippet": "alpha beta"}] if relevant is None else relevant
    return {"id": identifier, "kind": kind, "question": question, "relevant": relevant,
            "reviewed": reviewed, "distractor_files": distractors or []}


def chunk(file, text, rank=0, score=0.5):
    return {"file_path": f"/data/{file}", "text": text, "retrieval_rank": rank, "score": score}


class MatchingTests(unittest.TestCase):
    def test_whitespace_is_normalised_and_file_must_match(self):
        rel = {"file": "a.md", "snippet": "alpha beta"}
        self.assertEqual(match_rank([chunk("b.md", "alpha beta"), chunk("a.md", "x alpha\n  beta y")], rel), 2)
        self.assertIsNone(match_rank([chunk("b.md", "alpha beta")], rel))
        self.assertIsNone(match_rank([chunk("a.md", "alpha gamma beta")], rel))
        self.assertEqual(normalize(" a \n\t b "), "a b")

    def test_inline_markdown_is_ignored_because_the_index_stores_plain_text(self):
        rel = {"file": "a.md", "snippet": "`SET k v NX` creates it **only if** absent"}
        self.assertEqual(match_rank([chunk("a.md", "SET k v NX creates it only if absent")], rel), 1)
        self.assertEqual(match_rank([chunk("a.md", "`SET k v NX` creates it **only if** absent")], rel), 1)

    def test_results_are_ordered_by_retrieval_rank(self):
        turn = {"results": [chunk("a.md", "second", rank=2), chunk("a.md", "first", rank=1)]}
        self.assertEqual([x["text"] for x in ranked(turn)], ["first", "second"])
        self.assertEqual(ranked({}), [])
        with self.assertRaises(ValueError):
            ranked({"results": [{"file_path": "a.md"}]})


class ScoringTests(unittest.TestCase):
    def test_hit_recall_and_first_rank(self):
        golden = row(relevant=[{"file": "a.md", "snippet": "one"}, {"file": "a.md", "snippet": "two"},
                               {"file": "a.md", "snippet": "three"}, {"file": "a.md", "snippet": "four"}])
        results = [chunk("b.md", "noise"), chunk("a.md", "one"), chunk("a.md", "two"), chunk("a.md", "noise")]
        entry = score_question(golden, results)
        self.assertEqual(entry["ranks"], [2, 3, None, None])
        self.assertEqual(entry["first_rank"], 2)
        self.assertEqual({k: entry["hit"][k] for k in CUTOFFS}, {1: False, 3: True, 5: True, 10: True})
        self.assertEqual(entry["recall"][1], 0)
        self.assertEqual(entry["recall"][3], 0.5)
        self.assertEqual(entry["distinct_files"], 2)
        self.assertEqual(entry["max_chunks_per_file"], 3)

    def test_complete_miss_and_unanswerable(self):
        miss = score_question(row(), [chunk("b.md", "noise")])
        self.assertIsNone(miss["first_rank"])
        self.assertFalse(any(miss["hit"].values()))
        none = score_question(row("n", "unanswerable", relevant=[]), [chunk("b.md", "noise", score=0.07)])
        self.assertNotIn("ranks", none)
        self.assertEqual(none["top_score"], 0.07)
        self.assertIsNone(score_question(row("n", "unanswerable", relevant=[]), [])["top_score"])

    def test_distractor_ranked_before_relevant_is_flagged(self):
        golden = row(kind="distractor", distractors=["x.md"])
        first = score_question(golden, [chunk("x.md", "noise"), chunk("a.md", "alpha beta")])
        self.assertTrue(first["distractor_first"])
        self.assertEqual(first["distractor_rank"], 1)
        later = score_question(golden, [chunk("a.md", "alpha beta"), chunk("x.md", "noise")])
        self.assertFalse(later["distractor_first"])
        only = score_question(golden, [chunk("x.md", "noise")])
        self.assertTrue(only["distractor_first"])
        self.assertIsNone(score_question(golden, [chunk("a.md", "alpha beta")])["distractor_rank"])

    def test_aggregate_arithmetic(self):
        entries = [
            score_question(row("a"), [chunk("a.md", "alpha beta")]),                       # rank 1
            score_question(row("b"), [chunk("z.md", "n"), chunk("a.md", "alpha beta")]),   # rank 2
            score_question(row("c"), [chunk("z.md", "n")]),                                # miss
            score_question(row("n", "unanswerable", relevant=[]), [chunk("z.md", "n", score=0.2)]),
        ]
        totals = aggregate(entries)
        self.assertEqual((totals["answerable"], totals["unanswerable"], totals["failed"]), (3, 1, 0))
        self.assertAlmostEqual(totals["mrr"], (1 + 0.5 + 0) / 3)
        self.assertAlmostEqual(totals["hit_rate"]["1"], 1 / 3)
        self.assertAlmostEqual(totals["hit_rate"]["3"], 2 / 3)
        self.assertEqual(totals["unanswerable_top_score"], {"mean": 0.2, "max": 0.2})

    def test_failed_questions_are_counted_but_not_scored(self):
        failed = {"id": "f", "kind": "fact", "reviewed": True, "status": "failed", "error": "boom"}
        totals = aggregate([score_question(row("a"), [chunk("a.md", "alpha beta")]), failed])
        self.assertEqual((totals["questions"], totals["failed"], totals["answerable"]), (2, 1, 1))
        self.assertEqual(totals["mrr"], 1)

    def test_summary_gate_needs_enough_reviewed_answerable_items(self):
        entries = [score_question(row(f"q{i}", reviewed=i < 29), [chunk("a.md", "alpha beta")]) for i in range(40)]
        self.assertEqual(summarize(entries, 10)["gate"], {"reviewed_answerable": 29, "minimum": 30, "gate_quality": False})
        entries[29] = score_question(row("q29", reviewed=True), [chunk("a.md", "alpha beta")])
        self.assertTrue(summarize(entries, 10)["gate"]["gate_quality"])
        self.assertEqual(set(summarize(entries, 10)["by_kind"]), {"fact", "multi_chunk_same_file", "distractor",
                                                                "multi_file", "unanswerable"})


class GoldenFileTests(unittest.TestCase):
    def load(self, rows):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "golden.json"
            path.write_text(json.dumps(rows))
            return load_golden(path)

    def test_invalid_golden_sets_are_rejected(self):
        good = {"id": "a", "kind": "fact", "question": "q?", "reviewed": False,
                "relevant": [{"file": "a.md", "snippet": "s"}]}
        self.assertEqual(self.load([good])[0][0]["distractor_files"], [])
        cases = {
            "empty": [],
            "duplicate id": [good, good],
            "unknown field": [{**good, "extra": 1}],
            "bad kind": [{**good, "kind": "other"}],
            "answerable without evidence": [{**good, "relevant": []}],
            "unanswerable with evidence": [{**good, "kind": "unanswerable"}],
            "reviewed not boolean": [{**good, "reviewed": "yes"}],
            "blank snippet": [{**good, "relevant": [{"file": "a.md", "snippet": " "}]}],
            "chunk id label": [{**good, "relevant": [{"file": "a.md", "snippet": "s", "chunk": 3}]}],
            "bad distractors": [{**good, "distractor_files": "a.md"}],
        }
        for name, rows in cases.items():
            with self.subTest(name), self.assertRaises(ValueError):
                self.load(rows)

    def test_source_check_finds_missing_ambiguous_and_unknown_labels(self):
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "a.md").write_text("alpha beta\ngamma delta\nalpha beta again")
            rows = [row("ok", relevant=[{"file": "a.md", "snippet": "gamma\ndelta"}]),
                    row("twice", relevant=[{"file": "a.md", "snippet": "alpha beta"}]),
                    row("absent", relevant=[{"file": "a.md", "snippet": "nowhere"}]),
                    row("nofile", relevant=[{"file": "z.md", "snippet": "x"}], distractors=["y.md"])]
            problems = check_sources(rows, directory)
        self.assertEqual(len(problems), 4)
        self.assertFalse([p for p in problems if p.startswith("ok:")])

    def test_shipped_golden_set_matches_the_sample_corpus(self):
        rows, snapshot = load_golden(ROOT / "eval/golden/retrieval.json")
        self.assertEqual(check_sources(rows, ROOT / "eval/samples"), [])
        self.assertEqual(len(snapshot["sha256"]), 64)
        kinds = {r["kind"] for r in rows}
        self.assertEqual(kinds, {"fact", "multi_chunk_same_file", "distractor", "multi_file", "unanswerable"})


class CollectionTests(unittest.IsolatedAsyncioTestCase):
    def api(self, answers, *, inventory=("a.md", "b.md"), cached=False):
        requests = []
        deleted = []

        async def handle(request):
            path = request.url.path
            if path == "/api/v1/ready":
                return httpx.Response(200, json={"ready": True})
            if path == "/api/v1/documents":
                return httpx.Response(200, json={"documents": [{"file_path": f"/d/{n}", "source_sha": "x"} for n in inventory]})
            if path == "/api/v1/turns" and request.method == "POST":
                body = json.loads(request.content)
                requests.append(body)
                answer = answers(body)
                if isinstance(answer, int):
                    return httpx.Response(answer, json={})
                return httpx.Response(200, json={"session_id": f"s{len(requests)}", "from_cache": cached,
                                                 "results": answer})
            if request.method == "DELETE":
                deleted.append(path)
            return httpx.Response(204)

        return (lambda host, timeout: ApiClient(host, timeout, transport=httpx.MockTransport(handle))), requests, deleted

    def run_for(self, directory):
        run = Run("retrieval", {}, Path(directory) / "run")
        run.record["provenance"] = {"phase": "retrieval", "producer": {}}
        run.record["results"] = {"questions": []}
        return run

    async def test_scores_questions_probes_misses_and_cleans_up(self):
        def answers(body):
            if body["query"] == "found":
                return [chunk("a.md", "alpha beta", rank=1)]
            if body["top_k"] == 50:                      # the probe finds the passage deeper down
                return [chunk("b.md", "n", rank=1)] * 3 + [chunk("a.md", "alpha beta", rank=4)]
            return [chunk("b.md", "noise", rank=1)]
        factory, requests, deleted = self.api(answers)
        rows = [row("hit", question="found"), row("late", question="late"),
                row("none", "unanswerable", relevant=[], question="late")]
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_for(directory)
            await collect(run, rows, {"sha256": "abc"}, "http://nadir", 10, 5, 50, api_factory=factory)
            finish(run, 10, False)
            report = json.loads((run.path / "report.json").read_text())
        for body in requests:
            self.assertFalse(body["generate"])
            self.assertTrue(body["skip_cache"])
        self.assertEqual([b["top_k"] for b in requests], [10, 10, 50, 10])   # only the missing question is probed
        record = report["runs"][0]
        self.assertEqual((report["tool"], record["run_type"], record["status"]), ("evaluator", "retrieval", "completed"))
        by_id = {e["id"]: e for e in record["results"]["questions"]}
        self.assertEqual(by_id["hit"]["first_rank"], 1)
        self.assertIsNone(by_id["late"]["first_rank"])
        self.assertEqual(by_id["late"]["probe"]["ranks"], [4])
        self.assertEqual(record["summary"]["never_retrieved"], [])
        self.assertEqual(record["summary"]["overall"]["answerable"], 2)
        self.assertEqual(len(deleted), 4)                                    # every created session removed
        self.assertEqual(record["provenance"]["dataset"], {"sha256": "abc"})

    async def test_label_never_retrieved_even_when_probed_is_reported(self):
        factory, _, _ = self.api(lambda body: [chunk("b.md", "noise", rank=1)])
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_for(directory)
            await collect(run, [row("ghost")], {}, "http://nadir", 10, 5, 50, api_factory=factory)
            finish(run, 10, False)
        self.assertEqual(run.record["summary"]["never_retrieved"], ["ghost"])

    async def test_failed_question_is_recorded_and_the_run_continues(self):
        factory, _, _ = self.api(lambda body: 500 if body["query"] == "bad" else [chunk("a.md", "alpha beta", rank=1)])
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_for(directory)
            await collect(run, [row("bad", question="bad"), row("good", question="good")], {}, "http://nadir", 10, 5, 0,
                          api_factory=factory)
            finish(run, 10, False)
        self.assertEqual(run.record["status"], "failed")
        self.assertEqual([e["status"] for e in run.record["results"]["questions"]], ["failed", "completed"])
        self.assertEqual(run.record["summary"]["overall"]["failed"], 1)

    async def test_missing_corpus_documents_stop_the_run_before_any_query(self):
        factory, requests, _ = self.api(lambda body: [], inventory=("b.md",))
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_for(directory)
            with self.assertRaisesRegex(ValueError, "a.md"):
                await collect(run, [row()], {}, "http://nadir", 10, 5, 50, api_factory=factory)
        self.assertEqual(requests, [])

    async def test_cache_hit_is_an_error_not_a_score(self):
        factory, _, _ = self.api(lambda body: [chunk("a.md", "alpha beta", rank=1)], cached=True)
        with tempfile.TemporaryDirectory() as directory:
            run = self.run_for(directory)
            await collect(run, [row()], {}, "http://nadir", 10, 5, 0, api_factory=factory)
        self.assertEqual(run.record["results"]["questions"][0]["status"], "failed")


class CommandTests(unittest.TestCase):
    def options(self, **overrides):
        values = dict(golden=ROOT / "eval/golden/retrieval.json", samples_dir=ROOT / "eval/samples", validate=True,
                      host=None, top_k=10, probe_top_k=50, timeout=5, output_dir=None)
        return types.SimpleNamespace(**{**values, **overrides})

    def test_validate_needs_no_api_and_reports_label_problems(self):
        self.assertEqual(run_command(self.options(), Run), 0)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "golden.json"
            path.write_text(json.dumps([row("bad", relevant=[{"file": "os.md", "snippet": "not in the document"}])]))
            self.assertEqual(run_command(self.options(golden=path), Run), 1)

    def test_running_without_a_host_is_an_error(self):
        with self.assertRaisesRegex(ValueError, "--host"):
            run_command(self.options(validate=False), Run)


if __name__ == "__main__":
    unittest.main()
